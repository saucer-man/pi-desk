package appservice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pi-desk/internal/domain"
	"pi-desk/internal/piruntime"
	"pi-desk/internal/sessionindex"
	"pi-desk/internal/workspace"
)

func TestLiveOrphanSessionHTMLExport(t *testing.T) {
	if os.Getenv("PI_DESK_ORPHAN_EXPORT_LIVE") != "1" {
		t.Skip("set PI_DESK_ORPHAN_EXPORT_LIVE=1 to run the local Pi export fixture")
	}
	root := t.TempDir()
	sessionsRoot := filepath.Join(root, "sessions")
	anchorRoot := filepath.Join(root, "anchors")
	workspaceID := "workspace-fedcba9876543210fedcba9876543210"
	anchor, err := workspace.EnsureSSHAnchor(anchorRoot, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sessionsRoot, "remote"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessionsRoot, "remote", "orphan.jsonl")
	content := `{"type":"session","version":3,"id":"orphan-export","timestamp":"2026-08-10T08:00:00Z","cwd":` + quoted(anchor) + "}\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := NewOrphanSessionService(workspace.NewCatalog(filepath.Join(root, "state.json")), sessionindex.NewWithAnchorRoot(sessionsRoot, anchorRoot), piruntime.NewLocator())
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "export.html")
	if err := service.ExportOrphanSession(domain.ExportOrphanSessionRequest{Path: path, OutputPath: output}); err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(output)
	if err != nil || !strings.Contains(string(html), "<!DOCTYPE html>") {
		t.Fatalf("export is invalid: bytes=%d err=%v", len(html), err)
	}
	if matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "pi-desk-orphan-transcript-*.jsonl")); len(matches) != 0 {
		t.Fatalf("orphan transcript staging files remain: %#v", matches)
	}
}
