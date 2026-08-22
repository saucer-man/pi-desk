package appservice

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"pi-desk/internal/domain"
	"pi-desk/internal/remotessh"
	"pi-desk/internal/repository"
	"pi-desk/internal/workspace"
)

func TestRemoteBackendCoordinatorRejectsOldGenerationAndUnbindsTarget(t *testing.T) {
	catalog, target, record := remoteBackendCatalog(t)
	repositoryService := newRepositoryService(catalog, &fakeRepositoryScanner{})
	terminalService := NewTerminalService(catalog)
	coordinator, err := NewRemoteBackendCoordinator(catalog, repositoryService, terminalService)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &remotessh.RuntimeLeaseSupervisor{}
	first := &fakeRemoteRepositoryBackend{
		workspaceID: record.ID, generation: 1,
		snapshot: repository.Snapshot{Files: []repository.File{{Path: "first", Name: "first"}}},
	}
	if err := coordinator.bindWorkspaceBackend(record.ID, target.ID, runtime, first); err != nil {
		t.Fatal(err)
	}
	second := &fakeRemoteRepositoryBackend{
		workspaceID: record.ID, generation: 2,
		snapshot: repository.Snapshot{Files: []repository.File{{Path: "second", Name: "second"}}},
	}
	if err := coordinator.bindWorkspaceBackend(record.ID, target.ID, runtime, second); err != nil {
		t.Fatal(err)
	}
	duplicate := &fakeRemoteRepositoryBackend{
		workspaceID: record.ID, generation: 2,
		snapshot: repository.Snapshot{Files: []repository.File{{Path: "duplicate", Name: "duplicate"}}},
	}
	if err := coordinator.bindWorkspaceBackend(record.ID, target.ID, runtime, duplicate); err != nil {
		t.Fatalf("same generation re-open failed: %v", err)
	}
	snapshot, err := repositoryService.Snapshot(domain.RepositoryRequest{WorkspaceID: record.ID})
	if err != nil || len(snapshot.Files) != 1 || snapshot.Files[0].Path != "second" {
		t.Fatalf("same generation re-open replaced snapshot=%#v err=%v", snapshot, err)
	}
	if err := coordinator.bindWorkspaceBackend(record.ID, target.ID, runtime, first); !errors.Is(err, ErrRemoteRepositoryStale) {
		t.Fatalf("old generation rebound: %v", err)
	}
	coordinator.UnbindTarget(target.ID)
	if _, err := repositoryService.Snapshot(domain.RepositoryRequest{WorkspaceID: record.ID}); !errors.Is(err, ErrRemoteRepositoryStale) {
		t.Fatalf("target unbind error=%v", err)
	}
}

func TestRemoteBackendCoordinatorIgnoresLateTaskUnbind(t *testing.T) {
	newLease := &remotessh.RuntimeLease{}
	oldLease := &remotessh.RuntimeLease{}
	coordinator := &RemoteBackendCoordinator{
		tasks:    map[string]remoteTaskBinding{"thread-remote": {workspaceID: "workspace-new", generation: 2, lease: newLease}},
		terminal: &TerminalService{remoteThreads: map[string]remoteTerminalThread{"thread-remote": {workspaceID: "workspace-new", active: true}}},
	}
	if err := coordinator.UnbindTask("thread-remote", oldLease); err != nil {
		t.Fatal(err)
	}
	if task := coordinator.tasks["thread-remote"]; task.generation != 2 {
		t.Fatalf("late unbind removed new task: %#v", task)
	}
	if !coordinator.terminal.remoteThreads["thread-remote"].active {
		t.Fatal("late unbind detached the new remote Terminal")
	}
}

func TestRemoteCatalogRevokeUnbindsBackendsBeforeDenyPersistence(t *testing.T) {
	catalog, target, record := remoteBackendCatalog(t)
	repositoryService := newRepositoryService(catalog, &fakeRepositoryScanner{})
	terminalService := NewTerminalService(catalog)
	backends, err := NewRemoteBackendCoordinator(catalog, repositoryService, terminalService)
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeRemoteRepositoryBackend{workspaceID: record.ID, generation: 1}
	if err := backends.bindWorkspaceBackend(record.ID, target.ID, &remotessh.RuntimeLeaseSupervisor{}, backend); err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewRemoteCatalogCoordinator(catalog, remotessh.NewRuntimeRegistry(), backends)
	if err != nil {
		t.Fatal(err)
	}
	registration := workspace.SSHWorkspaceRegistration{
		Name: record.Name, TargetID: target.ID,
		RequestedRoot: record.Location.SSH.RequestedRoot, CanonicalRoot: record.Location.SSH.CanonicalRoot,
		Device: record.Location.SSH.Device, Inode: record.Location.SSH.Inode,
		RemoteOS: record.Location.SSH.RemoteOS, RemoteArch: record.Location.SSH.RemoteArch, Trust: "deny",
	}
	if _, err := coordinator.AddSSHWorkspace(context.Background(), registration); err != nil {
		t.Fatal(err)
	}
	if _, err := repositoryService.remoteBackend(record.ID); !errors.Is(err, ErrRemoteRepositoryStale) {
		t.Fatalf("denied workspace retained a Repository backend: %v", err)
	}
}

func remoteBackendCatalog(t *testing.T) (*workspace.Catalog, workspace.TargetRecord, workspace.Record) {
	t.Helper()
	catalog := workspace.NewCatalog(filepath.Join(t.TempDir(), "state.json"))
	target, err := catalog.RegisterTarget(workspace.TargetRegistration{
		Name: "Build host", HostAlias: "build-prod", ConfigFingerprint: strings.Repeat("a", 64),
		HostKeyAlgorithm: "ssh-ed25519", HostKeySHA256: "SHA256:" + strings.Repeat("A", 43),
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := catalog.AddSSHWorkspace(workspace.SSHWorkspaceRegistration{
		Name: "Remote", TargetID: target.ID, RequestedRoot: "/srv/repository", CanonicalRoot: "/srv/repository",
		Device: 7, Inode: 11, RemoteOS: "linux", RemoteArch: "amd64", Trust: "approve",
	})
	if err != nil {
		t.Fatal(err)
	}
	return catalog, target, record
}
