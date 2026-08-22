package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCatalogPersistsCanonicalWorkspaces(t *testing.T) {
	root := t.TempDir()
	workspacePath := filepath.Join(root, "project")
	if err := os.Mkdir(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, "config", "state.json")
	clock := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	catalog := NewCatalog(statePath)
	catalog.now = func() time.Time { return clock }

	created, err := catalog.Add(filepath.Join(workspacePath, "."), "deny")
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "project" || created.Trust != "deny" || created.Path != workspacePath {
		t.Fatalf("unexpected record: %#v", created)
	}

	clock = clock.Add(time.Hour)
	updated, err := catalog.Add(workspacePath, "approve")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID || updated.Trust != "approve" || !updated.LastOpenedAt.Equal(clock) {
		t.Fatalf("workspace was not updated in place: %#v", updated)
	}

	reloaded := NewCatalog(statePath)
	records, err := reloaded.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0] != updated {
		t.Fatalf("unexpected persisted records: %#v", records)
	}
}

func TestCatalogRejectsInvalidInputsAndCorruption(t *testing.T) {
	root := t.TempDir()
	catalog := NewCatalog(filepath.Join(root, "state.json"))
	if _, err := catalog.Add(filepath.Join(root, "missing"), "deny"); err == nil {
		t.Fatal("expected missing directory error")
	}
	if _, err := catalog.Add(root, "always"); err == nil {
		t.Fatal("expected trust validation error")
	}

	corruptPath := filepath.Join(root, "corrupt.json")
	if err := os.WriteFile(corruptPath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCatalog(corruptPath).List(); err == nil {
		t.Fatal("expected corrupt catalog error")
	}
}

func TestCatalogRejectsFutureVersionWithoutDowngradingState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	futureVersion := stateVersion + 1
	future := []byte(fmt.Sprintf(`{"version":%d,"workspaces":[],"desktop":{"threads":[]},"futureField":{"keep":true}}`, futureVersion))
	if err := os.WriteFile(statePath, future, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := NewCatalog(statePath)
	wantError := fmt.Sprintf("unsupported workspace catalog version %d", futureVersion)
	if err := catalog.SaveDesktop(DesktopRecord{}); err == nil || !strings.Contains(err.Error(), wantError) {
		t.Fatalf("future state write error = %v", err)
	}
	if _, err := catalog.List(); err == nil || !strings.Contains(err.Error(), wantError) {
		t.Fatalf("future state read error = %v", err)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(future) {
		t.Fatalf("future state was rewritten:\n%s", data)
	}
}

func TestCatalogRemove(t *testing.T) {
	root := t.TempDir()
	catalog := NewCatalog(filepath.Join(root, "state.json"))
	record, err := catalog.Add(root, "deny")
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Remove(record.ID); err != nil {
		t.Fatal(err)
	}
	records, err := catalog.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("expected empty catalog, got %#v", records)
	}
	if err := catalog.Remove(record.ID); err == nil {
		t.Fatal("expected not found error")
	}
}

func TestCatalogPersistsDesktopState(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, "state.json")
	catalog := NewCatalog(statePath)
	timestamp := time.Date(2026, 8, 10, 14, 0, 0, 0, time.UTC)
	catalog.now = func() time.Time { return timestamp }
	workspaceRecord, err := catalog.Add(project, "deny")
	if err != nil {
		t.Fatal(err)
	}
	desktop := DesktopRecord{
		ActiveThreadID: "thread-1",
		Threads: []ThreadRecord{{
			ID: "thread-1", Title: "Audit", WorkspacePath: workspaceRecord.Path, Trust: "deny", Status: "running", Draft: "continue", Unread: true,
		}},
	}
	if err := catalog.SaveDesktop(desktop); err != nil {
		t.Fatal(err)
	}

	reloaded := NewCatalog(statePath)
	loadedDesktop, err := reloaded.Desktop()
	if err != nil {
		t.Fatal(err)
	}
	if loadedDesktop.ActiveThreadID != "thread-1" || len(loadedDesktop.Threads) != 1 || loadedDesktop.Threads[0].Draft != "continue" {
		t.Fatalf("unexpected desktop state: %#v", loadedDesktop)
	}
}

func TestCatalogDropsRemovedMetadataOnSave(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	legacy := `{
  "version": 4,
  "workspaces": [],
  "worktrees": [{"threadId":"thread-1","workspaceId":"workspace-1","path":"old-checkout","branch":"old-branch"}],
  "sessions": [{"path":"one.jsonl","archivedAt":"2026-08-10T14:00:00Z"}],
  "desktop": {"threads": [{"id":"thread-1","title":"Legacy","workspacePath":"old-project","worktreePath":"old-checkout","branch":"old-branch","trust":"deny","status":"idle","sessionPath":"one.jsonl"}]}
}`
	if err := os.WriteFile(statePath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := NewCatalog(statePath)
	desktop, err := catalog.Desktop()
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveDesktop(desktop); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"sessions"`) || strings.Contains(string(data), `"archivedAt"`) || strings.Contains(string(data), `"worktrees"`) || strings.Contains(string(data), `"worktreePath"`) || strings.Contains(string(data), `"branch"`) {
		t.Fatalf("removed metadata was preserved: %s", data)
	}
}

func TestCatalogPersistsValidatedDesktopPreferences(t *testing.T) {
	root := t.TempDir()
	catalog := NewCatalog(filepath.Join(root, "state.json"))
	if err := catalog.SaveDesktop(DesktopRecord{Preferences: &PreferencesRecord{
		Appearance: "light", Language: "en",
		OfflineMode: true, ProxyEnabled: true, ProxyURL: "socks5://127.0.0.1:10800",
		StreamingBehavior: "followUp", SidebarCollapsed: true, SidebarWidth: 344,
		InspectorOpen: true, InspectorWidth: 468, InspectorTab: "context",
	}}); err != nil {
		t.Fatal(err)
	}
	reloaded := NewCatalog(filepath.Join(root, "state.json"))
	desktop, err := reloaded.Desktop()
	if err != nil {
		t.Fatal(err)
	}
	if desktop.Preferences == nil || desktop.Preferences.Appearance != "light" || desktop.Preferences.Language != "en" || !desktop.Preferences.ProxyEnabled || desktop.Preferences.StreamingBehavior != "followUp" || desktop.Preferences.SidebarWidth != 344 || desktop.Preferences.InspectorWidth != 468 || desktop.Preferences.InspectorTab != "context" {
		t.Fatalf("unexpected preferences: %#v", desktop.Preferences)
	}
	if err := catalog.SaveDesktop(DesktopRecord{Preferences: &PreferencesRecord{
		ProxyEnabled: true, ProxyURL: "http://user:secret@example.com", StreamingBehavior: "steer", InspectorTab: "changes",
	}}); err == nil {
		t.Fatal("expected persisted proxy credentials to be rejected")
	}
}

func TestCatalogMigratesVersionTwoAppearancePreference(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	legacy := `{"version":2,"workspaces":[],"desktop":{"threads":[],"preferences":{"streamingBehavior":"steer","inspectorTab":"changes"}}}`
	if err := os.WriteFile(statePath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := NewCatalog(statePath)
	desktop, err := catalog.Desktop()
	if err != nil {
		t.Fatal(err)
	}
	if desktop.Preferences == nil || desktop.Preferences.Appearance != "dark" {
		t.Fatalf("expected migrated dark appearance, got %#v", desktop.Preferences)
	}
	if !desktop.Preferences.CloseToTray {
		t.Fatalf("expected legacy state to enable close-to-tray, got %#v", desktop.Preferences)
	}
	if desktop.Preferences.Language != "" {
		t.Fatalf("legacy language should remain unset until save, got %#v", desktop.Preferences)
	}
	if err := catalog.SaveDesktop(desktop); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version": 6`) {
		t.Fatalf("expected version six state, got %s", data)
	}
	if !strings.Contains(string(data), `"language": "zh-CN"`) {
		t.Fatalf("expected default Chinese language after save, got %s", data)
	}
}

func TestCloseToTrayPreferenceRoundTrip(t *testing.T) {
	catalog := NewCatalog(filepath.Join(t.TempDir(), "state.json"))
	preferences := &PreferencesRecord{
		Appearance: "light", Language: "zh-CN", StreamingBehavior: "steer", InspectorTab: "changes", CloseToTray: false,
	}
	if err := catalog.SaveDesktop(DesktopRecord{Preferences: preferences}); err != nil {
		t.Fatal(err)
	}
	reloaded := NewCatalog(catalog.path)
	desktop, err := reloaded.Desktop()
	if err != nil {
		t.Fatal(err)
	}
	if desktop.Preferences == nil || desktop.Preferences.CloseToTray {
		t.Fatalf("expected explicit close-to-tray false to persist, got %#v", desktop.Preferences)
	}
}

func TestWindowStateRoundTripAndValidation(t *testing.T) {
	catalog := NewCatalog(filepath.Join(t.TempDir(), "state.json"))
	want := WindowRecord{X: 80, Y: 120, Width: 1280, Height: 760, Maximized: true, Valid: true}
	if err := catalog.SaveWindow(want); err != nil {
		t.Fatalf("save window state: %v", err)
	}
	got, err := catalog.Window()
	if err != nil {
		t.Fatalf("load window state: %v", err)
	}
	if got != want {
		t.Fatalf("window state = %#v, want %#v", got, want)
	}
	if err := catalog.SaveWindow(WindowRecord{Width: 979, Height: 680, Valid: true}); err == nil {
		t.Fatal("expected minimum window size validation to fail")
	}
}

func TestCatalogForgetsDesktopThread(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	catalog := NewCatalog(filepath.Join(root, "state.json"))
	workspaceRecord, err := catalog.Add(project, "deny")
	if err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(root, "session.jsonl")
	if err := catalog.SaveDesktop(DesktopRecord{ActiveThreadID: "thread-1", Threads: []ThreadRecord{{
		ID: "thread-1", Title: "Audit", WorkspacePath: workspaceRecord.Path, Trust: "deny", Status: "idle", SessionPath: sessionPath,
	}}}); err != nil {
		t.Fatal(err)
	}

	if err := catalog.ForgetSession(sessionPath); err != nil {
		t.Fatal(err)
	}
	desktop, err := catalog.Desktop()
	if err != nil || desktop.ActiveThreadID != "" || len(desktop.Threads) != 0 {
		t.Fatalf("desktop thread remains: %#v, %v", desktop, err)
	}
}

func TestCatalogValidatesDesktopStateAndPurgesRemovedWorkspace(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	catalog := NewCatalog(filepath.Join(root, "state.json"))
	workspaceRecord, err := catalog.Add(project, "approve")
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveDesktop(DesktopRecord{ActiveThreadID: "missing"}); err == nil {
		t.Fatal("expected invalid active thread error")
	}
	if err := catalog.SaveDesktop(DesktopRecord{Threads: []ThreadRecord{{
		ID: "thread-1", Title: "Audit", WorkspacePath: filepath.Join(root, "unknown"), Trust: "deny", Status: "idle",
	}}}); err == nil {
		t.Fatal("expected unknown workspace error")
	}
	desktop := DesktopRecord{ActiveThreadID: "thread-1", Threads: []ThreadRecord{{
		ID: "thread-1", Title: "Audit", WorkspacePath: workspaceRecord.Path, Trust: "approve", Status: "idle",
	}}}
	if err := catalog.SaveDesktop(desktop); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Remove(workspaceRecord.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err := catalog.Desktop()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ActiveThreadID != "" || len(loaded.Threads) != 0 {
		t.Fatalf("removed workspace left desktop threads: %#v", loaded)
	}
}

func TestCatalogResolvesOnlyRegisteredWorkspacePaths(t *testing.T) {
	root := t.TempDir()
	catalog := NewCatalog(filepath.Join(t.TempDir(), "state.json"))
	record, err := catalog.Add(root, "approve")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := catalog.ResolvePath(filepath.Join(root, "."))
	if err != nil || resolved.ID != record.ID {
		t.Fatalf("unexpected resolved workspace: %#v, err=%v", resolved, err)
	}
	if _, err := catalog.ResolvePath(t.TempDir()); err == nil {
		t.Fatal("expected an unregistered workspace to fail")
	}
}
