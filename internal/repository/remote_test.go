package repository

import (
	"context"
	"testing"

	"pi-desk/internal/remotessh"
)

type fakeRemoteRepositoryRuntime struct {
	find      remotessh.RuntimeSearchFindResult
	git       map[string]remotessh.RuntimeGitReadResult
	gitErr    map[string]error
	stat      remotessh.RuntimeFileInfo
	read      remotessh.RuntimeFileRead
	readErr   error
	acquired  int
	lastOwner string
}

func (runtime *fakeRemoteRepositoryRuntime) ValidateGeneration(uint64) error { return nil }

func (runtime *fakeRemoteRepositoryRuntime) AcquireRead(_ context.Context, request remotessh.RuntimeLeaseRequest) (*remotessh.RuntimeLease, error) {
	runtime.acquired++
	runtime.lastOwner = request.OwnerID
	return &remotessh.RuntimeLease{}, nil
}
func (runtime *fakeRemoteRepositoryRuntime) FindFiles(context.Context, *remotessh.RuntimeLease, remotessh.RuntimeSearchFindRequest) (remotessh.RuntimeSearchFindResult, error) {
	return runtime.find, nil
}
func (runtime *fakeRemoteRepositoryRuntime) ReadGit(_ context.Context, _ *remotessh.RuntimeLease, request remotessh.RuntimeGitReadRequest) (remotessh.RuntimeGitReadResult, error) {
	return runtime.git[request.Operation], runtime.gitErr[request.Operation]
}
func (runtime *fakeRemoteRepositoryRuntime) StatFile(context.Context, *remotessh.RuntimeLease, string) (remotessh.RuntimeFileInfo, error) {
	return runtime.stat, nil
}
func (runtime *fakeRemoteRepositoryRuntime) ReadFile(context.Context, *remotessh.RuntimeLease, string, int, int) (remotessh.RuntimeFileRead, error) {
	return runtime.read, runtime.readErr
}

func remoteGitResult(operation string, parts ...struct {
	name  string
	value string
}) remotessh.RuntimeGitReadResult {
	result := remotessh.RuntimeGitReadResult{Operation: operation}
	for _, part := range parts {
		result.Parts = append(result.Parts, remotessh.RuntimeGitOutputPart{Name: part.name, Offset: int64(len(result.Blob)), Size: int64(len(part.value))})
		result.Blob = append(result.Blob, part.value...)
	}
	return result
}

func gitPart(name, value string) struct {
	name  string
	value string
} {
	return struct {
		name  string
		value string
	}{name: name, value: value}
}

func TestRemoteBackendProjectsSnapshotDiffBranchesAndPreview(t *testing.T) {
	runtime := &fakeRemoteRepositoryRuntime{
		find: remotessh.RuntimeSearchFindResult{Paths: []string{"README.md", "src/main.go"}, BudgetReached: true},
		git: map[string]remotessh.RuntimeGitReadResult{
			"status": remoteGitResult("status", gitPart("status", "## main...origin/main [ahead 1]\x00 M src/main.go\x00")),
			"diff":   remoteGitResult("diff", gitPart("staged", ""), gitPart("working", "diff --git a/src/main.go b/src/main.go\n")),
			"branches": remoteGitResult("branches",
				gitPart("worktrees", "worktree /srv/repository\x00HEAD abc\x00branch refs/heads/main\x00\x00"),
				gitPart("refs", "refs/heads/main\tmain\t*\torigin/main\tabc123\t\n")),
		},
		gitErr: map[string]error{},
		stat:   remotessh.RuntimeFileInfo{Path: "README.md", Kind: "file", Size: 12},
		read:   remotessh.RuntimeFileRead{Path: "README.md", Content: "hello remote", StartLine: 1, EndLine: 1},
	}
	backend := newRemoteBackend(runtime, &remotessh.RuntimeRootCapability{})
	snapshot, err := backend.Snapshot(context.Background())
	if err != nil || len(snapshot.Files) != 2 || !snapshot.Truncated || !snapshot.Git.IsRepository || snapshot.Git.Branch != "main" || snapshot.Git.Ahead != 1 || len(snapshot.Git.Files) != 1 {
		t.Fatalf("snapshot = %#v, %v", snapshot, err)
	}
	diff, err := backend.Diff(context.Background(), "src/main.go")
	if err != nil || diff.Working == "" || diff.Path != "src/main.go" {
		t.Fatalf("diff = %#v, %v", diff, err)
	}
	branches, err := backend.Branches(context.Background())
	if err != nil || len(branches.Branches) != 1 || branches.Branches[0].WorktreePath != "/srv/repository" || !branches.Branches[0].Current {
		t.Fatalf("branches = %#v, %v", branches, err)
	}
	preview, err := backend.Preview(context.Background(), "README.md")
	if err != nil || preview.Content != "hello remote" || preview.Size != 12 || preview.Path != "README.md" {
		t.Fatalf("preview = %#v, %v", preview, err)
	}
	if runtime.acquired != 4 || runtime.lastOwner == "" {
		t.Fatalf("lease acquisitions=%d owner=%q", runtime.acquired, runtime.lastOwner)
	}
}

func TestRemoteBackendHandlesNonGitAndRejectsHostPaths(t *testing.T) {
	runtime := &fakeRemoteRepositoryRuntime{
		find:   remotessh.RuntimeSearchFindResult{Paths: []string{"file.txt"}},
		git:    map[string]remotessh.RuntimeGitReadResult{},
		gitErr: map[string]error{"status": remotessh.ErrRuntimeGitUnavailable},
	}
	backend := newRemoteBackend(runtime, &remotessh.RuntimeRootCapability{})
	snapshot, err := backend.Snapshot(context.Background())
	if err != nil || snapshot.Git.IsRepository || len(snapshot.Files) != 1 {
		t.Fatalf("snapshot = %#v, %v", snapshot, err)
	}
	for _, invalid := range []string{"../secret", "/etc/passwd", `C:\secret`, "dir\\file", "a/./b"} {
		if _, err := backend.Preview(context.Background(), invalid); err == nil {
			t.Fatalf("invalid remote path %q was accepted", invalid)
		}
	}
	if runtime.acquired != 1 {
		t.Fatalf("invalid paths acquired leases: %d", runtime.acquired)
	}
}

func TestRemoteBackendProjectsUnsupportedRegularFileAsBinary(t *testing.T) {
	runtime := &fakeRemoteRepositoryRuntime{
		git:    map[string]remotessh.RuntimeGitReadResult{"diff": remoteGitResult("diff", gitPart("staged", ""), gitPart("working", ""))},
		gitErr: map[string]error{}, stat: remotessh.RuntimeFileInfo{Kind: "file", Size: 42}, readErr: remotessh.ErrRuntimeFileUnsupported,
	}
	backend := newRemoteBackend(runtime, &remotessh.RuntimeRootCapability{})
	preview, err := backend.Preview(context.Background(), "asset.bin")
	if err != nil || preview.Size != 42 || !preview.Binary || preview.Content != "" {
		t.Fatalf("preview = %#v, %v", preview, err)
	}
}
