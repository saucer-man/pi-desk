package piruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func TestBuildPiArgsForRemoteAdapterDisablesLocalToolFallback(t *testing.T) {
	directory := t.TempDir()
	adapter := filepath.Join(directory, "adapter.ts")
	adapterContent := []byte("export default () => {}")
	if err := os.WriteFile(adapter, adapterContent, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(adapterContent)
	config := StartConfig{
		Trust: TrustApprove, RemoteAdapter: adapter, RemoteSocket: filepath.Join(directory, "broker.sock"),
		RemoteToken: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", RemoteRoot: "/srv/repo",
		RemoteAdapterSHA256: hex.EncodeToString(digest[:]), RemoteAdapterSize: int64(len(adapterContent)),
	}
	args, err := buildPiArgs(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"--no-builtin-tools", "--no-extensions", "--no-context-files", "--extension", adapter} {
		if !slices.Contains(args, expected) {
			t.Fatalf("missing %q in %#v", expected, args)
		}
	}
	environment, err := processEnvironment(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"PI_DESK_REMOTE_SOCKET=" + config.RemoteSocket, "PI_DESK_REMOTE_TOKEN=" + config.RemoteToken, "PI_DESK_REMOTE_ROOT=/srv/repo"} {
		if !slices.Contains(environment, expected) {
			t.Fatalf("missing remote environment %q", expected)
		}
	}
	if err := os.WriteFile(adapter, append(adapterContent, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := processEnvironment(config); err == nil {
		t.Fatal("modified remote adapter was accepted at process launch")
	}
}

func TestValidateWorkspaceCanonicalizesAndRejectsFiles(t *testing.T) {
	workspace := t.TempDir()
	resolved, err := validateWorkspace(filepath.Join(workspace, "."))
	if err != nil {
		t.Fatalf("validateWorkspace returned an error: %v", err)
	}
	canonical, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != filepath.Clean(canonical) {
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

func TestProcessEnvironmentStripsRemoteCapabilitiesCaseInsensitively(t *testing.T) {
	t.Setenv("pi_desk_remote_token", "stale-secret")
	environment, err := processEnvironment(StartConfig{})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, "PI_DESK_REMOTE_TOKEN") {
			t.Fatalf("local Pi inherited a remote capability: %q", entry)
		}
	}
}

func TestProcessEnvironmentAppliesProxyOnlyToChild(t *testing.T) {
	environment, err := processEnvironment(StartConfig{ProxyURL: "socks5://127.0.0.1:10800"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		expected := key + "=socks5://127.0.0.1:10800"
		if !slices.Contains(environment, expected) {
			t.Fatalf("missing %q", expected)
		}
	}
}
