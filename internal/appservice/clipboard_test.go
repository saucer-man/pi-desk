package appservice

import (
	"os"
	"path/filepath"
	"testing"

	"pi-desk/internal/domain"
	"pi-desk/internal/workspace"
)

func TestClipboardFilesBoundaries(t *testing.T) {
	root, err := workspace.CanonicalDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "需求 文档.md")
	outside := filepath.Join(t.TempDir(), "private.txt")
	for _, path := range []string{inside, outside} {
		if err := os.WriteFile(path, []byte("unchanged"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	resolvedOutside, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		paths     []string
		trust     string
		kind      workspace.Kind
		wantError bool
		wantCount int
		wantPath  string
	}{
		{"unicode and deduplication", []string{inside, inside}, "approve", workspace.KindLocal, false, 1, "需求 文档.md"},
		{"outside workspace uses absolute path", []string{outside}, "approve", workspace.KindLocal, false, 1, filepath.ToSlash(resolvedOutside)},
		{"mixed batch keeps both", []string{inside, outside}, "approve", workspace.KindLocal, false, 2, "需求 文档.md"},
		{"untrusted", []string{inside}, "deny", workspace.KindLocal, true, 0, ""},
		{"remote", []string{inside}, "approve", workspace.KindSSH, true, 0, ""},
		{"relative path", []string{"需求 文档.md"}, "approve", workspace.KindLocal, true, 0, ""},
		{"directory", []string{root}, "approve", workspace.KindLocal, true, 0, ""},
		{"deleted file", []string{filepath.Join(root, "missing")}, "approve", workspace.KindLocal, true, 0, ""},
		{"ordinary paste needs no trust", nil, "deny", workspace.KindSSH, false, 0, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := newRepositoryService(fakeWorkspaceResolver{record: workspace.Record{
				ID: "workspace", Path: root, Trust: test.trust, Location: workspace.Location{Kind: test.kind},
			}}, nil)
			service.clipboardFiles = func() ([]string, error) { return test.paths, nil }
			files, err := service.ClipboardFiles(domain.RepositoryRequest{WorkspaceID: "workspace"})
			if (err != nil) != test.wantError || len(files) != test.wantCount {
				t.Fatalf("files=%v error=%v", files, err)
			}
			if len(files) > 0 && (files[0].Path != test.wantPath || files[0].Name != filepath.Base(test.wantPath)) {
				t.Fatalf("unexpected file reference: %q wantPath=%q name=%q", files[0].Path, test.wantPath, files[0].Name)
			}
		})
	}
	if data, err := os.ReadFile(inside); err != nil || string(data) != "unchanged" {
		t.Fatalf("paste must not alter source files: %q %v", data, err)
	}
	link := filepath.Join(root, "escape.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	service := newRepositoryService(fakeWorkspaceResolver{record: workspace.Record{Path: root, Trust: "approve"}}, nil)
	service.clipboardFiles = func() ([]string, error) { return []string{link}, nil }
	files, err := service.ClipboardFiles(domain.RepositoryRequest{WorkspacePath: root})
	if err != nil || len(files) != 1 || files[0].Path != filepath.ToSlash(resolvedOutside) {
		t.Fatalf("symlink should resolve to an absolute reference: %v err=%v", files, err)
	}
}
