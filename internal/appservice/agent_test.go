package appservice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"pi-desk/internal/domain"
	"pi-desk/internal/pirpc"
	"pi-desk/internal/piruntime"
	"pi-desk/internal/sessionindex"
)

type fakeAgentRuntime struct {
	startConfig     piruntime.StartConfig
	threadID        string
	command         map[string]any
	stopped         string
	shutdown        bool
	callError       error
	callHasDeadline bool
	failCommand     string
	stopError       error
	sent            map[string]any
	stateData       json.RawMessage
	responseData    json.RawMessage
}

func (runtime *fakeAgentRuntime) Start(_ context.Context, config piruntime.StartConfig) (piruntime.SessionInfo, error) {
	runtime.startConfig = config
	return piruntime.SessionInfo{
		ThreadID: config.ThreadID, Generation: 3, State: json.RawMessage(`{"sessionId":"session-1"}`),
	}, nil
}

func (runtime *fakeAgentRuntime) Call(ctx context.Context, threadID string, command map[string]any) (pirpc.Response, error) {
	runtime.threadID = threadID
	runtime.command = command
	_, runtime.callHasDeadline = ctx.Deadline()
	if runtime.callError != nil && (runtime.failCommand == "" || runtime.failCommand == command["type"]) {
		return pirpc.Response{}, runtime.callError
	}
	if command["type"] == "get_state" && runtime.stateData != nil {
		return pirpc.Response{Type: "response", Command: "get_state", Success: true, Data: runtime.stateData}, nil
	}
	if runtime.responseData != nil {
		return pirpc.Response{Type: "response", Command: command["type"].(string), Success: true, Data: runtime.responseData}, nil
	}
	return pirpc.Response{Type: "response", Command: command["type"].(string), Success: true, Data: json.RawMessage(`{"ok":true}`)}, nil
}

func TestAgentServiceEditsPersistedMessageAndReloadsPi(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join([]string{
		`{"type":"session","version":3,"id":"session","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`,
		`{"type":"message","id":"user-1","parentId":null,"message":{"role":"user","content":"Before"}}`,
	}, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := &fakeAgentRuntime{stateData: json.RawMessage(`{"sessionFile":` + strconv.Quote(path) + `,"isStreaming":false}`)}
	service := newAgentService(runtime)
	service.index = sessionindex.New(root)

	if _, err := service.EditSessionMessage(domain.SessionMessageRequest{ThreadID: "thread-1", Path: path, EntryID: "user-1", Text: "After"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"content":"After"`) || runtime.command["type"] != "switch_session" || runtime.command["sessionPath"] != path {
		t.Fatalf("session=%s command=%#v", data, runtime.command)
	}
}

func TestAgentServiceRemovesAssistantForkWhenPiCannotSwitch(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join([]string{
		`{"type":"session","version":3,"id":"session","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`,
		`{"type":"message","id":"user-1","parentId":null,"message":{"role":"user","content":"Question"}}`,
		`{"type":"message","id":"assistant-1","parentId":"user-1","message":{"role":"assistant","content":"Answer"}}`,
	}, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := &fakeAgentRuntime{
		stateData:   json.RawMessage(`{"sessionFile":` + strconv.Quote(path) + `,"isStreaming":false}`),
		callError:   errors.New("switch failed"),
		failCommand: "switch_session",
	}
	service := newAgentService(runtime)
	service.index = sessionindex.New(root)

	if _, err := service.ForkSessionAt(domain.SessionMessageRequest{ThreadID: "thread-1", Path: path, EntryID: "assistant-1"}); err == nil {
		t.Fatal("expected the failed Pi switch to be reported")
	}
	files, err := filepath.Glob(filepath.Join(directory, "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != path {
		t.Fatalf("failed fork left an orphaned session: %#v", files)
	}
}

func TestAgentServiceForksBeforeRootUserIntoPersistedSession(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join([]string{
		`{"type":"session","version":3,"id":"session","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`,
		`{"type":"message","id":"user-1","parentId":null,"message":{"role":"user","content":"Question"}}`,
	}, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := &fakeAgentRuntime{
		stateData:    json.RawMessage(`{"sessionFile":` + strconv.Quote(path) + `,"isStreaming":false}`),
		responseData: json.RawMessage(`null`),
	}
	service := newAgentService(runtime)
	service.index = sessionindex.New(root)

	result, err := service.ForkSessionAt(domain.SessionMessageRequest{
		ThreadID: "thread-1", Path: path, EntryID: "user-1", Before: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	forked, _ := runtime.command["sessionPath"].(string)
	if runtime.command["type"] != "switch_session" || forked == "" {
		t.Fatalf("unexpected switch command: %#v", runtime.command)
	}
	data, err := os.ReadFile(forked)
	if err != nil {
		t.Fatalf("fork was not persisted before switching: %v", err)
	}
	if strings.Count(string(data), "\n") != 1 || strings.Contains(string(data), `"id":"user-1"`) {
		t.Fatalf("root fork should contain only its header: %s", data)
	}
	var response struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(result.DataJSON), &response); err != nil || response.Text != "Question" {
		t.Fatalf("fork response = %q, error = %v", result.DataJSON, err)
	}
}

func (runtime *fakeAgentRuntime) Stop(threadID string) error {
	runtime.stopped = threadID
	return runtime.stopError
}

func (runtime *fakeAgentRuntime) Send(_ string, command map[string]any) error {
	runtime.sent = command
	return nil
}

func (*fakeAgentRuntime) Diagnostics(string) (string, error) { return "diagnostic", nil }
func (*fakeAgentRuntime) ActiveCount() int                   { return 0 }
func (runtime *fakeAgentRuntime) StopAll() error {
	runtime.stopped = "*"
	return runtime.stopError
}
func (runtime *fakeAgentRuntime) Shutdown() { runtime.shutdown = true }

func TestAgentServiceStartsTrustedSessionAndForwardsPrompt(t *testing.T) {
	runtime := &fakeAgentRuntime{}
	service := newAgentService(runtime)

	session, err := service.StartSession(domain.StartSessionRequest{
		ThreadID: " thread-1 ", Workspace: " workspace ", Trust: "approve", Offline: true,
	})
	if err != nil {
		t.Fatalf("StartSession returned an error: %v", err)
	}
	if session.ThreadID != "thread-1" || session.Generation != 3 || runtime.startConfig.Trust != piruntime.TrustApprove {
		t.Fatalf("unexpected session: %#v, config: %#v", session, runtime.startConfig)
	}

	result, err := service.SendPrompt(domain.PromptRequest{
		ThreadID: "thread-1", Message: "  continue  ", StreamingBehavior: "steer",
		Images: []domain.ImageContent{{Type: "image", Data: "aW1hZ2U=", MIMEType: "image/png"}},
	})
	if err != nil {
		t.Fatalf("SendPrompt returned an error: %v", err)
	}
	if result.Command != "prompt" || runtime.command["message"] != "continue" || runtime.command["streamingBehavior"] != "steer" || runtime.threadID != "thread-1" {
		t.Fatalf("prompt was not forwarded correctly: %#v", runtime.command)
	}
	if runtime.callHasDeadline {
		t.Fatal("prompt preflight must not use a fixed RPC deadline")
	}
	if images, ok := runtime.command["images"].([]domain.ImageContent); !ok || len(images) != 1 {
		t.Fatalf("prompt images were not forwarded correctly: %#v", runtime.command)
	}
}

func TestAgentServiceCompactionDoesNotAbandonModelBackedRPC(t *testing.T) {
	runtime := &fakeAgentRuntime{}
	service := newAgentService(runtime)

	result, err := service.Compact(domain.CompactRequest{
		ThreadID: "thread-1", CustomInstructions: "  retain the architecture decisions  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Command != "compact" || runtime.command["type"] != "compact" {
		t.Fatalf("unexpected command: %#v", runtime.command)
	}
	if runtime.command["customInstructions"] != "retain the architecture decisions" {
		t.Fatalf("custom instructions were not normalized: %#v", runtime.command)
	}
	if runtime.callHasDeadline {
		t.Fatal("model-backed compaction must not use a fixed RPC deadline")
	}
}

func TestAgentServiceValidatesCommandsAndPropagatesRuntimeError(t *testing.T) {
	runtime := &fakeAgentRuntime{callError: errors.New("runtime failed")}
	service := newAgentService(runtime)

	if _, err := service.SendPrompt(domain.PromptRequest{ThreadID: "thread-1", Message: "  "}); err == nil {
		t.Fatal("expected empty prompt to fail")
	}
	if _, err := service.SendPrompt(domain.PromptRequest{
		ThreadID: "thread-1", Images: []domain.ImageContent{{Type: "image", Data: "not-base64", MIMEType: "image/png"}},
	}); err == nil {
		t.Fatal("expected invalid image data to fail")
	}
	if _, err := service.SendPrompt(domain.PromptRequest{
		ThreadID: "thread-1", Images: []domain.ImageContent{{Type: "image", Data: "aW1hZ2U=", MIMEType: "image/svg+xml"}},
	}); err == nil {
		t.Fatal("expected unsupported image type to fail")
	}
	oversizedImage := base64.StdEncoding.EncodeToString(make([]byte, maxImageBytes+1))
	if _, err := service.SendPrompt(domain.PromptRequest{
		ThreadID: "thread-1", Images: []domain.ImageContent{{Type: "image", Data: oversizedImage, MIMEType: "image/png"}},
	}); err == nil {
		t.Fatal("expected oversized image data to fail")
	}
	if _, err := service.SendPrompt(domain.PromptRequest{ThreadID: "thread-1", Message: "test", StreamingBehavior: "later"}); err == nil {
		t.Fatal("expected invalid streaming behavior to fail")
	}
	if _, err := service.SetModel(domain.ModelRequest{ThreadID: "thread-1"}); err == nil {
		t.Fatal("expected incomplete model selection to fail")
	}
	if _, err := service.SetSessionName(domain.SessionNameRequest{ThreadID: "thread-1", Name: "  "}); err == nil {
		t.Fatal("expected empty session name to fail")
	}
	if _, err := service.SetSteeringMode(domain.QueueModeRequest{ThreadID: "thread-1", Mode: "later"}); err == nil {
		t.Fatal("expected invalid queue mode to fail")
	}
	if _, err := service.Bash(domain.BashRequest{ThreadID: "thread-1", Command: "  "}); err == nil {
		t.Fatal("expected empty bash command to fail")
	}
	if _, err := service.ForkSession(domain.SessionForkRequest{ThreadID: "thread-1"}); err == nil {
		t.Fatal("expected empty fork entry to fail")
	}
	if _, err := service.ExportSession(domain.ExportSessionRequest{ThreadID: "thread-1", OutputPath: "relative.html"}); err == nil {
		t.Fatal("expected relative export path to fail")
	}
	if _, err := service.GetState(domain.ThreadRequest{ThreadID: "thread-1"}); !errors.Is(err, runtime.callError) {
		t.Fatalf("expected runtime error, got %v", err)
	}
}

func TestAgentServiceCompactsFlatSessionBranches(t *testing.T) {
	runtime := &fakeAgentRuntime{responseData: json.RawMessage(`{
		"entries": [
			{"id":"user-1","parentId":null,"type":"message","timestamp":"2026-08-10T08:00:00Z","message":{"role":"user","content":[{"type":"text","text":"Inspect runtime"},{"type":"image","data":"ignored"}]}},
			{"id":"assistant-1","parentId":"user-1","type":"message","message":{"role":"assistant","content":"Done"}},
			{"id":"label-1","parentId":"assistant-1","type":"label","targetId":"user-1","label":"Audit root"}
		],
		"leafId":"label-1"
	}`)}
	service := newAgentService(runtime)

	result, err := service.GetSessionBranches(domain.ThreadRequest{ThreadID: "thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.command["type"] != "get_entries" || result.LeafID != "label-1" || len(result.Entries) != 3 {
		t.Fatalf("unexpected branches: command=%#v result=%#v", runtime.command, result)
	}
	if result.Entries[0].Text != "Inspect runtime" || result.Entries[0].Role != "user" || result.Entries[0].Label != "Audit root" {
		t.Fatalf("unexpected compact user entry: %#v", result.Entries[0])
	}
	if result.Entries[1].ParentID != "user-1" || result.Entries[1].Text != "Done" {
		t.Fatalf("unexpected compact assistant entry: %#v", result.Entries[1])
	}
}

func TestCompactSessionBranchesRejectsMalformedResponse(t *testing.T) {
	if _, err := compactSessionBranches([]byte(`{"entries":[`)); err == nil {
		t.Fatal("expected malformed response to fail")
	}
}

func TestAgentServiceForwardsSessionLifecycleCommands(t *testing.T) {
	runtime := &fakeAgentRuntime{}
	service := newAgentService(runtime)

	tests := []struct {
		name    string
		invoke  func() (domain.CommandResult, error)
		command string
	}{
		{name: "fork messages", invoke: func() (domain.CommandResult, error) {
			return service.GetForkMessages(domain.ThreadRequest{ThreadID: "thread-1"})
		}, command: "get_fork_messages"},
		{name: "session stats", invoke: func() (domain.CommandResult, error) {
			return service.GetSessionStats(domain.ThreadRequest{ThreadID: "thread-1"})
		}, command: "get_session_stats"},
		{name: "clone", invoke: func() (domain.CommandResult, error) {
			return service.CloneSession(domain.ThreadRequest{ThreadID: "thread-1"})
		}, command: "clone"},
		{name: "fork", invoke: func() (domain.CommandResult, error) {
			return service.ForkSession(domain.SessionForkRequest{ThreadID: "thread-1", EntryID: " entry-1 "})
		}, command: "fork"},
		{name: "export", invoke: func() (domain.CommandResult, error) {
			return service.ExportSession(domain.ExportSessionRequest{ThreadID: "thread-1", OutputPath: filepath.Join(t.TempDir(), "session.html")})
		}, command: "export_html"},
		{name: "auto retry", invoke: func() (domain.CommandResult, error) {
			return service.SetAutoRetry(domain.ToggleRequest{ThreadID: "thread-1", Enabled: false})
		}, command: "set_auto_retry"},
		{name: "auto compaction", invoke: func() (domain.CommandResult, error) {
			return service.SetAutoCompaction(domain.ToggleRequest{ThreadID: "thread-1", Enabled: false})
		}, command: "set_auto_compaction"},
		{name: "steering mode", invoke: func() (domain.CommandResult, error) {
			return service.SetSteeringMode(domain.QueueModeRequest{ThreadID: "thread-1", Mode: "all"})
		}, command: "set_steering_mode"},
		{name: "follow-up mode", invoke: func() (domain.CommandResult, error) {
			return service.SetFollowUpMode(domain.QueueModeRequest{ThreadID: "thread-1", Mode: "one-at-a-time"})
		}, command: "set_follow_up_mode"},
		{name: "abort retry", invoke: func() (domain.CommandResult, error) {
			return service.AbortRetry(domain.ThreadRequest{ThreadID: "thread-1"})
		}, command: "abort_retry"},
		{name: "bash", invoke: func() (domain.CommandResult, error) {
			return service.Bash(domain.BashRequest{ThreadID: "thread-1", Command: " git status ", ExcludeFromContext: true})
		}, command: "bash"},
		{name: "abort bash", invoke: func() (domain.CommandResult, error) {
			return service.AbortBash(domain.ThreadRequest{ThreadID: "thread-1"})
		}, command: "abort_bash"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.invoke()
			if err != nil {
				t.Fatal(err)
			}
			if result.Command != test.command || runtime.command["type"] != test.command {
				t.Fatalf("unexpected command: %#v", runtime.command)
			}
			if test.command == "export_html" && runtime.command["outputPath"] == "" {
				t.Fatalf("export path was not forwarded: %#v", runtime.command)
			}
			if test.command == "bash" && (runtime.command["command"] != "git status" || runtime.command["excludeFromContext"] != true) {
				t.Fatalf("bash command was not forwarded: %#v", runtime.command)
			}
		})
	}
}

func TestAgentServiceStopsAndShutsDownRuntime(t *testing.T) {
	runtime := &fakeAgentRuntime{}
	service := newAgentService(runtime)

	if err := service.StopSession(domain.ThreadRequest{ThreadID: " thread-1 "}); err != nil {
		t.Fatalf("StopSession returned an error: %v", err)
	}
	if runtime.stopped != "thread-1" {
		t.Fatalf("unexpected stopped thread: %q", runtime.stopped)
	}
	if err := service.ServiceShutdown(); err != nil {
		t.Fatalf("ServiceShutdown returned an error: %v", err)
	}
	if !runtime.shutdown {
		t.Fatal("runtime was not shut down")
	}
	if _, err := service.GetState(domain.ThreadRequest{ThreadID: "thread-1"}); err == nil {
		t.Fatal("expected service to reject calls after shutdown")
	}
}

func TestAgentServiceTreatsAnAlreadyStoppedThreadAsSafeForCleanup(t *testing.T) {
	runtime := &fakeAgentRuntime{stopError: piruntime.ErrThreadNotRunning}
	service := newAgentService(runtime)
	if err := service.StopSession(domain.ThreadRequest{ThreadID: " thread-1 "}); err != nil {
		t.Fatal(err)
	}
	if runtime.stopped != "thread-1" {
		t.Fatalf("unexpected stopped thread: %q", runtime.stopped)
	}
}

func TestAgentServiceRejectsOversizedExtensionUIResponse(t *testing.T) {
	runtime := &fakeAgentRuntime{}
	service := newAgentService(runtime)

	err := service.RespondExtensionUI(domain.ExtensionUIResponseRequest{
		ThreadID: "thread-1", RequestID: "ui-1", Value: strings.Repeat("x", maxExtensionUIResponse+1),
	})
	if err == nil {
		t.Fatal("expected oversized extension response to be rejected")
	}
	if runtime.sent != nil {
		t.Fatalf("runtime received an oversized response: %#v", runtime.sent)
	}
}

func TestAgentServiceSendsExtensionUIResponseWithoutWaitingForRPCReply(t *testing.T) {
	runtime := &fakeAgentRuntime{}
	service := newAgentService(runtime)
	confirmed := false

	if err := service.RespondExtensionUI(domain.ExtensionUIResponseRequest{
		ThreadID: "thread-1", RequestID: "ui-1", Confirmed: &confirmed,
	}); err != nil {
		t.Fatalf("RespondExtensionUI returned an error: %v", err)
	}
	if runtime.sent["type"] != "extension_ui_response" || runtime.sent["id"] != "ui-1" || runtime.sent["confirmed"] != false {
		t.Fatalf("unexpected extension response: %#v", runtime.sent)
	}
}
