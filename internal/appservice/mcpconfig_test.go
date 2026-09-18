package appservice

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"pi-desk/internal/domain"
	"pi-desk/internal/workspace"
)

func TestMcpConfigServicePreservesUnknownFieldsAndManagesGlobalConfig(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	agent := filepath.Join(root, "agent")
	if err := os.MkdirAll(agent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agent, "mcp.json"), []byte("{\n  \"settings\": {\"toolPrefix\": \"server\"},\n  \"mcpServers\": {\"docs\": {\"url\": \"https://example.test/mcp\"}}\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := newMcpConfigService(agent, nil)

	snapshot, err := service.ListMcpServers(domain.ListMcpServersRequest{WorkspacePath: filepath.Join(root, "ignored-project")})
	if err != nil || snapshot.ProjectEnabled || snapshot.ProjectPath != "" || len(snapshot.Servers) != 1 || snapshot.Servers[0].Transport != "http" {
		t.Fatalf("unexpected global MCP snapshot %#v, %v", snapshot, err)
	}
	if _, err := service.UpsertMcpServer(domain.UpsertMcpServerRequest{
		Scope: domain.McpConfigScopeGlobal, OriginalName: "docs", Name: "docs",
		Definition: "{\"url\":\"https://example.test/v2/mcp\",\"headers\":{\"X-Test\":\"kept\"}}",
	}); err != nil {
		t.Fatal(err)
	}
	globalContent, err := os.ReadFile(filepath.Join(agent, "mcp.json"))
	if err != nil || !strings.Contains(string(globalContent), "toolPrefix") || !strings.Contains(string(globalContent), "X-Test") {
		t.Fatalf("global unknown fields were not preserved: %v, %s", err, globalContent)
	}
}

func TestMcpConfigServiceListsProjectPiOverride(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	projectConfig := filepath.Join(project, ".pi", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(projectConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectConfig, []byte(`{"mcpServers":{"local":{"command":"node","args":["server.js"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	unsupportedConfig := filepath.Join(project, ".agents", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(unsupportedConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unsupportedConfig, []byte(`{"mcpServers":{"ignored":{"command":"node"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service := newMcpConfigService(filepath.Join(root, "agent"), fakeWorkspaceResolver{record: workspace.Record{Path: project, Trust: "approve"}})

	snapshot, err := service.ListMcpServers(domain.ListMcpServersRequest{WorkspacePath: project})
	if err != nil || !snapshot.ProjectEnabled || snapshot.ProjectPath != projectConfig || len(snapshot.Servers) != 1 {
		t.Fatalf("unexpected project MCP snapshot %#v, %v", snapshot, err)
	}
	if snapshot.Servers[0].Scope != domain.McpConfigScopeProject || snapshot.Servers[0].Name != "local" {
		t.Fatalf("unexpected project MCP server %#v", snapshot.Servers[0])
	}
}

func TestMcpConfigServiceUsesAdapterEffectiveConfiguration(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is unavailable")
	}
	root := t.TempDir()
	agent := filepath.Join(root, "agent")
	moduleDirectory := filepath.Join(agent, "npm", "node_modules", "pi-mcp-adapter")
	if err := os.MkdirAll(filepath.Join(moduleDirectory, "dist"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDirectory, "package.json"), []byte(`{"type":"module"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	module := `
export function loadMcpConfig() { return { mcpServers: { shared: { url: "https://example.test/mcp", headers: { "X-Test": "kept" } } } }; }
export function getMcpDiscoverySummary() { return { sources: [{ id: "shared-project", label: "project standard MCP", path: "PROJECT/.mcp.json", exists: true, scope: "project", kind: "shared", serverCount: 1 }], agentPlugins: [] }; }
export function getServerProvenance() { return new Map([["shared", { kind: "project" }]]); }
`
	if err := os.WriteFile(filepath.Join(moduleDirectory, "dist", "config.js"), []byte(module), 0o600); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	service := newMcpConfigService(agent, fakeWorkspaceResolver{record: workspace.Record{Path: project, Trust: "approve"}})

	snapshot, err := service.ListMcpServers(domain.ListMcpServersRequest{WorkspacePath: project})
	if err != nil || snapshot.AdapterNotice != "" || len(snapshot.EffectiveServers) != 1 || len(snapshot.Sources) != 1 {
		t.Fatalf("unexpected adapter MCP snapshot %#v, %v", snapshot, err)
	}
	server := snapshot.EffectiveServers[0]
	if server.Name != "shared" || server.Scope != domain.McpConfigScopeProject || !strings.Contains(server.Definition, "X-Test") {
		t.Fatalf("unexpected effective MCP server %#v", server)
	}
}

func TestMcpConfigServiceRenamesAndDeletesServer(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	service := newMcpConfigService(filepath.Join(root, "agent"), nil)
	if _, err := service.UpsertMcpServer(domain.UpsertMcpServerRequest{Scope: domain.McpConfigScopeGlobal, Name: "old", Definition: "{\"command\":\"node\"}"}); err != nil {
		t.Fatal(err)
	}
	renamed, err := service.UpsertMcpServer(domain.UpsertMcpServerRequest{Scope: domain.McpConfigScopeGlobal, OriginalName: "old", Name: "new", Definition: "{\"command\":\"node\",\"args\":[\"server.js\"]}"})
	if err != nil || renamed.Name != "new" {
		t.Fatalf("unexpected renamed server %#v, %v", renamed, err)
	}
	if _, err := service.GetMcpServer(domain.McpServerRequest{Scope: domain.McpConfigScopeGlobal, Name: "old"}); err == nil {
		t.Fatal("expected old server name to be absent")
	}
	if err := service.DeleteMcpServer(domain.McpServerRequest{Scope: domain.McpConfigScopeGlobal, Name: "new"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetMcpServer(domain.McpServerRequest{Scope: domain.McpConfigScopeGlobal, Name: "new"}); err == nil {
		t.Fatal("expected deleted server to be absent")
	}
}

func TestMcpConfigServiceRejectsProjectScopeAndUnsafeDefinitions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	service := newMcpConfigService(filepath.Join(root, "agent"), nil)
	if _, err := service.UpsertMcpServer(domain.UpsertMcpServerRequest{Scope: domain.McpConfigScopeProject, WorkspacePath: filepath.Join(root, "project"), Name: "local", Definition: "{\"command\":\"node\"}"}); err == nil || !strings.Contains(err.Error(), "only global") {
		t.Fatalf("expected project MCP scope to be rejected, got %v", err)
	}
	if _, err := service.UpsertMcpServer(domain.UpsertMcpServerRequest{Scope: domain.McpConfigScopeGlobal, Name: "../outside", Definition: "{\"command\":\"node\"}"}); err == nil {
		t.Fatal("expected unsafe server name to fail")
	}
	if _, err := service.UpsertMcpServer(domain.UpsertMcpServerRequest{Scope: domain.McpConfigScopeGlobal, Name: "missing", Definition: "{\"disabled\":true}"}); err == nil {
		t.Fatal("expected missing transport to fail")
	}
	if _, err := service.UpsertMcpServer(domain.UpsertMcpServerRequest{Scope: domain.McpConfigScopeGlobal, Name: "multiple", Definition: "{\"command\":\"node\",\"url\":\"https://example.test\"}"}); err == nil {
		t.Fatal("expected multiple transports to fail")
	}
}

func TestMcpConfigServiceTestServerValidatesBeforeStartingClient(t *testing.T) {
	t.Parallel()
	service := newMcpConfigService(filepath.Join(t.TempDir(), "agent"), nil)
	if _, err := service.TestMcpServer(domain.TestMcpServerRequest{Definition: `{"disabled":true}`}); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected invalid definition error, got %v", err)
	}
	if _, err := service.TestMcpServer(domain.TestMcpServerRequest{Definition: `{"command":"node"}`}); err == nil || !strings.Contains(err.Error(), "install or update") {
		t.Fatalf("expected missing adapter error, got %v", err)
	}
}

func TestMcpConfigServiceEngineStatusDetectsAdapterAndShadowPaths(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	agent := filepath.Join(root, "agent")
	if err := os.MkdirAll(agent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agent, "settings.json"), []byte(`{"packages":["npm:other",{"source":"npm:pi-mcp-adapter@1.2.0"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	shadow := filepath.Join(root, ".config", "mcp", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(shadow), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shadow, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, "project")
	projectShadows := []string{filepath.Join(project, ".mcp.json")}
	for _, projectShadow := range projectShadows {
		if err := os.MkdirAll(filepath.Dir(projectShadow), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(projectShadow, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	service := newMcpConfigService(agent, fakeWorkspaceResolver{record: workspace.Record{Path: project, Location: workspace.Location{Kind: workspace.KindLocal}}})

	status, err := service.GetMcpEngineStatus(domain.McpEngineStatusRequest{WorkspacePath: project})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Installed || !status.Enabled || status.Source != "npm:pi-mcp-adapter@1.2.0" {
		t.Fatalf("unexpected engine status %#v", status)
	}
	expectedShadows := append([]string{shadow}, projectShadows...)
	if len(status.ShadowedPaths) != len(expectedShadows) {
		t.Fatalf("unexpected shadowed paths %#v", status.ShadowedPaths)
	}
	for index, expected := range expectedShadows {
		if status.ShadowedPaths[index] != expected {
			t.Fatalf("unexpected shadowed paths %#v", status.ShadowedPaths)
		}
	}

	empty, err := newMcpConfigService(filepath.Join(root, "missing-agent"), nil).GetMcpEngineStatus(domain.McpEngineStatusRequest{WorkspacePath: project})
	if err != nil || empty.Installed || len(empty.ShadowedPaths) != 1 {
		t.Fatalf("unexpected empty engine status %#v, %v", empty, err)
	}
}

func TestMcpConfigServiceListImportableMcpServersScansHostConfigs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("AppData", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	if err := os.WriteFile(filepath.Join(root, ".claude.json"), []byte(`{"mcpServers":{"fs":{"type":"stdio","command":"npx","args":["-y","@mcp/fs"]},"broken":{"foo":1}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cursor := filepath.Join(root, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(cursor), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cursor, []byte("// .cursor/mcp.json\n{\"mcpServers\":{\"git\":{\"command\":\"uvx\",\"args\":[\"mcp-server-git\"]}}}"), 0o600); err != nil {
		t.Fatal(err)
	}

	candidates, err := newMcpConfigService(filepath.Join(root, "agent"), nil).ListImportableMcpServers()
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 {
		t.Fatalf("unexpected import candidates %#v", candidates)
	}
	if candidates[0].Host != "Claude Code" || candidates[0].Name != "fs" {
		t.Fatalf("unexpected first candidate %#v", candidates[0])
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(candidates[0].Definition), &parsed); err != nil || parsed["command"] != "npx" {
		t.Fatalf("first definition was not preserved: %v, %s", err, candidates[0].Definition)
	}
	if candidates[1].Host != "Cursor" || candidates[1].Name != "git" {
		t.Fatalf("unexpected second candidate %#v", candidates[1])
	}
	if !strings.Contains(candidates[1].Definition, "mcp-server-git") {
		t.Fatalf("definition was not preserved: %s", candidates[1].Definition)
	}
}
