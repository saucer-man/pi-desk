package workspaceapp

import (
	"errors"
	"image"
	"image/color"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestManagerListsOnlyUsableUniqueApplications(t *testing.T) {
	manager := &Manager{discover: func() []candidate {
		return []candidate{
			{Application: Application{ID: VSCodeID, Name: "Visual Studio Code"}, executable: "/apps/code"},
			{Application: Application{ID: VSCodeID, Name: "Duplicate"}, executable: "/apps/duplicate"},
			{Application: Application{ID: CursorID, Name: "Cursor"}},
			{Application: Application{ID: FileManagerID, Name: "Files"}, executable: "/apps/files"},
		}
	}}

	got := manager.List()
	if len(got) != 2 || got[0].ID != VSCodeID || got[0].Name != "Visual Studio Code" || got[1].ID != FileManagerID || got[1].Name != "Files" {
		t.Fatalf("List() = %#v", got)
	}
	for _, application := range got {
		if !strings.HasPrefix(application.IconDataURL, "data:image/png;base64,") {
			t.Fatalf("%s has invalid icon data URL", application.ID)
		}
	}
}

func TestManagerCachesNormalizedNativeIconsForTheProcess(t *testing.T) {
	calls := 0
	source := image.NewNRGBA(image.Rect(0, 0, 120, 80))
	for y := 20; y < 60; y++ {
		for x := 40; x < 80; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: 240, G: 20, B: 40, A: 255})
		}
	}
	manager := &Manager{
		discover: func() []candidate {
			return []candidate{{Application: Application{ID: CursorID, Name: "Cursor"}, executable: "/apps/cursor"}}
		},
		loadIcon: func(candidate) (image.Image, error) {
			calls++
			return source, nil
		},
	}

	first := manager.List()
	second := manager.List()
	if calls != 1 {
		t.Fatalf("native icon loader called %d times", calls)
	}
	if len(first) != 1 || len(second) != 1 || first[0].IconDataURL == "" || first[0].IconDataURL != second[0].IconDataURL {
		t.Fatalf("unexpected cached applications: %#v, %#v", first, second)
	}
	if bounds := visibleBounds(normalizeIcon(source)); bounds.Min.X != workspaceIconPadding || bounds.Min.Y != workspaceIconPadding || bounds.Max.X != workspaceIconSize-workspaceIconPadding || bounds.Max.Y != workspaceIconSize-workspaceIconPadding {
		t.Fatalf("normalized visible bounds = %v", bounds)
	}
}

func TestReviewedFallbackIconsDecode(t *testing.T) {
	for _, id := range []string{VSCodeID, CursorID, PyCharmID, PyCharmCommunityID, FileManagerID} {
		icon, err := fallbackIcon(id)
		if err != nil {
			t.Fatalf("fallbackIcon(%q): %v", id, err)
		}
		if iconDataURL(icon) == "" {
			t.Fatalf("fallbackIcon(%q) produced no PNG data URL", id)
		}
	}
	if _, err := fallbackIcon(VSCodeInsidersID); err == nil {
		t.Fatal("VS Code Insiders must not reuse the stable edition fallback")
	}
}

func TestManagerOpensDirectoryWithDiscoveredExecutableAndArgumentVector(t *testing.T) {
	workspace := t.TempDir()
	var executable string
	var args []string
	manager := &Manager{
		discover: func() []candidate {
			return []candidate{{
				Application: Application{ID: VSCodeID, Name: "Visual Studio Code"},
				executable:  "/apps/code",
				prefixArgs:  []string{"--reuse-window"},
			}}
		},
		start: func(name string, commandArgs ...string) error {
			executable = name
			args = append([]string{}, commandArgs...)
			return nil
		},
	}

	if err := manager.Open(VSCodeID, workspace); err != nil {
		t.Fatal(err)
	}
	if executable != "/apps/code" {
		t.Fatalf("executable = %q", executable)
	}
	wantArgs := []string{"--reuse-window", filepath.Clean(workspace)}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
}

func TestManagerRejectsUnknownApplicationAndNonDirectory(t *testing.T) {
	workspace := t.TempDir()
	started := false
	manager := &Manager{
		discover: func() []candidate {
			return []candidate{{Application: Application{ID: VSCodeID, Name: "Visual Studio Code"}, executable: "/apps/code"}}
		},
		start: func(string, ...string) error { started = true; return errors.New("should not run") },
	}

	if err := manager.Open("arbitrary-command", workspace); err == nil {
		t.Fatal("expected unknown application to be rejected")
	}
	if err := manager.Open(VSCodeID, filepath.Join(workspace, "missing")); err == nil {
		t.Fatal("expected missing workspace to be rejected")
	}
	if started {
		t.Fatal("launcher ran for a rejected request")
	}
}
