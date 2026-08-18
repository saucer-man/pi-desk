package appservice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"pi-desk/internal/domain"
	"pi-desk/internal/pirpc"
	"pi-desk/internal/piruntime"
	"pi-desk/internal/sessionindex"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	piEventName            = "pi:event"
	commandTimeout         = 10 * time.Second
	longCommandTimeout     = 120 * time.Second
	sessionTimeout         = 30 * time.Second
	maxPromptBytes         = 1 << 20
	maxAttachedImages      = 10
	maxImageBytes          = 4 << 20
	maxImageBase64         = 6 << 20
	maxSessionNameLen      = 200
	maxEntryIDBytes        = 256
	maxOutputPathBytes     = 32 << 10
	maxBranchTextRunes     = 400
	maxBranchLabelRunes    = 200
	maxExtensionUIResponse = 1 << 20
)

type agentRuntime interface {
	Start(context.Context, piruntime.StartConfig) (piruntime.SessionInfo, error)
	Call(context.Context, string, map[string]any) (pirpc.Response, error)
	Send(string, map[string]any) error
	Stop(string) error
	Diagnostics(string) (string, error)
	ActiveCount() int
	StopAll() error
	Shutdown()
}

func (service *AgentService) runningSessionCount() int {
	runtime, err := service.getRuntime()
	if err != nil {
		return 0
	}
	return runtime.ActiveCount()
}

type AgentService struct {
	locator *piruntime.Locator
	index   *sessionindex.Index

	mu            sync.RWMutex
	mutationMu    sync.Mutex
	maintenanceMu sync.RWMutex
	runtime       agentRuntime
}

func NewAgentService(locator *piruntime.Locator, index *sessionindex.Index) *AgentService {
	return &AgentService{locator: locator, index: index}
}

func newAgentService(runtime agentRuntime) *AgentService {
	return &AgentService{runtime: runtime}
}

func (service *AgentService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	app := application.Get()
	runtime := piruntime.NewSupervisor(ctx, piruntime.NewExecStarter(service.locator), func(event piruntime.SessionEvent) {
		app.Event.Emit(piEventName, event)
	})
	service.mu.Lock()
	service.runtime = runtime
	service.mu.Unlock()
	return nil
}

func (service *AgentService) ServiceShutdown() error {
	service.maintenanceMu.Lock()
	defer service.maintenanceMu.Unlock()
	service.mu.Lock()
	runtime := service.runtime
	service.runtime = nil
	service.mu.Unlock()
	if runtime != nil {
		runtime.Shutdown()
	}
	return nil
}

func (service *AgentService) StartSession(request domain.StartSessionRequest) (domain.LiveSession, error) {
	service.maintenanceMu.RLock()
	defer service.maintenanceMu.RUnlock()
	runtime, err := service.getRuntime()
	if err != nil {
		return domain.LiveSession{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sessionTimeout)
	defer cancel()

	info, err := runtime.Start(ctx, piruntime.StartConfig{
		ThreadID:       strings.TrimSpace(request.ThreadID),
		Workspace:      strings.TrimSpace(request.Workspace),
		SessionPath:    strings.TrimSpace(request.SessionPath),
		SessionName:    strings.TrimSpace(request.SessionName),
		Trust:          piruntime.TrustMode(request.Trust),
		NoSession:      request.NoSession,
		Offline:        request.Offline,
		DisableThemes:  request.DisableThemes,
		DisableSkills:  request.DisableSkills,
		DisablePlugins: request.DisablePlugins,
		ProxyURL:       strings.TrimSpace(request.ProxyURL),
	})
	if err != nil {
		return domain.LiveSession{}, err
	}
	return domain.LiveSession{
		ThreadID:   info.ThreadID,
		Generation: info.Generation,
		StateJSON:  string(info.State),
	}, nil
}

func (service *AgentService) preparePiMaintenance() (func(), error) {
	service.maintenanceMu.Lock()
	release := func() { service.maintenanceMu.Unlock() }
	runtime, err := service.getRuntime()
	if err != nil {
		release()
		return nil, err
	}
	if err := runtime.StopAll(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func (service *AgentService) StopSession(request domain.ThreadRequest) error {
	return service.stopThreadIfRunning(request.ThreadID)
}

func (service *AgentService) stopThreadIfRunning(threadID string) error {
	runtime, err := service.getRuntime()
	if err != nil {
		return err
	}
	err = runtime.Stop(strings.TrimSpace(threadID))
	if errors.Is(err, piruntime.ErrThreadNotRunning) {
		return nil
	}
	return err
}

func (service *AgentService) SendPrompt(request domain.PromptRequest) (domain.CommandResult, error) {
	message := strings.TrimSpace(request.Message)
	if message == "" && len(request.Images) == 0 {
		return domain.CommandResult{}, errors.New("prompt is required")
	}
	if len(message) > maxPromptBytes {
		return domain.CommandResult{}, errors.New("prompt exceeds the 1 MiB limit")
	}
	if err := validateImages(request.Images); err != nil {
		return domain.CommandResult{}, err
	}
	command := map[string]any{"type": "prompt", "message": message}
	if len(request.Images) > 0 {
		command["images"] = request.Images
	}
	switch request.StreamingBehavior {
	case "", "prompt":
	case "steer":
		command["streamingBehavior"] = "steer"
	case "followUp":
		command["streamingBehavior"] = "followUp"
	default:
		return domain.CommandResult{}, errors.New("invalid streaming behavior")
	}
	// Pi acknowledges prompts after preflight, which may include model-backed context compaction.
	return service.callWithoutDeadline(request.ThreadID, command)
}

func validateImages(images []domain.ImageContent) error {
	if len(images) > maxAttachedImages {
		return fmt.Errorf("a prompt can include at most %d images", maxAttachedImages)
	}
	totalBase64 := 0
	for _, image := range images {
		if image.Type != "image" {
			return errors.New("attachment type must be image")
		}
		switch image.MIMEType {
		case "image/png", "image/jpeg", "image/gif", "image/webp":
		default:
			return errors.New("unsupported image type")
		}
		totalBase64 += len(image.Data)
		if image.Data == "" || totalBase64 > maxImageBase64 {
			return errors.New("image attachments exceed the RPC size limit")
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(image.Data)
		if err != nil {
			return errors.New("image attachment is not valid base64")
		}
		if len(decoded) > maxImageBytes {
			return fmt.Errorf("each image must be %d MiB or smaller", maxImageBytes>>20)
		}
	}
	return nil
}

func (service *AgentService) Abort(request domain.ThreadRequest) (domain.CommandResult, error) {
	return service.call(request.ThreadID, map[string]any{"type": "abort"})
}

func (service *AgentService) SetAutoRetry(request domain.ToggleRequest) (domain.CommandResult, error) {
	return service.call(request.ThreadID, map[string]any{"type": "set_auto_retry", "enabled": request.Enabled})
}

func (service *AgentService) SetAutoCompaction(request domain.ToggleRequest) (domain.CommandResult, error) {
	return service.call(request.ThreadID, map[string]any{"type": "set_auto_compaction", "enabled": request.Enabled})
}

func (service *AgentService) SetSteeringMode(request domain.QueueModeRequest) (domain.CommandResult, error) {
	return service.setQueueMode(request, "set_steering_mode")
}

func (service *AgentService) SetFollowUpMode(request domain.QueueModeRequest) (domain.CommandResult, error) {
	return service.setQueueMode(request, "set_follow_up_mode")
}

func (service *AgentService) setQueueMode(request domain.QueueModeRequest, commandType string) (domain.CommandResult, error) {
	mode := strings.TrimSpace(request.Mode)
	if mode != "all" && mode != "one-at-a-time" {
		return domain.CommandResult{}, errors.New("queue mode must be all or one-at-a-time")
	}
	return service.call(request.ThreadID, map[string]any{"type": commandType, "mode": mode})
}

func (service *AgentService) AbortRetry(request domain.ThreadRequest) (domain.CommandResult, error) {
	return service.call(request.ThreadID, map[string]any{"type": "abort_retry"})
}

func (service *AgentService) Bash(request domain.BashRequest) (domain.CommandResult, error) {
	commandText := strings.TrimSpace(request.Command)
	if commandText == "" {
		return domain.CommandResult{}, errors.New("bash command is required")
	}
	if len(commandText) > maxPromptBytes {
		return domain.CommandResult{}, errors.New("bash command exceeds the 1 MiB limit")
	}
	return service.callWithTimeout(request.ThreadID, map[string]any{
		"type": "bash", "command": commandText, "excludeFromContext": request.ExcludeFromContext,
	}, longCommandTimeout)
}

func (service *AgentService) AbortBash(request domain.ThreadRequest) (domain.CommandResult, error) {
	return service.call(request.ThreadID, map[string]any{"type": "abort_bash"})
}

func (service *AgentService) GetState(request domain.ThreadRequest) (domain.CommandResult, error) {
	return service.call(request.ThreadID, map[string]any{"type": "get_state"})
}

func (service *AgentService) GetMessages(request domain.ThreadRequest) (domain.CommandResult, error) {
	return service.call(request.ThreadID, map[string]any{"type": "get_messages"})
}

func (service *AgentService) GetSessionStats(request domain.ThreadRequest) (domain.CommandResult, error) {
	return service.call(request.ThreadID, map[string]any{"type": "get_session_stats"})
}

func (service *AgentService) GetAvailableModels(request domain.ThreadRequest) (domain.CommandResult, error) {
	return service.call(request.ThreadID, map[string]any{"type": "get_available_models"})
}

func (service *AgentService) GetAvailableThinkingLevels(request domain.ThreadRequest) (domain.CommandResult, error) {
	return service.call(request.ThreadID, map[string]any{"type": "get_available_thinking_levels"})
}

func (service *AgentService) GetCommands(request domain.ThreadRequest) (domain.CommandResult, error) {
	return service.call(request.ThreadID, map[string]any{"type": "get_commands"})
}

func (service *AgentService) GetForkMessages(request domain.ThreadRequest) (domain.CommandResult, error) {
	return service.call(request.ThreadID, map[string]any{"type": "get_fork_messages"})
}

func (service *AgentService) GetSessionBranches(request domain.ThreadRequest) (domain.SessionBranches, error) {
	result, err := service.call(request.ThreadID, map[string]any{"type": "get_entries"})
	if err != nil {
		return domain.SessionBranches{}, err
	}
	return compactSessionBranches([]byte(result.DataJSON))
}

func compactSessionBranches(data []byte) (domain.SessionBranches, error) {
	var response struct {
		Entries []json.RawMessage `json:"entries"`
		LeafID  string            `json:"leafId"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return domain.SessionBranches{}, fmt.Errorf("decode Pi session entries: %w", err)
	}

	type parsedEntry struct {
		ID        string
		ParentID  string
		Type      string
		Timestamp string
		Role      string
		Text      string
	}
	parsed := make([]parsedEntry, 0, len(response.Entries))
	labels := make(map[string]string)
	for _, raw := range response.Entries {
		var entry struct {
			ID        string          `json:"id"`
			ParentID  string          `json:"parentId"`
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp"`
			TargetID  string          `json:"targetId"`
			Label     *string         `json:"label"`
			Message   json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(raw, &entry); err != nil {
			return domain.SessionBranches{}, fmt.Errorf("decode Pi session entry: %w", err)
		}
		if entry.Type == "label" && entry.TargetID != "" {
			if entry.Label == nil || strings.TrimSpace(*entry.Label) == "" {
				delete(labels, entry.TargetID)
			} else {
				labels[entry.TargetID] = truncateRunes(strings.TrimSpace(*entry.Label), maxBranchLabelRunes)
			}
		}
		role, text := compactBranchMessage(entry.Message)
		parsed = append(parsed, parsedEntry{
			ID: entry.ID, ParentID: entry.ParentID, Type: entry.Type, Timestamp: entry.Timestamp,
			Role: role, Text: text,
		})
	}

	result := domain.SessionBranches{Entries: make([]domain.SessionBranchEntry, 0, len(parsed)), LeafID: response.LeafID}
	for _, entry := range parsed {
		if entry.ID == "" {
			continue
		}
		result.Entries = append(result.Entries, domain.SessionBranchEntry{
			ID: entry.ID, ParentID: entry.ParentID, Type: entry.Type, Timestamp: entry.Timestamp,
			Role: entry.Role, Text: entry.Text, Label: labels[entry.ID],
		})
	}
	return result, nil
}

func compactBranchMessage(raw json.RawMessage) (string, string) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", ""
	}
	var message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &message) != nil {
		return "", ""
	}
	var text string
	if json.Unmarshal(message.Content, &text) != nil {
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(message.Content, &parts) == nil {
			var builder strings.Builder
			for _, part := range parts {
				if part.Type != "text" || part.Text == "" {
					continue
				}
				if builder.Len() > 0 {
					builder.WriteByte(' ')
				}
				builder.WriteString(part.Text)
			}
			text = builder.String()
		}
	}
	return message.Role, truncateRunes(text, maxBranchTextRunes)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func (service *AgentService) CloneSession(request domain.ThreadRequest) (domain.CommandResult, error) {
	return service.callWithTimeout(request.ThreadID, map[string]any{"type": "clone"}, longCommandTimeout)
}

func (service *AgentService) ForkSession(request domain.SessionForkRequest) (domain.CommandResult, error) {
	entryID := strings.TrimSpace(request.EntryID)
	if entryID == "" {
		return domain.CommandResult{}, errors.New("entry id is required")
	}
	if len(entryID) > maxEntryIDBytes {
		return domain.CommandResult{}, errors.New("entry id exceeds the safety limit")
	}
	return service.callWithTimeout(request.ThreadID, map[string]any{"type": "fork", "entryId": entryID}, longCommandTimeout)
}

func (service *AgentService) EditSessionMessage(request domain.SessionMessageRequest) (domain.CommandResult, error) {
	text := request.Text
	if strings.TrimSpace(text) == "" {
		return domain.CommandResult{}, errors.New("message text is required")
	}
	return service.mutateSessionMessage(request, func(path, entryID string) (sessionindex.Mutation, error) {
		return service.index.EditMessage(path, entryID, text)
	})
}

func (service *AgentService) DeleteSessionMessage(request domain.SessionMessageRequest) (domain.CommandResult, error) {
	return service.mutateSessionMessage(request, service.index.DeleteMessage)
}

func (service *AgentService) ForkSessionAt(request domain.SessionMessageRequest) (domain.CommandResult, error) {
	service.mutationMu.Lock()
	defer service.mutationMu.Unlock()
	path, entryID, err := service.editableSession(request)
	if err != nil {
		return domain.CommandResult{}, err
	}
	forked := ""
	selectedText := ""
	if request.Before {
		forkedResult, forkErr := service.index.ForkBefore(path, entryID)
		if forkErr != nil {
			return domain.CommandResult{}, forkErr
		}
		forked, selectedText = forkedResult.Path, forkedResult.Text
	} else {
		forked, err = service.index.ForkAt(path, entryID)
		if err != nil {
			return domain.CommandResult{}, err
		}
	}
	result, err := service.callWithTimeout(request.ThreadID, map[string]any{"type": "switch_session", "sessionPath": forked}, longCommandTimeout)
	if err != nil {
		_ = os.Remove(forked)
		return domain.CommandResult{}, err
	}
	if request.Before {
		var payload map[string]any
		if result.DataJSON == "" {
			payload = make(map[string]any)
		} else if err := json.Unmarshal([]byte(result.DataJSON), &payload); err != nil {
			return domain.CommandResult{}, fmt.Errorf("decode Pi session switch response: %w", err)
		}
		if payload == nil {
			payload = make(map[string]any)
		}
		payload["text"] = selectedText
		data, err := json.Marshal(payload)
		if err != nil {
			return domain.CommandResult{}, fmt.Errorf("encode fork response: %w", err)
		}
		result.DataJSON = string(data)
	}
	return result, nil
}

func (service *AgentService) mutateSessionMessage(request domain.SessionMessageRequest, mutate func(string, string) (sessionindex.Mutation, error)) (domain.CommandResult, error) {
	service.mutationMu.Lock()
	defer service.mutationMu.Unlock()
	path, entryID, err := service.editableSession(request)
	if err != nil {
		return domain.CommandResult{}, err
	}
	mutation, err := mutate(path, entryID)
	if err != nil {
		return domain.CommandResult{}, err
	}
	result, err := service.callWithTimeout(request.ThreadID, map[string]any{"type": "switch_session", "sessionPath": path}, longCommandTimeout)
	if err == nil {
		return result, nil
	}
	if restoreErr := service.index.RestoreMutation(mutation); restoreErr != nil {
		return domain.CommandResult{}, fmt.Errorf("reload edited session: %v; restore backup: %w", err, restoreErr)
	}
	_, _ = service.callWithTimeout(request.ThreadID, map[string]any{"type": "switch_session", "sessionPath": path}, longCommandTimeout)
	return domain.CommandResult{}, fmt.Errorf("reload edited session: %w", err)
}

func (service *AgentService) editableSession(request domain.SessionMessageRequest) (string, string, error) {
	if service.index == nil {
		return "", "", errors.New("session mutation service is unavailable")
	}
	entryID := strings.TrimSpace(request.EntryID)
	if entryID == "" || len(entryID) > maxEntryIDBytes {
		return "", "", errors.New("a valid entry id is required")
	}
	path, err := service.index.ValidatePath(strings.TrimSpace(request.Path))
	if err != nil {
		return "", "", err
	}
	state, err := service.GetState(domain.ThreadRequest{ThreadID: request.ThreadID})
	if err != nil {
		return "", "", err
	}
	var current struct {
		SessionFile  string `json:"sessionFile"`
		IsStreaming  bool   `json:"isStreaming"`
		IsCompacting bool   `json:"isCompacting"`
	}
	if err := json.Unmarshal([]byte(state.DataJSON), &current); err != nil {
		return "", "", errors.New("Pi returned an invalid session state")
	}
	if current.IsStreaming || current.IsCompacting {
		return "", "", errors.New("wait for the current Pi turn to finish")
	}
	currentPath, err := service.index.ValidatePath(current.SessionFile)
	if err != nil || !sameSessionPath(currentPath, path) {
		return "", "", errors.New("message does not belong to the active Pi session")
	}
	return path, entryID, nil
}

func sameSessionPath(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func (service *AgentService) ExportSession(request domain.ExportSessionRequest) (domain.CommandResult, error) {
	command := map[string]any{"type": "export_html"}
	outputPath := strings.TrimSpace(request.OutputPath)
	if outputPath != "" {
		if len(outputPath) > maxOutputPathBytes {
			return domain.CommandResult{}, errors.New("export path exceeds the safety limit")
		}
		if !filepath.IsAbs(outputPath) {
			return domain.CommandResult{}, errors.New("export path must be absolute")
		}
		if !strings.EqualFold(filepath.Ext(outputPath), ".html") {
			return domain.CommandResult{}, errors.New("export path must use the .html extension")
		}
		command["outputPath"] = filepath.Clean(outputPath)
	}
	return service.callWithTimeout(request.ThreadID, command, longCommandTimeout)
}

func (service *AgentService) SetModel(request domain.ModelRequest) (domain.CommandResult, error) {
	if strings.TrimSpace(request.Provider) == "" || strings.TrimSpace(request.ModelID) == "" {
		return domain.CommandResult{}, errors.New("provider and model are required")
	}
	return service.call(request.ThreadID, map[string]any{
		"type": "set_model", "provider": strings.TrimSpace(request.Provider), "modelId": strings.TrimSpace(request.ModelID),
	})
}

func (service *AgentService) SetThinkingLevel(request domain.ThinkingRequest) (domain.CommandResult, error) {
	level := strings.TrimSpace(request.Level)
	if level == "" {
		return domain.CommandResult{}, errors.New("thinking level is required")
	}
	return service.call(request.ThreadID, map[string]any{"type": "set_thinking_level", "level": level})
}

func (service *AgentService) Compact(request domain.CompactRequest) (domain.CommandResult, error) {
	command := map[string]any{"type": "compact"}
	if instructions := strings.TrimSpace(request.CustomInstructions); instructions != "" {
		command["customInstructions"] = instructions
	}
	return service.call(request.ThreadID, command)
}

func (service *AgentService) SetSessionName(request domain.SessionNameRequest) (domain.CommandResult, error) {
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return domain.CommandResult{}, errors.New("session name is required")
	}
	if len(name) > maxSessionNameLen {
		return domain.CommandResult{}, fmt.Errorf("session name exceeds %d characters", maxSessionNameLen)
	}
	return service.call(request.ThreadID, map[string]any{"type": "set_session_name", "name": name})
}

func (service *AgentService) RespondExtensionUI(request domain.ExtensionUIResponseRequest) error {
	runtime, err := service.getRuntime()
	if err != nil {
		return err
	}
	threadID := strings.TrimSpace(request.ThreadID)
	requestID := strings.TrimSpace(request.RequestID)
	if threadID == "" || requestID == "" {
		return errors.New("thread id and extension request id are required")
	}
	if len(requestID) > maxEntryIDBytes {
		return fmt.Errorf("extension request id exceeds %d bytes", maxEntryIDBytes)
	}
	if len(request.Value) > maxExtensionUIResponse {
		return fmt.Errorf("extension UI response exceeds %d bytes", maxExtensionUIResponse)
	}
	response := map[string]any{"type": "extension_ui_response", "id": requestID}
	switch {
	case request.Cancelled:
		response["cancelled"] = true
	case request.Confirmed != nil:
		response["confirmed"] = *request.Confirmed
	default:
		response["value"] = request.Value
	}
	return runtime.Send(threadID, response)
}

func (service *AgentService) GetDiagnostics(request domain.ThreadRequest) (string, error) {
	runtime, err := service.getRuntime()
	if err != nil {
		return "", err
	}
	return runtime.Diagnostics(strings.TrimSpace(request.ThreadID))
}

func (service *AgentService) call(threadID string, command map[string]any) (domain.CommandResult, error) {
	return service.callWithTimeout(threadID, command, commandTimeout)
}

func (service *AgentService) callWithoutDeadline(threadID string, command map[string]any) (domain.CommandResult, error) {
	return service.callWithContext(context.Background(), threadID, command)
}

func (service *AgentService) callWithTimeout(threadID string, command map[string]any, timeout time.Duration) (domain.CommandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return service.callWithContext(ctx, threadID, command)
}

func (service *AgentService) callWithContext(ctx context.Context, threadID string, command map[string]any) (domain.CommandResult, error) {
	runtime, err := service.getRuntime()
	if err != nil {
		return domain.CommandResult{}, err
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return domain.CommandResult{}, errors.New("thread id is required")
	}
	response, err := runtime.Call(ctx, threadID, command)
	if err != nil {
		return domain.CommandResult{}, err
	}
	return domain.CommandResult{Command: response.Command, DataJSON: string(response.Data)}, nil
}

func (service *AgentService) getRuntime() (agentRuntime, error) {
	service.mu.RLock()
	runtime := service.runtime
	service.mu.RUnlock()
	if runtime == nil {
		return nil, errors.New("Pi agent service is not ready")
	}
	return runtime, nil
}
