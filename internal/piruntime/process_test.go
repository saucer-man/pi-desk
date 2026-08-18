package piruntime

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestBuildPiArgsRequiresTrustAndPreservesSessionOptions(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	args, err := buildPiArgs(StartConfig{
		SessionPath:    sessionPath,
		SessionName:    "Desktop task",
		Trust:          TrustApprove,
		Offline:        true,
		DisableThemes:  true,
		DisableSkills:  true,
		DisablePlugins: true,
	})
	if err != nil {
		t.Fatalf("buildPiArgs returned an error: %v", err)
	}
	for _, expected := range []string{"--mode", "rpc", "--session", sessionPath, "--name", "Desktop task", "--approve", "--offline", "--no-themes", "--no-skills", "--no-extensions"} {
		if !slices.Contains(args, expected) {
			t.Fatalf("missing %q in %#v", expected, args)
		}
	}

	if _, err := buildPiArgs(StartConfig{}); err == nil {
		t.Fatal("expected missing trust decision to fail")
	}
}

func TestValidateWorkspaceCanonicalizesAndRejectsFiles(t *testing.T) {
	workspace := t.TempDir()
	resolved, err := validateWorkspace(filepath.Join(workspace, "."))
	if err != nil {
		t.Fatalf("validateWorkspace returned an error: %v", err)
	}
	if resolved != filepath.Clean(workspace) {
		t.Fatalf("unexpected canonical workspace: %q", resolved)
	}

	file := filepath.Join(workspace, "file.txt")
	if err := os.WriteFile(file, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateWorkspace(file); err == nil {
		t.Fatal("expected a file path to be rejected")
	}
}

func TestProcessEnvironmentAppliesProxyOnlyToChild(t *testing.T) {
	environment := processEnvironment("socks5://127.0.0.1:10800")
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		expected := key + "=socks5://127.0.0.1:10800"
		if !slices.Contains(environment, expected) {
			t.Fatalf("missing %q", expected)
		}
	}
}
