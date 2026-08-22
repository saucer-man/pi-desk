package appservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"pi-desk/internal/domain"
	"pi-desk/internal/repository"
	"pi-desk/internal/workspace"
)

type fakeWorkspaceResolver struct {
	record workspace.Record
	err    error
}

func (resolver fakeWorkspaceResolver) ResolvePath(string) (workspace.Record, error) {
	return resolver.record, resolver.err
}

func (resolver fakeWorkspaceResolver) ResolveID(string) (workspace.Record, error) {
	return resolver.record, resolver.err
}

type fakeRemoteRepositoryBackend struct {
	workspaceID string
	generation  uint64
	validation  error
	snapshot    repository.Snapshot
	started     chan struct{}
	release     chan struct{}
}

func (backend *fakeRemoteRepositoryBackend) WorkspaceID() string    { return backend.workspaceID }
func (backend *fakeRemoteRepositoryBackend) Generation() uint64     { return backend.generation }
func (backend *fakeRemoteRepositoryBackend) ValidateBinding() error { return backend.validation }
func (backend *fakeRemoteRepositoryBackend) Snapshot(context.Context) (repository.Snapshot, error) {
	if backend.started != nil {
		close(backend.started)
	}
	if backend.release != nil {
		<-backend.release
	}
	return backend.snapshot, nil
}
func (*fakeRemoteRepositoryBackend) Diff(context.Context, string) (repository.FileDiff, error) {
	return repository.FileDiff{}, nil
}
func (*fakeRemoteRepositoryBackend) Branches(context.Context) (repository.BranchInventory, error) {
	return repository.BranchInventory{}, nil
}
func (*fakeRemoteRepositoryBackend) Preview(context.Context, string) (repository.FilePreview, error) {
	return repository.FilePreview{}, nil
}

type fakeRepositoryScanner struct {
	root     string
	snapshot repository.Snapshot
	diff     repository.FileDiff
	branches repository.BranchInventory
}

func (scanner *fakeRepositoryScanner) Branches(_ context.Context, root string) (repository.BranchInventory, error) {
	scanner.root = root
	return scanner.branches, nil
}

func (scanner *fakeRepositoryScanner) Snapshot(_ context.Context, root string) (repository.Snapshot, error) {
	scanner.root = root
	return scanner.snapshot, nil
}

func (scanner *fakeRepositoryScanner) Diff(_ context.Context, root, path string) (repository.FileDiff, error) {
	scanner.root = root
	result := scanner.diff
	result.Path = path
	return result, nil
}

func TestRepositoryServiceRequiresRegisteredTrustedWorkspace(t *testing.T) {
	request := domain.RepositoryRequest{WorkspacePath: "C:\\repo"}
	service := newRepositoryService(fakeWorkspaceResolver{err: errors.New("not registered")}, &fakeRepositoryScanner{})
	if _, err := service.Snapshot(request); err == nil {
		t.Fatal("expected an unregistered workspace to fail")
	}
	service = newRepositoryService(fakeWorkspaceResolver{record: workspace.Record{Path: "C:\\repo", Trust: "deny"}}, &fakeRepositoryScanner{})
	if _, err := service.Snapshot(request); err == nil {
		t.Fatal("expected an untrusted workspace to fail")
	}
}

func TestRepositoryServiceMapsBoundedSnapshot(t *testing.T) {
	scanner := &fakeRepositoryScanner{snapshot: repository.Snapshot{
		Files: []repository.File{{Path: "main.go", Name: "main.go"}},
		Git:   repository.GitStatus{IsRepository: true, Branch: "main", Files: []repository.ChangedFile{{Path: "main.go", WorktreeStatus: "M"}}},
	}}
	service := newRepositoryService(fakeWorkspaceResolver{record: workspace.Record{Path: "C:\\repo", Trust: "approve"}}, scanner)
	result, err := service.Snapshot(domain.RepositoryRequest{WorkspacePath: "C:\\repo"})
	if err != nil {
		t.Fatal(err)
	}
	if scanner.root != "C:\\repo" || result.Git.Branch != "main" || len(result.Files) != 1 || len(result.Git.Files) != 1 {
		t.Fatalf("unexpected snapshot: %#v", result)
	}
}

func TestRepositoryServiceMapsDiffForTrustedWorkspace(t *testing.T) {
	scanner := &fakeRepositoryScanner{diff: repository.FileDiff{Working: "+new", Truncated: true}}
	service := newRepositoryService(fakeWorkspaceResolver{record: workspace.Record{Path: "C:\\repo", Trust: "approve"}}, scanner)
	result, err := service.Diff(domain.RepositoryFileRequest{WorkspacePath: "C:\\repo", Path: "main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if scanner.root != "C:\\repo" || result.Path != "main.go" || result.Working != "+new" || !result.Truncated {
		t.Fatalf("unexpected diff: %#v", result)
	}
}

func TestRepositoryServiceMapsBranchesForTrustedWorkspace(t *testing.T) {
	scanner := &fakeRepositoryScanner{branches: repository.BranchInventory{Branches: []repository.Branch{{
		Name: "main", FullName: "refs/heads/main", Current: true, Commit: "abc123", WorktreePath: "C:\\repo",
	}}}}
	service := newRepositoryService(fakeWorkspaceResolver{record: workspace.Record{Path: "C:\\repo", Trust: "approve"}}, scanner)
	result, err := service.Branches(domain.RepositoryRequest{WorkspacePath: "C:\\repo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Branches) != 1 || !result.Branches[0].Current || result.Branches[0].WorktreePath != "C:\\repo" {
		t.Fatalf("unexpected branches: %#v", result)
	}
}

func TestRepositoryServiceFailsClosedForUnboundRemoteWorkspace(t *testing.T) {
	record := workspace.Record{
		ID: "workspace-0123456789abcdef0123456789abcdef", Trust: "approve",
		Location: workspace.Location{Kind: workspace.KindSSH, SSH: workspace.SSHLocation{TargetID: "target-0123456789abcdef0123456789abcdef"}},
	}
	service := newRepositoryService(fakeWorkspaceResolver{record: record}, &fakeRepositoryScanner{})
	request := domain.RepositoryRequest{WorkspaceID: record.ID}
	if _, err := service.Snapshot(request); err == nil {
		t.Fatal("unbound remote Repository was accepted")
	}
	if err := service.OpenFile(domain.RepositoryFileRequest{WorkspaceID: record.ID, Path: "README.md"}); err == nil {
		t.Fatal("remote file was routed to the local opener")
	}
	if _, err := service.Snapshot(domain.RepositoryRequest{WorkspaceID: record.ID, WorkspacePath: "anchor"}); err == nil {
		t.Fatal("ambiguous remote workspace identity was accepted")
	}
}

func TestRepositoryServiceDropsStaleGenerationCompletion(t *testing.T) {
	record := workspace.Record{
		ID: "workspace-0123456789abcdef0123456789abcdef", Trust: "approve",
		Location: workspace.Location{Kind: workspace.KindSSH, SSH: workspace.SSHLocation{TargetID: "target-0123456789abcdef0123456789abcdef"}},
	}
	service := newRepositoryService(fakeWorkspaceResolver{record: record}, &fakeRepositoryScanner{})
	oldBackend := &fakeRemoteRepositoryBackend{
		workspaceID: record.ID, generation: 1,
		snapshot: repository.Snapshot{Files: []repository.File{{Path: "old", Name: "old"}}},
		started:  make(chan struct{}), release: make(chan struct{}),
	}
	if err := service.bindRemoteWorkspace(record.ID, oldBackend); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := service.Snapshot(domain.RepositoryRequest{WorkspaceID: record.ID})
		result <- err
	}()
	<-oldBackend.started
	service.unbindRemoteWorkspace(record.ID, 1)
	newBackend := &fakeRemoteRepositoryBackend{
		workspaceID: record.ID, generation: 2,
		snapshot: repository.Snapshot{Files: []repository.File{{Path: "new", Name: "new"}}},
	}
	if err := service.bindRemoteWorkspace(record.ID, newBackend); err != nil {
		t.Fatal(err)
	}
	service.unbindRemoteWorkspace(record.ID, 1)
	close(oldBackend.release)
	if err := <-result; !errors.Is(err, ErrRemoteRepositoryStale) {
		t.Fatalf("stale completion error = %v", err)
	}
	snapshot, err := service.Snapshot(domain.RepositoryRequest{WorkspaceID: record.ID})
	if err != nil || len(snapshot.Files) != 1 || snapshot.Files[0].Path != "new" {
		t.Fatalf("new snapshot=%#v err=%v", snapshot, err)
	}
	if err := service.bindRemoteWorkspace(record.ID, oldBackend); !errors.Is(err, ErrRemoteRepositoryStale) {
		t.Fatalf("old generation rebound: %v", err)
	}
}

func TestRepositoryServiceRejectsInvalidRemoteBinding(t *testing.T) {
	record := workspace.Record{
		ID: "workspace-0123456789abcdef0123456789abcdef", Trust: "approve",
		Location: workspace.Location{Kind: workspace.KindSSH, SSH: workspace.SSHLocation{TargetID: "target-0123456789abcdef0123456789abcdef"}},
	}
	service := newRepositoryService(fakeWorkspaceResolver{record: record}, &fakeRepositoryScanner{})
	backend := &fakeRemoteRepositoryBackend{workspaceID: record.ID, generation: 1, validation: errors.New("revoked")}
	if err := service.bindRemoteWorkspace(record.ID, backend); !errors.Is(err, ErrRemoteRepositoryStale) {
		t.Fatalf("invalid binding error = %v", err)
	}
	backend.validation = nil
	backend.workspaceID = "workspace-ffffffffffffffffffffffffffffffff"
	if err := service.bindRemoteWorkspace(record.ID, backend); err == nil {
		t.Fatal("cross-workspace backend was accepted")
	}
}

func TestRepositoryServiceOnlyOperatesOnResolvedWorkspaceFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("package main"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := newRepositoryService(fakeWorkspaceResolver{record: workspace.Record{Path: root, Trust: "approve"}}, &fakeRepositoryScanner{})
	var opened, openedWith string
	service.openFile = func(path string) error { opened = path; return nil }
	service.openFileWith = func(path string) error { openedWith = path; return nil }
	request := domain.RepositoryFileRequest{WorkspacePath: root, Path: "main.go"}
	if err := service.OpenFile(request); err != nil {
		t.Fatal(err)
	}
	if err := service.OpenFileWith(request); err != nil {
		t.Fatal(err)
	}
	if opened != path || openedWith != path {
		t.Fatalf("opened %q and %q, want %q", opened, openedWith, path)
	}
	preview, err := service.PreviewFile(request)
	if err != nil {
		t.Fatal(err)
	}
	if preview.AbsolutePath != path || preview.Content != "package main" || preview.Size != int64(len("package main")) || preview.Binary {
		t.Fatalf("unexpected preview: %#v", preview)
	}
	outputPath := filepath.Join(t.TempDir(), "saved.go")
	if err := service.SaveFileAs(domain.RepositorySaveFileRequest{WorkspacePath: root, Path: "main.go", OutputPath: outputPath}); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(outputPath); err != nil || string(saved) != "package main" {
		t.Fatalf("saved content = %q, err = %v", saved, err)
	}
	if err := service.OpenFile(domain.RepositoryFileRequest{WorkspacePath: root, Path: "../outside.go"}); err == nil {
		t.Fatal("expected an escaping path to fail")
	}
	if err := service.SaveFileAs(domain.RepositorySaveFileRequest{WorkspacePath: root, Path: "main.go", OutputPath: "relative.go"}); err == nil {
		t.Fatal("expected a relative save destination to fail")
	}
}
