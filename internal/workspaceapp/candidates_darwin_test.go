//go:build darwin

package workspaceapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDarwinPyCharmDiscoverySeparatesApplicationEditions(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"PyCharm.app", "PyCharm Community Edition.app"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	professional := pyCharmDarwinApplications([]string{root}, false)
	community := pyCharmDarwinApplications([]string{root}, true)
	if len(professional) != 1 || filepath.Base(professional[0]) != "PyCharm.app" {
		t.Fatalf("professional applications = %#v", professional)
	}
	if len(community) != 1 || filepath.Base(community[0]) != "PyCharm Community Edition.app" {
		t.Fatalf("community applications = %#v", community)
	}
}

func TestDarwinBundleIconsPreferNamedApplicationIcon(t *testing.T) {
	application := filepath.Join(t.TempDir(), "Cursor.app")
	resources := filepath.Join(application, "Contents", "Resources")
	if err := os.MkdirAll(resources, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Cursor.icns", "document.icns"} {
		if err := os.WriteFile(filepath.Join(resources, name), []byte("icon"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	icons := bundleIcons([]string{application}, "Cursor.icns")
	if len(icons) != 2 || filepath.Base(icons[0]) != "Cursor.icns" {
		t.Fatalf("bundle icons = %#v", icons)
	}
}
