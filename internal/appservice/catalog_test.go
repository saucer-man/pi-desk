package appservice

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pi-desk/internal/domain"
	"pi-desk/internal/sessionindex"
	"pi-desk/internal/workspace"
	"pi-desk/internal/workspaceapp"
)

type fakeWorkspaceApplicationManager struct {
	applications []workspaceapp.Application
	application  string
	workspace    string
	err          error
}

func canonicalTestPath(t *testing.T, path string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(canonical)
}

func (manager *fakeWorkspaceApplicationManager) List() []workspaceapp.Application {
	return manager.applications
}

func (manager *fakeWorkspaceApplicationManager) Open(applicationID, workspacePath string) error {
	manager.application = applicationID
	manager.workspace = workspacePath
	return manager.err
}

type fakeSessionLister struct {
	workspace string
	sessions  []sessionindex.Summary
	messages  []json.RawMessage
	model     *sessionindex.Model
	usage     sessionindex.UsageSummary
	err       error
}

func (index *fakeSessionLister) List(_ context.Context, workspace string) ([]sessionindex.Summary, error) {
	index.workspace = workspace
	return index.sessions, index.err
}

func (index *fakeSessionLister) Resolve(path string) (sessionindex.Summary, error) {
	if index.err != nil {
		return sessionindex.Summary{}, index.err
	}
	for _, summary := range index.sessions {
		if summary.Path == path {
			return summary, nil
		}
	}
	return sessionindex.Summary{ID: "resolved", Path: path}, nil
}

func (index *fakeSessionLister) Usage(_ context.Context, workspace string) (sessionindex.UsageSummary, error) {
	index.workspace = workspace
	return index.usage, index.err
}

func (index *fakeSessionLister) Header(path string) (sessionindex.Summary, error) {
	return index.Resolve(path)
}

func (index *fakeSessionLister) Snapshot(_ string) (sessionindex.Snapshot, error) {
	return sessionindex.Snapshot{
		Messages:     index.messages,
		Model:        index.model,
		MessageCount: len(index.messages),
	}, index.err
}

func TestCatalogServiceWorkspaceLifecycle(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	catalog := workspace.NewCatalog(filepath.Join(root, "state.json"))
	index := &fakeSessionLister{}
	service := newCatalogService(catalog, index, func(initialPath string) (string, error) {
		if initialPath != root {
			t.Fatalf("initial path = %q", initialPath)
		}
		return project, nil
	})

	picked, err := service.PickWorkspace(domain.PickWorkspaceRequest{InitialPath: root})
	if err != nil || picked != project {
		t.Fatalf("picked = %q, %v", picked, err)
	}
	created, err := service.AddWorkspace(domain.AddWorkspaceRequest{Path: picked, Trust: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := service.ListWorkspaces()
	if err != nil || len(listed) != 1 || listed[0] != created {
		t.Fatalf("listed = %#v, %v", listed, err)
	}
	if err := service.RemoveWorkspace(domain.WorkspaceRequest{ID: created.ID}); err != nil {
		t.Fatal(err)
	}
	listed, err = service.ListWorkspaces()
	if err != nil || len(listed) != 0 {
		t.Fatalf("after remove = %#v, %v", listed, err)
	}
}

func TestCatalogServiceOpensOnlyRegisteredWorkspace(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	catalog := workspace.NewCatalog(filepath.Join(root, "state.json"))
	service := newCatalogService(catalog, &fakeSessionLister{}, nil)
	opened := ""
	service.openWorkspace = func(path string) error { opened = path; return nil }
	created, err := service.AddWorkspace(domain.AddWorkspaceRequest{Path: project, Trust: "approve"})
	if err != nil {
		t.Fatal(err)
	}

	if err := service.OpenWorkspace(domain.WorkspaceRequest{ID: created.ID}); err != nil {
		t.Fatal(err)
	}
	canonicalProject := canonicalTestPath(t, project)
	if opened != canonicalProject {
		t.Fatalf("opened %q, want %q", opened, canonicalProject)
	}
	if err := service.OpenWorkspace(domain.WorkspaceRequest{ID: "unknown"}); err == nil {
		t.Fatal("expected an unknown workspace to be rejected")
	}
	denied, err := service.AddWorkspace(domain.AddWorkspaceRequest{Path: root, Trust: "deny"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.OpenWorkspace(domain.WorkspaceRequest{ID: denied.ID}); err == nil {
		t.Fatal("expected an untrusted workspace to be rejected")
	}
}

func TestCatalogServiceListsApplicationsAndOpensOnlyTrustedWorkspace(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	catalog := workspace.NewCatalog(filepath.Join(root, "state.json"))
	service := newCatalogService(catalog, &fakeSessionLister{}, nil)
	manager := &fakeWorkspaceApplicationManager{applications: []workspaceapp.Application{
		{ID: workspaceapp.VSCodeID, Name: "Visual Studio Code", IconDataURL: "data:image/png;base64,dnNjb2Rl"},
		{ID: workspaceapp.FileManagerID, Name: "File Explorer", IconDataURL: "data:image/png;base64,ZmlsZXM="},
	}}
	service.workspaceApplications = manager
	approved, err := service.AddWorkspace(domain.AddWorkspaceRequest{Path: project, Trust: "approve"})
	if err != nil {
		t.Fatal(err)
	}

	applications := service.ListWorkspaceApplications()
	if len(applications) != 2 || applications[0].ID != workspaceapp.VSCodeID || applications[0].IconDataURL != "data:image/png;base64,dnNjb2Rl" || applications[1].Name != "File Explorer" {
		t.Fatalf("applications = %#v", applications)
	}
	if err := service.OpenWorkspaceWith(domain.OpenWorkspaceWithRequest{WorkspaceID: approved.ID, ApplicationID: workspaceapp.VSCodeID}); err != nil {
		t.Fatal(err)
	}
	if manager.application != workspaceapp.VSCodeID || manager.workspace != canonicalTestPath(t, project) {
		t.Fatalf("open request = %q, %q", manager.application, manager.workspace)
	}

	denied, err := service.AddWorkspace(domain.AddWorkspaceRequest{Path: root, Trust: "deny"})
	if err != nil {
		t.Fatal(err)
	}
	manager.application, manager.workspace = "", ""
	if err := service.OpenWorkspaceWith(domain.OpenWorkspaceWithRequest{WorkspaceID: denied.ID, ApplicationID: workspaceapp.CursorID}); err == nil {
		t.Fatal("expected an untrusted workspace to be rejected")
	}
	if manager.application != "" || manager.workspace != "" {
		t.Fatal("application manager ran for an untrusted workspace")
	}
}

func TestCatalogServiceRejectsWorkspaceWhoseCanonicalBoundaryChanged(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	catalog := workspace.NewCatalog(filepath.Join(root, "state.json"))
	service := newCatalogService(catalog, &fakeSessionLister{}, nil)
	manager := &fakeWorkspaceApplicationManager{applications: []workspaceapp.Application{{ID: workspaceapp.FileManagerID, Name: "Files"}}}
	service.workspaceApplications = manager
	record, err := service.AddWorkspace(domain.AddWorkspaceRequest{Path: project, Trust: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(project); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, project); err != nil {
		t.Skipf("directory symlinks are unavailable: %v", err)
	}

	if err := service.OpenWorkspaceWith(domain.OpenWorkspaceWithRequest{WorkspaceID: record.ID, ApplicationID: workspaceapp.FileManagerID}); err == nil {
		t.Fatal("expected a changed canonical workspace boundary to be rejected")
	}
	if manager.application != "" || manager.workspace != "" {
		t.Fatal("application manager ran after the workspace boundary changed")
	}
}

func TestCatalogServiceMapsSessions(t *testing.T) {
	modified := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	index := &fakeSessionLister{sessions: []sessionindex.Summary{{
		ID: "session-1", Path: "one.jsonl", CWD: "D:\\repo", Title: "Audit", ModifiedAt: modified, MessageCount: 4,
	}}}
	catalog := workspace.NewCatalog(filepath.Join(t.TempDir(), "state.json"))
	service := newCatalogService(catalog, index, nil)

	sessions, err := service.ListSessions(domain.ListSessionsRequest{WorkspacePath: "D:\\repo"})
	if err != nil {
		t.Fatal(err)
	}
	if index.workspace != "D:\\repo" || len(sessions) != 1 || sessions[0].Title != "Audit" || sessions[0].ModifiedAt != modified {
		t.Fatalf("unexpected sessions: %#v", sessions)
	}
}

func TestCatalogServiceMapsOnlyKnownSSHAnchorSessions(t *testing.T) {
	catalog, _, record := remoteBackendCatalog(t)
	index := &fakeSessionLister{sessions: []sessionindex.Summary{
		{ID: "known", Path: "known.jsonl", CWD: "anchor", SSHAnchor: true, AnchorWorkspaceID: record.ID, Title: "Known"},
		{ID: "orphan", Path: "orphan.jsonl", CWD: "anchor", SSHAnchor: true, AnchorWorkspaceID: "workspace-fedcba9876543210fedcba9876543210", Title: "Orphan"},
	}}
	service := newCatalogService(catalog, index, nil)

	sessions, err := service.ListSessions(domain.ListSessionsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "known" || sessions[0].AnchorWorkspaceID != record.ID {
		t.Fatalf("sessions = %#v", sessions)
	}
	if _, err := service.GetSessionSnapshot(domain.SessionSnapshotRequest{Path: "known.jsonl"}); err != nil {
		t.Fatalf("known SSH session snapshot failed: %v", err)
	}
	if _, err := service.GetSessionSnapshot(domain.SessionSnapshotRequest{Path: "orphan.jsonl"}); err == nil {
		t.Fatal("ordinary snapshot accepted an orphan SSH transcript")
	}
}

func TestCatalogServiceRejectsSSHAnchorForLocalWorkspaceIdentity(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	catalog := workspace.NewCatalog(filepath.Join(root, "state.json"))
	record, err := catalog.Add(project, "approve")
	if err != nil {
		t.Fatal(err)
	}
	index := &fakeSessionLister{sessions: []sessionindex.Summary{{
		ID: "invalid-anchor", Path: "invalid.jsonl", CWD: project,
		SSHAnchor: true, AnchorWorkspaceID: record.ID, Title: "Invalid",
	}}}
	service := newCatalogService(catalog, index, nil)

	sessions, err := service.ListSessions(domain.ListSessionsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("local workspace accepted an SSH anchor session: %#v", sessions)
	}
	service.trash = func(string) (string, error) {
		t.Fatal("ordinary delete reached trash for an invalid SSH anchor")
		return "", nil
	}
	if _, err := service.GetSessionSnapshot(domain.SessionSnapshotRequest{Path: "invalid.jsonl"}); err == nil {
		t.Fatal("ordinary snapshot accepted an SSH anchor bound to a local workspace")
	}
	if _, err := service.DeleteSession(domain.DeleteSessionRequest{Path: "invalid.jsonl"}); err == nil {
		t.Fatal("ordinary delete accepted an SSH anchor bound to a local workspace")
	}
}

func TestCatalogServiceReadsSessionSnapshotWithoutStartingPi(t *testing.T) {
	want := []json.RawMessage{json.RawMessage(`{"role":"user","content":"Inspect runtime"}`)}
	index := &fakeSessionLister{
		messages: want,
		model:    &sessionindex.Model{Provider: "openai", ID: "gpt-5"},
	}
	service := newCatalogService(workspace.NewCatalog(filepath.Join(t.TempDir(), "state.json")), index, nil)

	snapshot, err := service.GetSessionSnapshot(domain.SessionSnapshotRequest{Path: "one.jsonl"})
	if err != nil || len(snapshot.Messages) != 1 || snapshot.Model == nil || snapshot.Model.Provider != "openai" || snapshot.Model.ID != "gpt-5" {
		t.Fatalf("snapshot = %#v, %v", snapshot, err)
	}
}

func TestCatalogServiceMapsHistoricalSessionUsageWithoutStartingPi(t *testing.T) {
	index := &fakeSessionLister{usage: sessionindex.UsageSummary{
		Sessions: 2, Messages: 8, UserMessages: 2, AssistantMessages: 3, ToolResults: 3,
		Tokens: sessionindex.TokenUsage{Input: 100, Output: 20, CacheRead: 50, CacheWrite: 5, Reasoning: 8, Total: 175},
		Cost:   0.42,
		Models: []sessionindex.ModelUsage{{
			Provider: "openai", Model: "gpt-5", AssistantMessages: 3,
			Tokens: sessionindex.TokenUsage{Input: 100, Output: 20, CacheRead: 50, CacheWrite: 5, Reasoning: 8, Total: 175}, Cost: 0.42,
		}},
	}}
	service := newCatalogService(workspace.NewCatalog(filepath.Join(t.TempDir(), "state.json")), index, nil)

	usage, err := service.GetSessionUsage(domain.ListSessionsRequest{WorkspacePath: `D:\repo`})
	if err != nil {
		t.Fatal(err)
	}
	if index.workspace != `D:\repo` || usage.Sessions != 2 || usage.Tokens.Total != 175 || usage.Tokens.Reasoning != 8 || len(usage.Models) != 1 || usage.Models[0].Model != "gpt-5" || usage.Cost != 0.42 {
		t.Fatalf("unexpected session usage: %#v", usage)
	}
}

func TestCatalogServicePersistsDesktopState(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	catalog := workspace.NewCatalog(filepath.Join(root, "state.json"))
	workspaceRecord, err := catalog.Add(project, "deny")
	if err != nil {
		t.Fatal(err)
	}
	index := &fakeSessionLister{sessions: []sessionindex.Summary{{ID: "session-1", Path: "one.jsonl", CWD: workspaceRecord.Path}}}
	service := newCatalogService(catalog, index, nil)

	if err := service.SaveDesktopState(domain.DesktopState{
		ActiveThreadID: "thread-1",
		Preferences: &domain.DesktopPreferences{
			Appearance: "light", Language: "zh-CN", FontFamily: "mono", FontSize: 15, OfflineMode: true, StreamingBehavior: "steer",
			SidebarWidth: 344, InspectorOpen: true, InspectorWidth: 468, InspectorTab: "changes", WorkspaceApplication: "vscode",
		},
		Threads: []domain.DesktopThreadState{{
			ID: "thread-1", Title: "Audit", WorkspacePath: workspaceRecord.Path, Trust: "deny", Status: "running",
			SessionPath: "one.jsonl", Draft: "continue", Unread: true,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	state, err := service.GetDesktopState()
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveThreadID != "thread-1" || len(state.Threads) != 1 || state.Threads[0].WorkspaceID != workspaceRecord.ID || state.Threads[0].Draft != "continue" || !state.Threads[0].Unread || state.Preferences == nil || state.Preferences.Language != "zh-CN" || state.Preferences.FontFamily != "mono" || state.Preferences.FontSize != 15 || !state.Preferences.OfflineMode || state.Preferences.SidebarWidth != 344 || state.Preferences.InspectorWidth != 468 || state.Preferences.WorkspaceApplication != "vscode" {
		t.Fatalf("unexpected desktop state: %#v", state)
	}
}

func TestCatalogServicePersistsRemoteDesktopStateByAnchorIdentity(t *testing.T) {
	catalog, _, remoteWorkspace := remoteBackendCatalog(t)
	index := &fakeSessionLister{sessions: []sessionindex.Summary{{
		ID: "session-remote", Path: "remote.jsonl", CWD: filepath.Join(t.TempDir(), "anchor"),
		SSHAnchor: true, AnchorWorkspaceID: remoteWorkspace.ID,
	}}}
	service := newCatalogService(catalog, index, nil)
	state := domain.DesktopState{ActiveThreadID: "thread-remote", Threads: []domain.DesktopThreadState{{
		ID: "thread-remote", Title: "Remote audit", WorkspaceID: remoteWorkspace.ID, WorkspacePath: "",
		Trust: "approve", Status: "idle", SessionPath: "remote.jsonl", Draft: "continue",
	}}}
	if err := service.SaveDesktopState(state); err != nil {
		t.Fatal(err)
	}
	persisted, err := service.GetDesktopState()
	if err != nil || len(persisted.Threads) != 1 || persisted.Threads[0].WorkspaceID != remoteWorkspace.ID || persisted.Threads[0].WorkspacePath != "" || persisted.Threads[0].SessionPath != "remote.jsonl" {
		t.Fatalf("remote desktop state=%#v err=%v", persisted, err)
	}

	index.sessions[0].AnchorWorkspaceID = "workspace-fedcba9876543210fedcba9876543210"
	if err := service.SaveDesktopState(state); err == nil {
		t.Fatal("mismatched remote session anchor was accepted")
	}
	index.sessions[0].AnchorWorkspaceID = remoteWorkspace.ID
	state.Threads[0].WorkspaceID = ""
	if err := service.SaveDesktopState(state); err == nil {
		t.Fatal("remote session without immutable WorkspaceID was accepted")
	}
}

func TestCatalogServiceMovesDeletedSessionToRecoveryFile(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(root, "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte("session"), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := workspace.NewCatalog(filepath.Join(root, "state.json"))
	workspaceRecord, err := catalog.Add(project, "deny")
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveDesktop(workspace.DesktopRecord{ActiveThreadID: "thread-1", Threads: []workspace.ThreadRecord{{
		ID: "thread-1", Title: "Audit", WorkspacePath: workspaceRecord.Path, Trust: "deny", Status: "idle", SessionPath: sessionPath,
	}}}); err != nil {
		t.Fatal(err)
	}
	index := &fakeSessionLister{sessions: []sessionindex.Summary{{ID: "session-1", Path: sessionPath, CWD: workspaceRecord.Path}}}
	service := newCatalogService(catalog, index, nil)

	deleted, err := service.DeleteSession(domain.DeleteSessionRequest{Path: sessionPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("source session still exists: %v", err)
	}
	if info, err := os.Stat(deleted.RecoveryPath); err != nil || info.Size() == 0 {
		t.Fatalf("recovery file missing: %v", err)
	}
	state, err := service.GetDesktopState()
	if err != nil || len(state.Threads) != 0 || state.ActiveThreadID != "" {
		t.Fatalf("deleted session remains in desktop state: %#v, %v", state, err)
	}
}

func TestCatalogServiceRejectsUnavailablePicker(t *testing.T) {
	service := newCatalogService(workspace.NewCatalog(filepath.Join(t.TempDir(), "state.json")), &fakeSessionLister{}, nil)
	if _, err := service.PickWorkspace(domain.PickWorkspaceRequest{}); err == nil {
		t.Fatal("expected picker error")
	}
}
