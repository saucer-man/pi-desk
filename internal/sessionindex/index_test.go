package sessionindex

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"pi-desk/internal/workspace"
)

func canonicalTestPath(t *testing.T, path string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(canonical)
}

func TestIndexListsAndFiltersPiSessions(t *testing.T) {
	root := t.TempDir()
	workspaceA := filepath.Join(root, "workspace-a")
	workspaceB := filepath.Join(root, "workspace-b")
	for _, path := range []string{workspaceA, workspaceB, filepath.Join(root, "sessions", "a"), filepath.Join(root, "sessions", "b")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	workspaceA = canonicalTestPath(t, workspaceA)
	workspaceB = canonicalTestPath(t, workspaceB)
	writeSession(t, filepath.Join(root, "sessions", "a", "one.jsonl"), strings.Join([]string{
		`{"type":"session","version":3,"id":"session-a","timestamp":"2026-08-10T08:00:00Z","cwd":` + quote(workspaceA) + `,"parentSession":"parent.jsonl"}`,
		`{"type":"message","id":"m1","timestamp":"2026-08-10T08:01:00Z","message":{"role":"user","content":[{"type":"text","text":"  Inspect   the repository  "}],"timestamp":1786348860000}}`,
		`not-json`,
		`{"type":"session_info","id":"n1","timestamp":"2026-08-10T08:02:00Z","name":"Runtime audit"}`,
		`{"type":"message","id":"m2","timestamp":"2026-08-10T08:03:00Z","message":{"role":"assistant","content":"Done"}}`,
	}, "\n")+"\n")
	writeSession(t, filepath.Join(root, "sessions", "b", "two.jsonl"),
		`{"type":"session","version":3,"id":"session-b","timestamp":"2026-08-09T08:00:00Z","cwd":`+quote(workspaceB)+"}\n")

	summaries, err := New(filepath.Join(root, "sessions")).List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 || summaries[0].ID != "session-a" {
		t.Fatalf("unexpected summaries: %#v", summaries)
	}
	first := summaries[0]
	if first.Title != "Runtime audit" || first.FirstMessage != "Inspect the repository" || first.MessageCount != 2 || first.ParentSessionPath != "parent.jsonl" {
		t.Fatalf("unexpected summary: %#v", first)
	}
	wantModified := time.Date(2026, 8, 10, 8, 3, 0, 0, time.UTC)
	if !first.ModifiedAt.Equal(wantModified) {
		t.Fatalf("modified = %s, want %s", first.ModifiedAt, wantModified)
	}

	filtered, err := New(filepath.Join(root, "sessions")).List(context.Background(), workspaceB)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].ID != "session-b" || filtered[0].Title != "Empty session" {
		t.Fatalf("unexpected filtered summaries: %#v", filtered)
	}
}

func TestIndexProjectsSSHAnchorsAndListsOnlyUnknownOrphans(t *testing.T) {
	root := t.TempDir()
	sessionsRoot := filepath.Join(root, "sessions")
	anchorRoot := filepath.Join(root, "remote-anchors")
	knownID := "workspace-0123456789abcdef0123456789abcdef"
	orphanID := "workspace-fedcba9876543210fedcba9876543210"
	knownAnchor, err := workspace.EnsureSSHAnchor(anchorRoot, knownID)
	if err != nil {
		t.Fatal(err)
	}
	orphanAnchor, err := workspace.EnsureSSHAnchor(anchorRoot, orphanID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		directory string
		filename  string
		id        string
		cwd       string
	}{
		{directory: "known", filename: "known.jsonl", id: "known-session", cwd: knownAnchor},
		{directory: "orphan", filename: "orphan.jsonl", id: "orphan-session", cwd: orphanAnchor},
		{directory: "local", filename: "local.jsonl", id: "local-session", cwd: root},
	} {
		directory := filepath.Join(sessionsRoot, item.directory)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		writeSession(t, filepath.Join(directory, item.filename), `{"type":"session","version":3,"id":"`+item.id+`","timestamp":"2026-08-10T08:00:00Z","cwd":`+quote(item.cwd)+"}\n")
	}
	index := NewWithAnchorRoot(sessionsRoot, anchorRoot)
	summaries, err := index.List(context.Background(), "")
	if err != nil || len(summaries) != 3 {
		t.Fatalf("anchor summaries = %#v, %v", summaries, err)
	}
	byID := make(map[string]Summary)
	for _, summary := range summaries {
		byID[summary.ID] = summary
	}
	if !byID["known-session"].SSHAnchor || byID["known-session"].AnchorWorkspaceID != knownID || !byID["orphan-session"].SSHAnchor || byID["local-session"].SSHAnchor {
		t.Fatalf("unexpected anchor projection: %#v", byID)
	}
	orphans, err := index.ListOrphanSSH(context.Background(), map[string]struct{}{knownID: {}})
	if err != nil || len(orphans) != 1 || orphans[0].ID != "orphan-session" || orphans[0].AnchorWorkspaceID != orphanID {
		t.Fatalf("orphan projection = %#v, %v", orphans, err)
	}
	resolved, err := index.Resolve(byID["orphan-session"].Path)
	if err != nil || !resolved.SSHAnchor || resolved.AnchorWorkspaceID != orphanID {
		t.Fatalf("resolved anchor = %#v, %v", resolved, err)
	}
}

func TestIndexKeepsLocalSessionOnDifferentWindowsVolume(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows volume semantics only")
	}
	index := NewWithAnchorRoot(`C:\pi-desk\remote-anchors`, `C:\pi-desk\remote-anchors`)
	summary := Summary{CWD: `D:\repo`}
	if !index.projectSSHAnchor(&summary) || summary.SSHAnchor {
		t.Fatalf("cross-volume local session was filtered: %#v", summary)
	}
}

func TestIndexFailsClosedForMalformedConfiguredAnchor(t *testing.T) {
	root := t.TempDir()
	sessionsRoot := filepath.Join(root, "sessions")
	anchorRoot := filepath.Join(root, "remote-anchors")
	workspaceID := "workspace-0123456789abcdef0123456789abcdef"
	anchor, err := workspace.EnsureSSHAnchor(anchorRoot, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(anchor, "unexpected"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(sessionsRoot, "bad")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(directory, "bad.jsonl")
	writeSession(t, sessionPath, `{"type":"session","version":3,"id":"bad-session","timestamp":"2026-08-10T08:00:00Z","cwd":`+quote(anchor)+"}\n")
	index := NewWithAnchorRoot(sessionsRoot, anchorRoot)
	summaries, err := index.List(context.Background(), "")
	if err != nil || len(summaries) != 0 {
		t.Fatalf("malformed anchor was listed: %#v, %v", summaries, err)
	}
	if _, err := index.Resolve(sessionPath); err == nil {
		t.Fatal("malformed anchor session resolved")
	}
}

func TestIndexIgnoresNamesCorruptedByWindowsCommandQuoting(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "quoted-name.jsonl")
	writeSession(t, path, strings.Join([]string{
		`{"type":"session","version":3,"id":"quoted","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`,
		`{"type":"message","id":"user","parentId":null,"timestamp":"2026-08-10T08:01:00Z","message":{"role":"user","content":"Create test.txt and write hello world"}}`,
		`{"type":"message","id":"assistant","parentId":"user","timestamp":"2026-08-10T08:02:00Z","message":{"role":"assistant","content":"Done"}}`,
		`{"type":"session_info","id":"name-1","parentId":"assistant","timestamp":"2026-08-10T08:03:00Z","name":"\"Create test.txt"}`,
		`{"type":"session_info","id":"name-2","parentId":"name-1","timestamp":"2026-08-10T08:04:00Z","name":"\"\"\"Create test.txt\""}`,
	}, "\n")+"\n")

	summary, err := New(root).Resolve(path)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Name != "" || summary.Title != "Create test.txt and write hello world" {
		t.Fatalf("corrupted automatic name was not repaired: %#v", summary)
	}
}

func TestIndexProjectsExpandedSkillUserTextIntoSessionTitle(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	expanded := "<skill name=\"grill-me\" location=\"D:\\\\skills\\\\grill-me\\\\SKILL.md\">\r\nInternal skill instructions.\r\n</skill>\r\n\r\nReview the image generation plan."
	path := filepath.Join(directory, "skill.jsonl")
	writeSession(t, path, strings.Join([]string{
		`{"type":"session","version":3,"id":"skill","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`,
		`{"type":"message","id":"user","timestamp":"2026-08-10T08:01:00Z","message":{"role":"user","content":` + strconv.Quote(expanded) + `}}`,
	}, "\n")+"\n")

	summary, err := New(root).Resolve(path)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Name != "" || summary.FirstMessage != "Review the image generation plan." || summary.Title != summary.FirstMessage {
		t.Fatalf("expanded skill transport leaked into summary: %#v", summary)
	}

	if got := sessionTitleText("<skill name=\"grill-me\" location=\"/skills/grill/SKILL.md\">\nInstructions\n</skill>"); got != "/skill:grill-me" {
		t.Fatalf("skill-only title = %q", got)
	}
	ordinary := "<skill name=\"not-a-transport-block\">ordinary user text</skill>"
	if got := sessionTitleText(ordinary); got != ordinary {
		t.Fatalf("ordinary XML was modified: %q", got)
	}
}

func TestIndexSkipsMalformedOversizedAndSymlinkedEntries(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	validDir := filepath.Join(sessions, "valid")
	if err := os.MkdirAll(validDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSession(t, filepath.Join(validDir, "bad-header.jsonl"), "{}\n")
	writeSession(t, filepath.Join(validDir, "long-line.jsonl"), strings.Repeat("x", maxLineBytes+1)+"\n")
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSession(t, filepath.Join(outside, "outside.jsonl"), `{"type":"session","id":"outside","timestamp":"2026-08-10T08:00:00Z","cwd":"x"}`+"\n")
	if err := os.Symlink(outside, filepath.Join(sessions, "linked")); err != nil {
		t.Logf("symlink unavailable: %v", err)
	}

	summaries, err := New(sessions).List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 0 {
		t.Fatalf("unsafe or malformed sessions were indexed: %#v", summaries)
	}
}

func TestIndexHonorsCancellationAndMissingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	summaries, err := New(root).List(context.Background(), "")
	if err != nil || len(summaries) != 0 {
		t.Fatalf("missing root = %#v, %v", summaries, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(t.TempDir()).List(ctx, ""); err == nil {
		t.Fatal("expected cancellation")
	}
}

func TestIndexAggregatesPersistedUsageFromActiveBranches(t *testing.T) {
	root := t.TempDir()
	workspaceA := filepath.Join(root, "workspace-a")
	workspaceB := filepath.Join(root, "workspace-b")
	for _, path := range []string{workspaceA, workspaceB, filepath.Join(root, "sessions", "a"), filepath.Join(root, "sessions", "b")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	workspaceA = canonicalTestPath(t, workspaceA)
	workspaceB = canonicalTestPath(t, workspaceB)
	writeSession(t, filepath.Join(root, "sessions", "a", "one.jsonl"), strings.Join([]string{
		`{"type":"session","version":3,"id":"one","timestamp":"2026-08-10T08:00:00Z","cwd":` + quote(workspaceA) + `}`,
		`{"type":"message","id":"user","parentId":null,"message":{"role":"user","content":"Inspect"}}`,
		`{"type":"message","id":"old","parentId":"user","message":{"role":"assistant","provider":"openai","model":"old-branch","content":"Old","usage":{"input":900,"output":90,"cacheRead":0,"cacheWrite":0,"reasoning":20,"totalTokens":990,"cost":{"total":9.9}}}}`,
		`{"type":"message","id":"active","parentId":"user","message":{"role":"assistant","provider":"openai","model":"gpt-5","content":"Active","usage":{"input":100,"output":20,"cacheRead":30,"cacheWrite":4,"reasoning":8,"totalTokens":154,"cost":{"total":0.25}}}}`,
		`{"type":"message","id":"tool","parentId":"active","message":{"role":"toolResult","content":"Done"}}`,
	}, "\n")+"\n")
	writeSession(t, filepath.Join(root, "sessions", "b", "two.jsonl"), strings.Join([]string{
		`{"type":"session","version":3,"id":"two","timestamp":"2026-08-10T08:00:00Z","cwd":` + quote(workspaceB) + `}`,
		`{"type":"message","id":"assistant","parentId":null,"message":{"role":"assistant","provider":"deepseek","model":"deepseek-v3","content":"Other","usage":{"input":40,"output":10,"cacheRead":0,"cacheWrite":0,"reasoning":5,"totalTokens":50,"cost":{"total":0.05}}}}`,
	}, "\n")+"\n")

	all, err := New(filepath.Join(root, "sessions")).Usage(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if all.Sessions != 2 || all.Messages != 4 || all.UserMessages != 1 || all.AssistantMessages != 2 || all.ToolResults != 1 {
		t.Fatalf("unexpected message usage: %#v", all)
	}
	if all.Tokens != (TokenUsage{Input: 140, Output: 30, CacheRead: 30, CacheWrite: 4, Reasoning: 13, Total: 204}) || math.Abs(all.Cost-0.3) > 1e-9 {
		t.Fatalf("unexpected token usage: %#v", all)
	}
	if len(all.Models) != 2 || all.Models[0].Provider != "openai" || all.Models[0].Model != "gpt-5" || all.Models[0].Tokens.Total != 154 {
		t.Fatalf("unexpected model usage: %#v", all.Models)
	}

	filtered, err := New(filepath.Join(root, "sessions")).Usage(context.Background(), workspaceA)
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Sessions != 1 || filtered.Tokens.Total != 154 || len(filtered.Models) != 1 || filtered.Models[0].Model != "gpt-5" {
		t.Fatalf("unexpected filtered usage: %#v", filtered)
	}
}

func TestIndexUsageKeepsPreCompactionProviderCost(t *testing.T) {
	root := t.TempDir()
	workspacePath := filepath.Join(root, "workspace")
	directory := filepath.Join(root, "sessions", "project")
	for _, path := range []string{workspacePath, directory} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	workspacePath = canonicalTestPath(t, workspacePath)
	writeSession(t, filepath.Join(directory, "compacted.jsonl"), strings.Join([]string{
		`{"type":"session","version":3,"id":"compacted","timestamp":"2026-08-10T08:00:00Z","cwd":` + quote(workspacePath) + `}`,
		`{"type":"message","id":"old","parentId":null,"message":{"role":"assistant","provider":"openai","model":"gpt-5","content":"Old","usage":{"input":80,"output":20,"totalTokens":100,"cost":{"total":0.1}}}}`,
		`{"type":"compaction","id":"compact","parentId":"old","summary":"summary","tokensBefore":100}`,
		`{"type":"message","id":"new","parentId":"compact","message":{"role":"assistant","provider":"openai","model":"gpt-5","content":"New","usage":{"input":40,"output":10,"totalTokens":50,"cost":{"total":0.05}}}}`,
	}, "\n")+"\n")

	usage, err := New(filepath.Join(root, "sessions")).Usage(context.Background(), workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	if usage.AssistantMessages != 2 || usage.Tokens.Total != 150 || math.Abs(usage.Cost-0.15) > 1e-9 {
		t.Fatalf("compaction dropped consumed usage: %#v", usage)
	}
}

func TestIndexPreservesMissingLegacyCWD(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "legacy")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSession(t, filepath.Join(directory, "legacy.jsonl"),
		`{"type":"session","id":"legacy","timestamp":"2026-08-10T08:00:00Z","cwd":""}`+"\n")
	summaries, err := New(root).List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].CWD != "" {
		t.Fatalf("legacy cwd should stay empty: %#v", summaries)
	}
}

func TestIndexValidatesAndResolvesSessionPathsWithinRoot(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "valid.jsonl")
	writeSession(t, path, `{"type":"session","id":"valid","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`+"\n")
	index := New(root)

	validated, err := index.ValidatePath(path)
	if err != nil || validated != canonicalTestPath(t, path) {
		t.Fatalf("validated path = %q, %v", validated, err)
	}
	summary, err := index.Resolve(path)
	if err != nil || summary.ID != "valid" {
		t.Fatalf("resolved summary = %#v, %v", summary, err)
	}
	header, err := index.Header(path)
	if err != nil || header.ID != "valid" || header.CWD != `D:\repo` {
		t.Fatalf("resolved header = %#v, %v", header, err)
	}

	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	writeSession(t, outside, `{"type":"session","id":"outside","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`+"\n")
	if _, err := index.ValidatePath(outside); err == nil {
		t.Fatal("expected outside-root validation error")
	}
	textPath := filepath.Join(directory, "not-jsonl.txt")
	writeSession(t, textPath, "text\n")
	if _, err := index.ValidatePath(textPath); err == nil {
		t.Fatal("expected extension validation error")
	}
}

func TestCopyValidatedCopiesOnlyStableSessionFile(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "copy.jsonl")
	content := `{"type":"session","version":3,"id":"copy","timestamp":"2026-08-10T08:00:00Z","cwd":"/tmp"}` + "\n"
	writeSession(t, path, content)
	var copied strings.Builder
	if err := New(root).CopyValidated(path, &copied); err != nil || copied.String() != content {
		t.Fatalf("copy = %q, %v", copied.String(), err)
	}
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	writeSession(t, outside, content)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err == nil {
		if err := New(root).CopyValidated(path, &copied); err == nil {
			t.Fatal("symlink session was copied")
		}
	}
}

func TestHeaderDoesNotScanTranscriptBody(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "large-body.jsonl")
	writeSession(t, path, `{"type":"session","id":"valid","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`+"\n"+strings.Repeat("x", maxLineBytes+1)+"\n")

	header, err := New(root).Header(path)
	if err != nil || header.ID != "valid" {
		t.Fatalf("header = %#v, %v", header, err)
	}
	if _, err := New(root).Resolve(path); err == nil {
		t.Fatal("expected full transcript resolution to reject an oversized body line")
	}
}

func TestIndexReadsOnlyTheActiveSessionBranch(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "branch.jsonl")
	writeSession(t, path, strings.Join([]string{
		`{"type":"session","version":3,"id":"branch","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`,
		`{"type":"message","id":"root","parentId":null,"timestamp":"2026-08-10T08:01:00Z","message":{"role":"user","content":"Root"}}`,
		`{"type":"message","id":"old","parentId":"root","timestamp":"2026-08-10T08:02:00Z","message":{"role":"assistant","content":"Old branch"}}`,
		`{"type":"message","id":"active","parentId":"root","timestamp":"2026-08-10T08:03:00Z","message":{"role":"assistant","content":"Active branch"}}`,
	}, "\n")+"\n")

	messages, err := New(root).Messages(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := messageContents(t, messages); !slices.Equal(got, []string{"Root", "Active branch"}) {
		t.Fatalf("active messages = %#v", got)
	}
}

func TestIndexRestoresModelFromTheActiveSessionBranch(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "models.jsonl")
	writeSession(t, path, strings.Join([]string{
		`{"type":"session","version":3,"id":"models","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`,
		`{"type":"message","id":"root","parentId":null,"timestamp":"2026-08-10T08:01:00Z","message":{"role":"assistant","provider":"openai","model":"gpt-4.1","content":"Root"}}`,
		`{"type":"model_change","id":"old","parentId":"root","timestamp":"2026-08-10T08:02:00Z","provider":"anthropic","modelId":"claude-old"}`,
		`{"type":"model_change","id":"active","parentId":"root","timestamp":"2026-08-10T08:03:00Z","provider":"openai","modelId":"gpt-5"}`,
	}, "\n")+"\n")

	snapshot, err := New(root).Snapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Model == nil || snapshot.Model.Provider != "openai" || snapshot.Model.ID != "gpt-5" {
		t.Fatalf("model = %#v", snapshot.Model)
	}
}

func TestIndexReadsFullActiveBranchAcrossCompaction(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "compacted.jsonl")
	writeSession(t, path, strings.Join([]string{
		`{"type":"session","version":3,"id":"compacted","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`,
		`{"type":"message","id":"old","parentId":null,"timestamp":"2026-08-10T08:01:00Z","message":{"role":"user","content":"Old"}}`,
		`{"type":"message","id":"kept","parentId":"old","timestamp":"2026-08-10T08:02:00Z","message":{"role":"user","content":"Kept"}}`,
		`{"type":"compaction","id":"compact","parentId":"kept","timestamp":"2026-08-10T08:03:00Z","summary":"summary","firstKeptEntryId":"kept","tokensBefore":1000}`,
		`{"type":"message","id":"new","parentId":"compact","timestamp":"2026-08-10T08:04:00Z","message":{"role":"assistant","content":"New"}}`,
	}, "\n")+"\n")

	messages, err := New(root).Messages(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := messageContents(t, messages); !slices.Equal(got, []string{"Old", "Kept", "New"}) {
		t.Fatalf("visible messages = %#v", got)
	}
	if len(messages) != 4 {
		t.Fatalf("visible timeline entries = %d", len(messages))
	}
	var marker struct {
		Role                 string `json:"role"`
		EntryID              string `json:"piDeskEntryId"`
		Timestamp            string `json:"timestamp"`
		Summary              string `json:"summary"`
		TokensBefore         int64  `json:"tokensBefore"`
		EstimatedTokensAfter int64  `json:"estimatedTokensAfter"`
	}
	if err := json.Unmarshal(messages[2], &marker); err != nil {
		t.Fatal(err)
	}
	if marker.Role != "piDeskCompaction" || marker.EntryID != "compact" || marker.Timestamp != "2026-08-10T08:03:00Z" || marker.Summary != "summary" || marker.TokensBefore != 1000 || marker.EstimatedTokensAfter != 3 {
		t.Fatalf("compaction marker = %#v", marker)
	}
}

func TestCompactionEstimateMatchesPiCharacterHeuristic(t *testing.T) {
	entries := []rawEntry{
		{
			Type: "message", ID: "kept",
			Message: json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"abcd"},{"type":"thinking","thinking":"😀"},{"type":"toolCall","name":"read","arguments":{"path":"a"}}]}`),
		},
		{
			Type: "message", ID: "tool",
			Message: json.RawMessage(`{"role":"toolResult","content":[{"type":"text","text":"done"},{"type":"image","data":"ignored"}]}`),
		},
		{
			Type: "compaction", ID: "compact", FirstKeptEntryID: "kept", Summary: "摘要",
		},
	}

	estimated := addCompactionEstimates(entries)[2].EstimatedTokensAfter
	if estimated == nil || *estimated != 1208 {
		t.Fatalf("estimated tokens after = %v", estimated)
	}
}

func TestIndexReadsLegacyLinearMessages(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "legacy.jsonl")
	writeSession(t, path, strings.Join([]string{
		`{"type":"session","id":"legacy","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`,
		`{"type":"message","timestamp":"2026-08-10T08:01:00Z","message":{"role":"user","content":"One"}}`,
		`{"type":"message","timestamp":"2026-08-10T08:02:00Z","message":{"role":"assistant","content":"Two"}}`,
	}, "\n")+"\n")

	messages, err := New(root).Messages(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := messageContents(t, messages); !slices.Equal(got, []string{"One", "Two"}) {
		t.Fatalf("legacy messages = %#v", got)
	}
	var first struct {
		EntryID   string `json:"piDeskEntryId"`
		DisplayID string `json:"piDeskDisplayId"`
	}
	if json.Unmarshal(messages[0], &first) != nil || first.EntryID != "" || first.DisplayID != "legacy-00000001" {
		t.Fatalf("legacy display identity = %#v", first)
	}
}

func TestSnapshotIncludesStableEntryIDs(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "entries.jsonl")
	writeSession(t, path, strings.Join([]string{
		`{"type":"session","version":3,"id":"entries","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`,
		`{"type":"message","id":"user-1","parentId":null,"timestamp":"2026-08-10T08:01:00Z","message":{"role":"user","content":"One"}}`,
	}, "\n")+"\n")

	snapshot, err := New(root).Snapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	var message struct {
		EntryID   string `json:"piDeskEntryId"`
		Timestamp string `json:"timestamp"`
	}
	if len(snapshot.Messages) != 1 || json.Unmarshal(snapshot.Messages[0], &message) != nil || message.EntryID != "user-1" || message.Timestamp != "2026-08-10T08:01:00Z" {
		t.Fatalf("snapshot message = %s", snapshot.Messages)
	}
}

func TestIndexEditsMessageTextAndPreservesStructuredBlocks(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "edit.jsonl")
	writeSession(t, path, strings.Join([]string{
		`{"type":"session","version":3,"id":"edit","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`,
		`{"type":"message","id":"assistant-1","parentId":null,"message":{"role":"assistant","content":[{"type":"thinking","thinking":"keep"},{"type":"text","text":"Before"},{"type":"toolCall","id":"tool-1","name":"read"}]}}`,
	}, "\n")+"\n")

	mutation, err := New(root).EditMessage(path, "assistant-1", "After")
	if err != nil {
		t.Fatal(err)
	}
	if mutation.BackupPath == "" {
		t.Fatal("expected a recoverable backup")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"text":"After"`) || !strings.Contains(text, `"thinking":"keep"`) || !strings.Contains(text, `"name":"read"`) {
		t.Fatalf("edited session = %s", text)
	}
}

func TestIndexDeletesMessageAndReparentsChildren(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "delete.jsonl")
	writeSession(t, path, strings.Join([]string{
		`{"type":"session","version":3,"id":"delete","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`,
		`{"type":"message","id":"root","parentId":null,"message":{"role":"user","content":"Root"}}`,
		`{"type":"message","id":"remove","parentId":"root","message":{"role":"assistant","content":"Remove"}}`,
		`{"type":"message","id":"child","parentId":"remove","message":{"role":"user","content":"Child"}}`,
	}, "\n")+"\n")

	if _, err := New(root).DeleteMessage(path, "remove"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `"id":"remove"`) {
		t.Fatalf("deleted session still contains the target: %s", text)
	}
	entries, err := readTranscriptEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	child := slices.IndexFunc(entries, func(entry rawEntry) bool { return entry.ID == "child" })
	if child < 0 || entries[child].ParentID == nil || *entries[child].ParentID != "root" {
		t.Fatalf("child was not reparented: %#v", entries)
	}
}

func TestIndexDeletesCompactionBoundaryAndMovesFirstKeptEntry(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "delete-compaction-boundary.jsonl")
	writeSession(t, path, strings.Join([]string{
		`{"type":"session","version":3,"id":"delete","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`,
		`{"type":"message","id":"root","parentId":null,"message":{"role":"user","content":"Root"}}`,
		`{"type":"message","id":"remove","parentId":"root","message":{"role":"assistant","content":"Remove"}}`,
		`{"type":"message","id":"kept","parentId":"remove","message":{"role":"user","content":"Kept"}}`,
		`{"type":"compaction","id":"compact","parentId":"kept","summary":"Summary","firstKeptEntryId":"remove","tokensBefore":1000}`,
		`{"type":"message","id":"new","parentId":"compact","message":{"role":"assistant","content":"New"}}`,
	}, "\n")+"\n")

	index := New(root)
	if _, err := index.DeleteMessage(path, "remove"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"firstKeptEntryId":"kept"`) {
		t.Fatalf("compaction boundary was not moved: %s", data)
	}
	messages, err := index.Messages(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := messageContents(t, messages); !slices.Equal(got, []string{"Root", "Kept", "New"}) {
		t.Fatalf("visible messages after deletion = %#v", got)
	}
}

func TestIndexDeletesAssistantToolResultsAsPartOfDisplayedMessage(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "delete-tools.jsonl")
	writeSession(t, path, strings.Join([]string{
		`{"type":"session","version":3,"id":"delete","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`,
		`{"type":"message","id":"root","parentId":null,"message":{"role":"user","content":"Root"}}`,
		`{"type":"message","id":"assistant","parentId":"root","message":{"role":"assistant","content":[{"type":"toolCall","id":"tool-1","name":"read"}]}}`,
		`{"type":"message","id":"result-1","parentId":"assistant","message":{"role":"toolResult","toolCallId":"tool-1","content":"output"}}`,
		`{"type":"message","id":"later","parentId":"result-1","message":{"role":"assistant","content":"Later"}}`,
	}, "\n")+"\n")

	if _, err := New(root).DeleteMessage(path, "assistant"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `"id":"assistant"`) || strings.Contains(text, `"id":"result-1"`) {
		t.Fatalf("deleted tool message left structured children: %s", text)
	}
	entries, err := readTranscriptEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	later := slices.IndexFunc(entries, func(entry rawEntry) bool { return entry.ID == "later" })
	if later < 0 || entries[later].ParentID == nil || *entries[later].ParentID != "root" {
		t.Fatalf("later message was not reparented: %#v", entries)
	}
}

func TestIndexForkBeforeCreatesPersistedEmptyBranch(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "fork-before.jsonl")
	writeSession(t, path, strings.Join([]string{
		`{"type":"session","version":3,"id":"fork-before","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`,
		`{"type":"message","id":"root","parentId":null,"message":{"role":"user","content":"Continue this task"}}`,
	}, "\n")+"\n")

	result, err := New(root).ForkBefore(path, "root")
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Continue this task" {
		t.Fatalf("selected text = %q", result.Text)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, "\n") != 1 || strings.Contains(text, `"id":"root"`) || !strings.Contains(text, `"parentSession":`+quote(canonicalTestPath(t, path))) {
		t.Fatalf("empty fork = %s", text)
	}
	if _, err := New(root).Snapshot(result.Path); err != nil {
		t.Fatalf("header-only fork is not readable: %v", err)
	}
}

func TestIndexForkBeforeKeepsHistoryAndExcludesSelectedUser(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "fork-before-history.jsonl")
	writeSession(t, path, strings.Join([]string{
		`{"type":"session","version":3,"id":"fork-before-history","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`,
		`{"type":"message","id":"root","parentId":null,"message":{"role":"user","content":"Root"}}`,
		`{"type":"message","id":"assistant","parentId":"root","message":{"role":"assistant","content":"Answer"}}`,
		`{"type":"message","id":"next","parentId":"assistant","message":{"role":"user","content":[{"type":"text","text":"Next"},{"type":"image","data":"ignored"}]}}`,
	}, "\n")+"\n")

	result, err := New(root).ForkBefore(path, "next")
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Next" {
		t.Fatalf("selected text = %q", result.Text)
	}
	messages, err := New(root).Messages(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	contents := messageContents(t, messages)
	if !slices.Equal(contents, []string{"Root", "Answer"}) {
		t.Fatalf("fork history = %#v", contents)
	}
}

func TestIndexForkAtCreatesBranchThroughAssistantEntry(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "fork.jsonl")
	writeSession(t, path, strings.Join([]string{
		`{"type":"session","version":3,"id":"fork","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`,
		`{"type":"message","id":"root","parentId":null,"message":{"role":"user","content":"Root"}}`,
		`{"type":"message","id":"assistant","parentId":"root","message":{"role":"assistant","content":"Keep"}}`,
		`{"type":"message","id":"later","parentId":"assistant","message":{"role":"user","content":"Later"}}`,
	}, "\n")+"\n")

	forked, err := New(root).ForkAt(path, "assistant")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(forked)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"parentSession":`+quote(canonicalTestPath(t, path))) || !strings.Contains(text, `"id":"assistant"`) || strings.Contains(text, `"id":"later"`) {
		t.Fatalf("forked session = %s", text)
	}
}

func TestIndexForkAtIncludesDisplayedToolResults(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "fork-tools.jsonl")
	writeSession(t, path, strings.Join([]string{
		`{"type":"session","version":3,"id":"fork","timestamp":"2026-08-10T08:00:00Z","cwd":"D:\\repo"}`,
		`{"type":"message","id":"root","parentId":null,"message":{"role":"user","content":"Root"}}`,
		`{"type":"message","id":"assistant","parentId":"root","message":{"role":"assistant","content":[{"type":"toolCall","id":"tool-1","name":"read"}]}}`,
		`{"type":"message","id":"result-1","parentId":"assistant","message":{"role":"toolResult","toolCallId":"tool-1","content":"output"}}`,
		`{"type":"message","id":"later","parentId":"result-1","message":{"role":"assistant","content":"Later"}}`,
	}, "\n")+"\n")

	forked, err := New(root).ForkAt(path, "assistant")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(forked)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"id":"assistant"`) || !strings.Contains(text, `"id":"result-1"`) || strings.Contains(text, `"id":"later"`) {
		t.Fatalf("forked tool message = %s", text)
	}
}

func messageContents(t *testing.T, messages []json.RawMessage) []string {
	t.Helper()
	result := make([]string, 0, len(messages))
	for _, message := range messages {
		var decoded struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(message, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Role == "piDeskCompaction" {
			continue
		}
		result = append(result, decoded.Content)
	}
	return result
}

func writeSession(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func quote(value string) string {
	return `"` + strings.ReplaceAll(value, `\`, `\\`) + `"`
}
