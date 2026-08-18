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
