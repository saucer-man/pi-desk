//go:build windows

package workspaceapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsElectronIconDiscoveryPrefersNewestInstalledAsset(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "Code.exe")
	if err := os.WriteFile(executable, []byte("exe"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"version-a", "version-b"} {
		icon := filepath.Join(root, version, "resources", "app", "resources", "win32", "code.ico")
		if err := os.MkdirAll(filepath.Dir(icon), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(icon, []byte("icon"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths := electronWindowsIconPaths(executable, "code.ico")
	if len(paths) != 2 || filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(paths[0])))))) != "version-b" {
		t.Fatalf("icon paths = %#v", paths)
	}
}

func TestWindowsPyCharmDiscoverySeparatesEditions(t *testing.T) {
	root := t.TempDir()
	professional := filepath.Join(root, "JetBrains", "PyCharm 2026.1", "bin", "pycharm64.exe")
	community := filepath.Join(root, "JetBrains", "PyCharm Community Edition 2026.1", "bin", "pycharm64.exe")
	for _, executable := range []string{professional, community} {
		if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(executable, []byte("exe"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if paths := pyCharmWindowsPaths(false, root); len(paths) != 1 || paths[0] != professional {
		t.Fatalf("professional paths = %#v", paths)
	}
	if paths := pyCharmWindowsPaths(true, root); len(paths) != 1 || paths[0] != community {
		t.Fatalf("community paths = %#v", paths)
	}
}
