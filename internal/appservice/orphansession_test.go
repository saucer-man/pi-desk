package appservice

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pi-desk/internal/domain"
	"pi-desk/internal/sessionindex"
	"pi-desk/internal/workspace"
)

func TestOrphanSessionServiceStaysLocalAndRevalidatesEveryOperation(t *testing.T) {
	root := t.TempDir()
	sessionsRoot := filepath.Join(root, "sessions")
	anchorRoot := filepath.Join(root, "anchors")
	statePath := filepath.Join(root, "state.json")
	workspaceID := "workspace-fedcba9876543210fedcba9876543210"
	anchor, err := workspace.EnsureSSHAnchor(anchorRoot, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	sessionDirectory := filepath.Join(sessionsRoot, "remote")
	if err := os.MkdirAll(sessionDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(sessionDirectory, "orphan.jsonl")
	content := strings.Join([]string{
		`{"type":"session","version":3,"id":"orphan-session","timestamp":"2026-08-10T08:00:00Z","cwd":` + quoted(anchor) + `}`,
		`{"type":"message","id":"user-1","parentId":null,"timestamp":"2026-08-10T08:01:00Z","message":{"role":"user","content":"Inspect remote"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(sessionPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	catalog := workspace.NewCatalog(statePath)
	index := sessionindex.NewWithAnchorRoot(sessionsRoot, anchorRoot)
	exported := false
	service := newOrphanSessionService(catalog, index, trashSessionFile, func(_ context.Context, input, output string) error {
		exported = input == sessionPath && strings.HasSuffix(output, "orphan.html")
		return nil
	})
	listed, err := service.ListOrphanSessions()
	if err != nil || len(listed) != 1 || listed[0].AnchorWorkspaceID != workspaceID || listed[0].Path != sessionPath {
		t.Fatalf("orphans = %#v, %v", listed, err)
	}
	snapshot, err := service.GetOrphanSessionSnapshot(domain.SessionSnapshotRequest{Path: sessionPath})
	if err != nil || len(snapshot.Messages) != 1 || snapshot.MessageCount != 1 {
		t.Fatalf("snapshot = %#v, %v", snapshot, err)
	}
	outputPath := filepath.Join(root, "orphan.html")
	if err := service.ExportOrphanSession(domain.ExportOrphanSessionRequest{Path: sessionPath, OutputPath: outputPath}); err != nil || !exported {
		t.Fatalf("exported=%v err=%v", exported, err)
	}
	deleted, err := service.DeleteOrphanSession(domain.DeleteSessionRequest{Path: sessionPath})
	if err != nil || deleted.RecoveryPath == "" {
		t.Fatalf("deleted = %#v, %v", deleted, err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("session still exists: %v", err)
	}
	if _, err := os.Stat(deleted.RecoveryPath); err != nil {
		t.Fatalf("recovery file: %v", err)
	}
}

func TestOrphanSessionServiceRejectsKnownWorkspaceAndNonAnchor(t *testing.T) {
	root := t.TempDir()
	sessionsRoot := filepath.Join(root, "sessions")
	anchorRoot := filepath.Join(root, "anchors")
	localRoot := filepath.Join(root, "local")
	if err := os.MkdirAll(localRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	catalog := workspace.NewCatalog(filepath.Join(root, "state.json"))
	known, err := catalog.Add(localRoot, "approve")
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := workspace.EnsureSSHAnchor(anchorRoot, known.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sessionsRoot, "items"), 0o755); err != nil {
		t.Fatal(err)
	}
	knownPath := filepath.Join(sessionsRoot, "items", "known.jsonl")
	localPath := filepath.Join(sessionsRoot, "items", "local.jsonl")
	if err := os.WriteFile(knownPath, []byte(`{"type":"session","version":3,"id":"known","timestamp":"2026-08-10T08:00:00Z","cwd":`+quoted(anchor)+"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localPath, []byte(`{"type":"session","version":3,"id":"local","timestamp":"2026-08-10T08:00:00Z","cwd":`+quoted(localRoot)+"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := newOrphanSessionService(catalog, sessionindex.NewWithAnchorRoot(sessionsRoot, anchorRoot), trashSessionFile, nil)
	listed, err := service.ListOrphanSessions()
	if err != nil || len(listed) != 0 {
		t.Fatalf("orphans = %#v, %v", listed, err)
	}
	if _, err := service.GetOrphanSessionSnapshot(domain.SessionSnapshotRequest{Path: knownPath}); err == nil {
		t.Fatal("known workspace transcript was accepted as orphan")
	}
	if _, err := service.GetOrphanSessionSnapshot(domain.SessionSnapshotRequest{Path: localPath}); err == nil {
		t.Fatal("local transcript was accepted as orphan")
	}
}

func quoted(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
