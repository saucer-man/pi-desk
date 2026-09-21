package repository

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"pi-desk/internal/sessionindex"
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

func TestPreviewFileReturnsSpreadsheetSheets(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "report.xlsx")
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	parts := map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Summary" sheetId="1" r:id="rId1"/><sheet name="Details" sheetId="2" r:id="rId2"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Target="worksheets/sheet2.xml"/></Relationships>`,
		"xl/sharedStrings.xml":       `<sst><si><t>Name</t></si><si><t>Total</t></si><si><r><t>North</t></r><r><t> region</t></r></si></sst>`,
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row><row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2"><v>42</v></c></row></sheetData></worksheet>`,
		"xl/worksheets/sheet2.xml":   `<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>Ready</t></is></c><c r="B1" t="b"><v>1</v></c></row></sheetData></worksheet>`,
	}
	for name, content := range parts {
		entry, createErr := archive.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := entry.Write([]byte(content)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	preview, err := PreviewFile(root, "report.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if preview.MediaType != spreadsheetMediaType || !preview.Binary || preview.Truncated {
		t.Fatalf("unexpected spreadsheet preview: %#v", preview)
	}
	var document spreadsheetPreview
	if err := json.Unmarshal([]byte(preview.Content), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Sheets) != 2 || document.Sheets[0].Name != "Summary" || document.Sheets[0].Columns != 2 {
		t.Fatalf("unexpected spreadsheet sheets: %#v", document.Sheets)
	}
	if document.Sheets[0].Rows[1][0] != "North region" || document.Sheets[0].Rows[1][1] != "42" {
		t.Fatalf("unexpected spreadsheet values: %#v", document.Sheets[0].Rows)
	}
	if document.Sheets[1].Rows[0][0] != "Ready" || document.Sheets[1].Rows[0][1] != "TRUE" {
		t.Fatalf("unexpected second sheet: %#v", document.Sheets[1].Rows)
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

func TestRestoreSessionFileRevertsRecordedEditsInReverse(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	writeTestFile(t, root, "code.txt", "alpha\nbeta\ngamma\n")
	scanner := New()
	operations := []sessionindex.FileOperation{
		{Path: "code.txt", Edits: []sessionindex.TextEdit{{Old: "beta", New: "BETA"}}},
		{Path: "code.txt", Edits: []sessionindex.TextEdit{{Old: "BETA\ngamma", New: "BETA\ndelta"}}},
	}
	writeTestFile(t, root, "code.txt", "alpha\nBETA\ndelta\n")

	plan, err := scanner.RestoreSessionFile(context.Background(), root, "code.txt", operations)
	if err != nil || plan != PlanRevertEdits {
		t.Fatalf("unexpected restore result: plan=%s err=%v", plan, err)
	}
	if content := readTestFile(t, root, "code.txt"); content != "alpha\nbeta\ngamma\n" {
		t.Fatalf("unexpected restored content: %q", content)
	}
}

func TestRestoreSessionFileRefusesContentThatNoLongerMatches(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "code.txt", "someone else edited\n")
	operations := []sessionindex.FileOperation{
		{Path: "code.txt", Edits: []sessionindex.TextEdit{{Old: "beta", New: "BETA"}}},
	}
	if _, err := New().RestoreSessionFile(context.Background(), root, "code.txt", operations); err == nil {
		t.Fatal("expected a mismatching file to refuse rollback")
	}
}

func TestRestoreSessionFileRestoresRewrittenTrackedFileFromHead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "-q", "-b", "main")
	runGitTest(t, root, "config", "user.email", "pi-desk@example.invalid")
	runGitTest(t, root, "config", "user.name", "Pi Desk Test")
	runGitTest(t, root, "config", "core.autocrlf", "false")
	writeTestFile(t, root, "code.txt", "committed\n")
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-qm", "initial")
	writeTestFile(t, root, "code.txt", "overwritten by the session\n")

	plan, err := New().RestoreSessionFile(context.Background(), root, "code.txt", []sessionindex.FileOperation{
		{Path: "code.txt", Write: true},
	})
	if err != nil || plan != PlanGitRestore {
		t.Fatalf("unexpected restore result: plan=%s err=%v", plan, err)
	}
	if content := readTestFile(t, root, "code.txt"); content != "committed\n" {
		t.Fatalf("unexpected restored content: %q", content)
	}
}

func TestRestoreSessionFileDeletesFileCreatedBySession(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "-q", "-b", "main")
	runGitTest(t, root, "config", "user.email", "pi-desk@example.invalid")
	runGitTest(t, root, "config", "user.name", "Pi Desk Test")
	writeTestFile(t, root, "tracked.txt", "base\n")
	runGitTest(t, root, "add", "tracked.txt")
	runGitTest(t, root, "config", "core.autocrlf", "false")
	runGitTest(t, root, "commit", "-qm", "initial")
	writeTestFile(t, root, "created.txt", "new file\n")
	runGitTest(t, root, "add", "created.txt")

	plan, err := New().RestoreSessionFile(context.Background(), root, "created.txt", []sessionindex.FileOperation{
		{Path: "created.txt", Write: true},
	})
	if err != nil || plan != PlanDeleteFile {
		t.Fatalf("unexpected restore result: plan=%s err=%v", plan, err)
	}
	if _, err := os.Stat(filepath.Join(root, "created.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected the created file to be deleted: err=%v", err)
	}
	output, err := exec.Command("git", "-C", root, "status", "--porcelain").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "" {
		t.Fatalf("expected a clean worktree after deletion: err=%v output=%s", err, output)
	}
}

func TestSessionChangesClassifiesRollbackPlans(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "-q", "-b", "main")
	runGitTest(t, root, "config", "user.email", "pi-desk@example.invalid")
	runGitTest(t, root, "config", "user.name", "Pi Desk Test")
	writeTestFile(t, root, "edited.txt", "base\n")
	writeTestFile(t, root, "tracked.txt", "base\n")
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-qm", "initial")
	writeTestFile(t, root, "created.txt", "new\n")
	writeTestFile(t, root, "tracked.txt", "rewritten\n")

	operations := map[string][]sessionindex.FileOperation{
		"created.txt": {{Path: "created.txt", Write: true}},
		"edited.txt":  {{Path: "edited.txt", Edits: []sessionindex.TextEdit{{Old: "base", New: "edited"}}}},
		"tracked.txt": {{Path: "tracked.txt", Write: true}, {Path: "tracked.txt", Edits: []sessionindex.TextEdit{{Old: "a", New: "b"}}}},
	}
	changes, err := New().SessionChanges(context.Background(), root, operations)
	if err != nil {
		t.Fatal(err)
	}
	plans := make(map[string]SessionFileChange, len(changes))
	for _, change := range changes {
		plans[change.Path] = change
	}
	if plans["edited.txt"].Plan != PlanRevertEdits || plans["edited.txt"].EditCalls != 1 {
		t.Fatalf("unexpected edit-only plan: %#v", plans["edited.txt"])
	}
	if plans["created.txt"].Plan != PlanDeleteFile || plans["created.txt"].WriteCalls != 1 {
		t.Fatalf("unexpected created-file plan: %#v", plans["created.txt"])
	}
	if plans["tracked.txt"].Plan != PlanGitRestore || plans["tracked.txt"].WriteCalls != 1 || plans["tracked.txt"].EditCalls != 1 {
		t.Fatalf("unexpected rewritten-file plan: %#v", plans["tracked.txt"])
	}
}

func readTestFile(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
