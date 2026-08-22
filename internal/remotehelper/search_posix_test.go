//go:build linux || darwin

package remotehelper

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func openSearchRoot(t *testing.T, directory string) (*rootManager, string) {
	t.Helper()
	manager := newRootManager()
	opened, err := manager.Open(context.Background(), filepath.ToSlash(directory))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	return manager, opened.Handle
}

func TestRootManagerFindAndGrepUseBoundedCandidates(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"main.go":                 "package main\nfunc main() {}\n",
		"nested/readme.md":        "alpha\nbeta match\n",
		"nested/other.go":         "// match here\n",
		"node_modules/secret.txt": "match secret\n",
		"binary.dat":              "match\x00binary\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manager, handle := openSearchRoot(t, root)
	found, err := manager.Find(context.Background(), SearchFindRequest{RootHandle: handle, Path: ".", Pattern: "*.go", Limit: 10})
	if err != nil || !slices.Equal(found.Paths, []string{"main.go", "nested/other.go"}) {
		t.Fatalf("find=%#v err=%v", found, err)
	}
	found, err = manager.Find(context.Background(), SearchFindRequest{RootHandle: handle, Path: "nested", Pattern: "**/*.md", Limit: 10})
	if err != nil || !slices.Equal(found.Paths, []string{"nested/readme.md"}) {
		t.Fatalf("nested find=%#v err=%v", found, err)
	}
	grep, err := manager.Grep(context.Background(), SearchGrepRequest{RootHandle: handle, Path: ".", Pattern: "match", Glob: "*.{go,md,dat}", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(grep.Matches) != 2 || grep.Matches[0].Path != "nested/other.go" || grep.Matches[1].Path != "nested/readme.md" || grep.SkippedUnsupportedPaths != 1 {
		t.Fatalf("grep=%#v", grep)
	}
	if slices.ContainsFunc(grep.Matches, func(match SearchGrepMatch) bool {
		return strings.Contains(match.Path, "node_modules") || strings.Contains(match.Path, ".git")
	}) {
		t.Fatalf("excluded directory leaked: %#v", grep.Matches)
	}
}

func TestRootManagerSearchRejectsInvalidAndCancelledRequests(t *testing.T) {
	manager, handle := openSearchRoot(t, t.TempDir())
	invalid := []SearchFindRequest{
		{RootHandle: handle, Path: ".", Pattern: "../*", Limit: 10},
		{RootHandle: handle, Path: ".", Pattern: "*", Limit: 0},
		{RootHandle: handle, Path: "../outside", Pattern: "*", Limit: 10},
	}
	for _, request := range invalid {
		if _, err := manager.Find(context.Background(), request); !errors.Is(err, ErrSearchInvalid) {
			t.Fatalf("request=%#v err=%v", request, err)
		}
	}
	if _, err := manager.Grep(context.Background(), SearchGrepRequest{RootHandle: handle, Path: ".", Pattern: "[", Limit: 10}); !errors.Is(err, ErrSearchInvalid) {
		t.Fatalf("invalid regexp error=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.Find(ctx, SearchFindRequest{RootHandle: handle, Path: ".", Pattern: "*", Limit: 10}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled find error=%v", err)
	}
}

func TestRootManagerGitSearchUsesTrackedAndUntrackedCandidatesWithoutFilters(t *testing.T) {
	if _, err := os.Stat("/usr/bin/git"); err != nil {
		t.Skip("system Git unavailable")
	}
	root := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		command := exec.Command("/usr/bin/git", args...)
		command.Dir = root
		command.Env = []string{"HOME=/nonexistent", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "LC_ALL=C"}
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	runGit("init", "-q")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("tracked match\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("untracked match\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.txt"), []byte("ignored match\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "tracked.txt", ".gitignore")
	manager, handle := openSearchRoot(t, root)
	found, err := manager.Find(context.Background(), SearchFindRequest{RootHandle: handle, Path: ".", Pattern: "*.txt", Limit: 10})
	if err != nil || !slices.Equal(found.Paths, []string{"tracked.txt", "untracked.txt"}) {
		t.Fatalf("git find=%#v err=%v", found, err)
	}
}
