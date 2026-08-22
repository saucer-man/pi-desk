//go:build linux || darwin

package remotehelper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRootManagerGitReadUsesFixedOperationsAndDisablesFilters(t *testing.T) {
	if _, err := os.Stat("/usr/bin/git"); err != nil {
		t.Skip("system Git unavailable")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		command := exec.Command("/usr/bin/git", args...)
		command.Dir = root
		command.Env = safeGitEnvironment()
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("*.txt filter=evil\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".gitattributes", "file.txt")
	marker := filepath.Join(root, "filter-ran")
	run("config", "filter.evil.clean", "sh -c 'touch "+marker+"; cat'")
	run("config", "filter.evil.required", "true")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "file.txt")
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("filter fixture was not executable: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("third\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager, handle := openSearchRoot(t, root)
	for _, request := range []GitReadRequest{
		{RootHandle: handle, Operation: "status"},
		{RootHandle: handle, Operation: "files"},
		{RootHandle: handle, Operation: "diff", Path: "file.txt"},
		{RootHandle: handle, Operation: "branches"},
	} {
		response, blob, err := manager.Git(context.Background(), request)
		if err != nil || response.Operation != request.Operation {
			t.Fatalf("git %s response=%#v err=%v", request.Operation, response, err)
		}
		for _, part := range response.Parts {
			end := part.Offset + part.Size
			if part.Offset < 0 || end < part.Offset || end > int64(len(blob)) {
				t.Fatalf("invalid Git part=%#v blob=%d", part, len(blob))
			}
			digest := sha256.Sum256(blob[part.Offset:end])
			if part.SHA256 != hex.EncodeToString(digest[:]) {
				t.Fatalf("Git digest mismatch: %#v", part)
			}
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Git filter executed: %v", err)
	}
}

func TestRootManagerGitReadRejectsGenericAndNestedRoots(t *testing.T) {
	root := t.TempDir()
	command := exec.Command("/usr/bin/git", "init", "-q")
	command.Dir = root
	command.Env = safeGitEnvironment()
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("system Git unavailable: %v: %s", err, output)
	}
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager, handle := openSearchRoot(t, filepath.Join(root, "nested"))
	if _, _, err := manager.Git(context.Background(), GitReadRequest{RootHandle: handle, Operation: "status"}); !errors.Is(err, ErrGitUnavailable) {
		t.Fatalf("nested Git root error=%v", err)
	}
	if _, _, err := manager.Git(context.Background(), GitReadRequest{RootHandle: handle, Operation: "run", Path: "status"}); !errors.Is(err, ErrGitInvalid) {
		t.Fatalf("generic Git operation error=%v", err)
	}
}

func TestSupportedGitVersion(t *testing.T) {
	for value, expected := range map[string]bool{
		"git version 2.31.0": true,
		"git version 3.0.0":  true,
		"git version 2.30.9": false,
		"git version bad":    false,
	} {
		if supportedGitVersion(value) != expected {
			t.Fatalf("version %q", value)
		}
	}
}
