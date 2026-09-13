package browser

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadPortFile(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if _, _, err := ReadPortFile(directory); !errors.Is(err, ErrBrowserNotRunning) {
		t.Fatalf("expected ErrBrowserNotRunning for a missing port file, got %v", err)
	}

	if err := os.WriteFile(filepath.Join(directory, portFileName), []byte("0\n/devtools/browser/x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadPortFile(directory); err == nil {
		t.Fatal("expected port 0 to be rejected")
	}

	if err := os.WriteFile(filepath.Join(directory, portFileName), []byte("9223\n/devtools/browser/guid"), 0o600); err != nil {
		t.Fatal(err)
	}
	port, wsPath, err := ReadPortFile(directory)
	if err != nil || port != 9223 || wsPath != "/devtools/browser/guid" {
		t.Fatalf("unexpected port file parse: port=%d path=%q err=%v", port, wsPath, err)
	}
}

func TestParseKeySpec(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  KeySpec
	}{
		{"Enter", KeySpec{Key: "Enter", Code: "Enter", VirtualKey: 13, Text: "\r"}},
		{"esc", KeySpec{Key: "Escape", Code: "Escape", VirtualKey: 27}},
		{"space", KeySpec{Key: " ", Code: "Space", VirtualKey: 32, Text: " "}},
		{"ArrowLeft", KeySpec{Key: "ArrowLeft", Code: "ArrowLeft", VirtualKey: 37}},
		{"F12", KeySpec{Key: "F12", Code: "F12", VirtualKey: 123}},
		{"a", KeySpec{Key: "a", Code: "KeyA", VirtualKey: 65, Text: "a"}},
		{"5", KeySpec{Key: "5", Code: "Digit5", VirtualKey: 53, Text: "5"}},
	}
	for _, testCase := range cases {
		got, err := ParseKeySpec(testCase.input)
		if err != nil || got != testCase.want {
			t.Fatalf("ParseKeySpec(%q) = %#v, %v; want %#v", testCase.input, got, err, testCase.want)
		}
	}
	for _, invalid := range []string{"", "  ", "ctrl+a", "abc", "F13"} {
		if _, err := ParseKeySpec(invalid); err == nil {
			t.Fatalf("ParseKeySpec(%q) unexpectedly succeeded", invalid)
		}
	}
}

func TestProfileDirUnderLocalAppData(t *testing.T) {
	t.Setenv("LOCALAPPDATA", filepath.Join(t.TempDir(), "local"))
	directory, err := ProfileDir()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(directory) != "browser" || filepath.Base(filepath.Dir(directory)) != "pi-desk" {
		t.Fatalf("unexpected profile directory %q", directory)
	}
}
