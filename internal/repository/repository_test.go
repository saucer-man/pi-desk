package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseStatusHandlesBranchTrackingAndRename(t *testing.T) {
	status, err := parseStatus([]byte("## feature...origin/feature [ahead 2, behind 1]\x00M  main.go\x00R  new.go\x00old.go\x00?? notes.txt\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if status.Branch != "feature" || status.Ahead != 2 || status.Behind != 1 {
		t.Fatalf("unexpected branch status: %#v", status)
	}
	if len(status.Files) != 3 || status.Files[1].Path != "new.go" || status.Files[1].OriginalPath != "old.go" {
		t.Fatalf("unexpected changed files: %#v", status.Files)
	}
}

func TestParseFilesRejectsEscapingPathsAndCapsResults(t *testing.T) {
	if _, _, err := parseFiles([]byte("../outside\x00")); err == nil {
		t.Fatal("expected an escaping path to fail")
	}
	files, truncated, err := parseFiles([]byte("src/main.go\x00README.md\x00"))
	if err != nil || truncated || len(files) != 2 || files[0].Path != "README.md" {
		t.Fatalf("unexpected files: %#v, truncated=%v, err=%v", files, truncated, err)
	}
}

func TestScannerUsesGitIgnoreAndReturnsWorkingTreeState(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "-q")
	runGitTest(t, root, "config", "user.email", "pi-desk@example.invalid")
	runGitTest(t, root, "config", "user.name", "Pi Desk Test")
	writeTestFile(t, root, ".gitignore", "ignored.txt\n")
	writeTestFile(t, root, "tracked.txt", "one\n")
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-qm", "initial")
	writeTestFile(t, root, "tracked.txt", "two\n")
	writeTestFile(t, root, "untracked.txt", "new\n")
	writeTestFile(t, root, "ignored.txt", "ignored\n")

	snapshot, err := New().Snapshot(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Git.IsRepository || len(snapshot.Git.Files) != 2 {
		t.Fatalf("unexpected git status: %#v", snapshot.Git)
	}
	paths := make(map[string]bool)
	for _, file := range snapshot.Files {
		paths[file.Path] = true
	}
	if !paths["tracked.txt"] || !paths["untracked.txt"] || paths["ignored.txt"] {
		t.Fatalf("git ignore semantics were not preserved: %#v", paths)
	}
}

func TestScannerReturnsStagedWorkingAndUntrackedDiffs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "-q")
	runGitTest(t, root, "config", "user.email", "pi-desk@example.invalid")
	runGitTest(t, root, "config", "user.name", "Pi Desk Test")
	writeTestFile(t, root, "tracked.txt", "one\n")
	runGitTest(t, root, "add", "tracked.txt")
	runGitTest(t, root, "commit", "-qm", "initial")
	writeTestFile(t, root, "tracked.txt", "two\n")
	runGitTest(t, root, "add", "tracked.txt")
	writeTestFile(t, root, "tracked.txt", "three\n")
	writeTestFile(t, root, "untracked.txt", "new file\n")

	scanner := New()
	tracked, err := scanner.Diff(context.Background(), root, "tracked.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tracked.Staged, "+two") || !strings.Contains(tracked.Working, "+three") || tracked.Content != "" {
		t.Fatalf("unexpected tracked diff: %#v", tracked)
	}
	untracked, err := scanner.Diff(context.Background(), root, "untracked.txt")
	if err != nil {
		t.Fatal(err)
	}
	if untracked.Content != "new file\n" || untracked.Binary || untracked.Truncated {
		t.Fatalf("unexpected untracked diff: %#v", untracked)
	}
	if _, err := scanner.Diff(context.Background(), root, "../outside.txt"); err == nil {
		t.Fatal("expected an escaping path to fail")
	}
}

func TestResolveFileRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if _, err := ResolveFile(root, "link.txt"); err == nil {
		t.Fatal("expected a symlink outside the workspace to fail")
	}
}

func TestPreviewFileReturnsMarkdownAndSafeMediaData(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	png := []byte("\x89PNG\r\n\x1a\ncontent")
	if err := os.WriteFile(filepath.Join(root, "image.png"), png, 0o600); err != nil {
		t.Fatal(err)
	}
	markdown, err := PreviewFile(root, "README.md")
	if err != nil || markdown.MediaType != "text/markdown" || markdown.Content != "# Hello" {
		t.Fatalf("unexpected markdown preview %#v, %v", markdown, err)
	}
	image, err := PreviewFile(root, "image.png")
	if err != nil || image.MediaType != "image/png" || !strings.HasPrefix(image.DataURL, "data:image/png;base64,") || !image.Binary {
		t.Fatalf("unexpected image preview %#v, %v", image, err)
	}
}

func TestParseBranchesTracksCurrentAndOccupiedBranches(t *testing.T) {
	root := t.TempDir()
	mainWorktree := filepath.Join(root, "repo")
	featureWorktree := filepath.Join(root, "feature")
	worktreeOutput := fmt.Sprintf(
		"worktree %s\x00HEAD abc\x00branch refs/heads/main\x00\x00worktree %s\x00HEAD def\x00branch refs/heads/feature\x00\x00",
		filepath.ToSlash(mainWorktree), filepath.ToSlash(featureWorktree),
	)
	worktrees, err := parseWorktreeBranches([]byte(worktreeOutput))
	if err != nil {
		t.Fatal(err)
	}
	output := []byte("refs/heads/main\tmain\t*\torigin/main\tabc123\t\n" +
		"refs/heads/feature\tfeature\t \t\tdef456\t\n" +
		"refs/remotes/origin/main\torigin/main\t \t\tabc123\t\n" +
		"refs/remotes/origin/HEAD\torigin/HEAD\t \t\tabc123\trefs/remotes/origin/main\n")
	branches, err := parseBranches(output, worktrees)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 3 || branches[0].Name != "main" || !branches[0].Current || branches[1].Name != "feature" {
		t.Fatalf("unexpected branches: %#v", branches)
	}
	if branches[1].WorktreePath != filepath.Clean(featureWorktree) || !branches[2].Remote {
		t.Fatalf("unexpected branch metadata: %#v", branches)
	}
}

func TestScannerListsLinkedWorktreeBranches(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "-q")
	runGitTest(t, root, "config", "user.email", "pi-desk@example.invalid")
	runGitTest(t, root, "config", "user.name", "Pi Desk Test")
	writeTestFile(t, root, "tracked.txt", "one\n")
	runGitTest(t, root, "add", "tracked.txt")
	runGitTest(t, root, "commit", "-qm", "initial")
	runGitTest(t, root, "branch", "feature")
	linked := filepath.Join(t.TempDir(), "feature")
	runGitTest(t, root, "worktree", "add", "-q", linked, "feature")

	inventory, err := New().Branches(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]Branch)
	for _, branch := range inventory.Branches {
		byName[branch.Name] = branch
	}
	if !byName[currentTestBranch(t, root)].Current {
		t.Fatalf("current branch not marked: %#v", inventory.Branches)
	}
	canonicalLinked, err := filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(byName["feature"].WorktreePath) != filepath.Clean(canonicalLinked) {
		t.Fatalf("linked worktree not reported: %#v", byName["feature"])
	}
}

func runGitTest(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, output)
	}
}

func currentTestBranch(t *testing.T, root string) string {
	t.Helper()
	command := exec.Command("git", "-C", root, "branch", "--show-current")
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(output))
}

func writeTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
