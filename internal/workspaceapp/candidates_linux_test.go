//go:build linux

package workspaceapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxDesktopEntryProjectsLauncherNameAndIcon(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cursor.desktop")
	content := "[Desktop Entry]\nName=Cursor Editor\nIcon=cursor\nExec=cursor %F\n\n[Desktop Action New]\nName=Ignored\nIcon=wrong\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	entry, ok := readDesktopEntry(path)
	if !ok || entry.name != "Cursor Editor" || entry.icon != "cursor" {
		t.Fatalf("entry = %#v, ok = %v", entry, ok)
	}
}

func TestLinuxPyCharmDesktopDiscoverySeparatesEditions(t *testing.T) {
	root := t.TempDir()
	professional := "[Desktop Entry]\nName=PyCharm Professional\nIcon=pycharm\n"
	community := "[Desktop Entry]\nName=PyCharm Community Edition\nIcon=pycharm-community\n"
	if err := os.WriteFile(filepath.Join(root, "jetbrains-pycharm.desktop"), []byte(professional), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "jetbrains-pycharm-community.desktop"), []byte(community), 0o600); err != nil {
		t.Fatal(err)
	}
	if entry := firstPyCharmDesktopEntry([]string{root}, false); entry.name != "PyCharm Professional" {
		t.Fatalf("professional entry = %#v", entry)
	}
	if entry := firstPyCharmDesktopEntry([]string{root}, true); entry.name != "PyCharm Community Edition" {
		t.Fatalf("community entry = %#v", entry)
	}
}
