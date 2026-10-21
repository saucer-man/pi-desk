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
	if _, err := service.TestMcpServer(domain.TestMcpServerRequest{Definition: `{"disabled":true}`}); err == nil || !strings.Contains(err.Error(), "trusted project") {
		t.Fatalf("expected invalid definition error, got %v", err)
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

func TestNativeMcpDefinitionMigratesDisabledAndRejectsLegacyTransports(t *testing.T) {
	definition, _, err := parseMcpDefinition(`{"command":"node","disabled":true,"headers":{"X-Test":"kept"}}`)
	if err != nil || definition["enabled"] != false || definition["disabled"] != nil {
		t.Fatalf("invalid migration: %#v, %v", definition, err)
	}
	for _, value := range []string{`{"socket":"/tmp/mcp"}`, `{"url":"https://example.test","type":"sse"}`} {
		if _, _, err := parseMcpDefinition(value); err == nil {
			t.Fatalf("accepted unsupported transport: %s", value)
		}
	}
}

func TestMcpNativeOverridesAndLegacyNames(t *testing.T) {
	root := t.TempDir()
	agent := filepath.Join(root, "agent")
	project := filepath.Join(root, "project")
	service := newMcpConfigService(agent, fakeWorkspaceResolver{record: workspace.Record{Path: project, Trust: "approve"}})
	if err := writeMcpConfig(filepath.Join(agent, "mcp.json"), map[string]any{"mcpServers": map[string]any{"docs": map[string]any{"command": "node"}, "old.name": map[string]any{"command": "node"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetMcpServer(domain.McpServerRequest{Scope: domain.McpConfigScopeGlobal, Name: "old.name"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpsertMcpServer(domain.UpsertMcpServerRequest{Scope: domain.McpConfigScopeGlobal, OriginalName: "old.name", Name: "old-name", Definition: `{"command":"node"}`}); err != nil {
		t.Fatal(err)
	}
	result, err := service.UpsertMcpServer(domain.UpsertMcpServerRequest{Scope: domain.McpConfigScopeProject, WorkspacePath: project, Name: "docs", Definition: `{"enabled":false,"exposure":"hidden"}`})
	if err != nil || !result.Disabled {
		t.Fatalf("native project override: %#v, %v", result, err)
	}
	if _, err := validMcpServerName("中文"); err == nil {
		t.Fatal("native Pi requires ASCII server names")
	}
}

func TestMcpNativeFieldValidation(t *testing.T) {
	for _, definition := range []string{
		`{"command":"node","type":"http"}`, `{"url":"https://example.test","type":"stdio"}`,
		`{"command":"node","exposure":"invalid"}`, `{"command":"node","exposure":null}`,
		`{"command":"node","toolExposure":{"read":"invalid"}}`, `{"command":"node","toolExposure":[]}`,
		`{"command":"node","timeout":0}`, `{"command":"node","timeout":-1}`, `{"command":"node","timeout":"60"}`,
		`{"command":"node","enabled":"false"}`, `{"command":"node","disabled":"true"}`,
		`{"command":"node","args":[1]}`, `{"command":"node","env":{"KEY":1}}`, `{"url":"https://example.test","headers":[]}`,
		`{"url":"file:///tmp/server"}`, `{"command":"node","description":false}`, `{"command":"node","cwd":1}`,
	} {
		t.Run(definition, func(t *testing.T) {
			if _, _, err := parseMcpDefinition(definition); err == nil {
				t.Fatal("accepted incompatible native definition")
			}
		})
	}
	for _, definition := range []string{
		`{"command":"node","type":"stdio","args":["server.js"],"env":{"KEY":"${TOKEN}"},"timeout":0.5,"exposure":"codemode-deferred"}`,
		`{"url":"https://example.test/mcp","type":"streamable-http","headers":{"Authorization":"Bearer ${TOKEN}"},"toolExposure":{"get_*":"direct"},"oauth":{"clientId":"test"}}`,
		`{"enabled":false,"exposure":"hidden"}`,
	} {
		if _, _, err := parseMcpDefinition(definition); err != nil {
			t.Fatalf("rejected native definition: %v", err)
		}
	}
}

func TestMcpNamespaceConflictsPreserveSavedConfig(t *testing.T) {
	root := t.TempDir()
	agent, project := filepath.Join(root, "agent"), filepath.Join(root, "project")
	service := newMcpConfigService(agent, fakeWorkspaceResolver{record: workspace.Record{Path: project, Trust: "approve"}})
	if _, err := service.UpsertMcpServer(domain.UpsertMcpServerRequest{Scope: domain.McpConfigScopeGlobal, Name: "docs-api", Definition: `{"command":"node"}`}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(agent, "mcp.json"))
	for _, scope := range []domain.McpConfigScope{domain.McpConfigScopeGlobal, domain.McpConfigScopeProject} {
		if _, err := service.UpsertMcpServer(domain.UpsertMcpServerRequest{Scope: scope, WorkspacePath: project, Name: "docs_api", Definition: `{"command":"node"}`}); err == nil || !strings.Contains(err.Error(), "conflicts") {
			t.Fatalf("accepted namespace conflict: %v", err)
		}
	}
	after, _ := os.ReadFile(filepath.Join(agent, "mcp.json"))
	if string(before) != string(after) {
		t.Fatal("failed save changed global config")
	}
	if _, err := os.Stat(filepath.Join(project, ".pi", "mcp.json")); !os.IsNotExist(err) {
		t.Fatal("failed save created project config")
	}
	if _, err := service.UpsertMcpServer(domain.UpsertMcpServerRequest{Scope: domain.McpConfigScopeProject, WorkspacePath: project, Name: "docs-api", Definition: `{"enabled":false}`}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpsertMcpServer(domain.UpsertMcpServerRequest{Scope: domain.McpConfigScopeProject, WorkspacePath: project, Name: "credentials", Definition: `{"url":"https://example.test","auth":{"provider":"openai"}}`}); err == nil {
		t.Fatal("accepted project auth")
	}
}

func TestMcpLegacyConfigIsMarkedAndMigratesAllEntriesOnSave(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "mcp.json")
	before := `{"autoEnableCodemode":false,"custom":{"keep":true},"mcp-servers":{"one":{"command":"node","disabled":false},"two":{"command":"node","disabled":true,"env":{"KEEP":"value"}}}}`
	if err := os.WriteFile(path, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	service := newMcpConfigService(directory, nil)
	snapshot, err := service.ListMcpServers(domain.ListMcpServersRequest{})
	if err != nil || len(snapshot.Servers) != 2 {
		t.Fatalf("legacy list: %#v %v", snapshot, err)
	}
	for _, server := range snapshot.Servers {
		if !server.NeedsMigration {
			t.Fatal("legacy server reported as native")
		}
	}
	unchanged, _ := os.ReadFile(path)
	if string(unchanged) != before {
		t.Fatal("listing changed the user's config")
	}
	server, err := service.GetMcpServer(domain.McpServerRequest{Scope: domain.McpConfigScopeGlobal, Name: "one"})
	if err != nil || !server.NeedsMigration {
		t.Fatalf("legacy editor: %#v %v", server, err)
	}
	if _, err := service.UpsertMcpServer(domain.UpsertMcpServerRequest{Scope: domain.McpConfigScopeGlobal, OriginalName: "one", Name: "one", Definition: server.Definition}); err != nil {
		t.Fatal(err)
	}
	raw, servers, err := readMcpConfig(path)
	if err != nil || raw["mcp-servers"] != nil || raw["custom"] == nil || raw["autoEnableCodemode"] != false {
		t.Fatalf("migration lost config: %#v %v", raw, err)
	}
	two := servers["two"].(map[string]any)
	if two["enabled"] != false || two["disabled"] != nil || two["env"].(map[string]any)["KEEP"] != "value" {
		t.Fatalf("migration changed untouched server: %#v", two)
	}
	snapshot, err = service.ListMcpServers(domain.ListMcpServersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range snapshot.Servers {
		if server.NeedsMigration {
			t.Fatal("native server still marked as legacy")
		}
	}
}

func TestNativeMcpProbeUsesPiWithoutChangingConfig(t *testing.T) {
	bin := os.Getenv("PI_DESK_TEST_NATIVE_PI")
	if bin == "" {
		t.Skip("set PI_DESK_TEST_NATIVE_PI for native Pi integration")
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	script := `import readline from 'node:readline';
for await (const line of readline.createInterface({input:process.stdin})) {
 const m = JSON.parse(line); if (m.id === undefined) continue;
 const result = m.method === 'initialize' ? {protocolVersion:'2025-06-18',capabilities:{tools:{}},serverInfo:{name:'fixture',version:'1'}} : m.method === 'tools/list' ? {tools:[{name:'search',description:'Search fixture',inputSchema:{type:'object'}}]} : {};
 process.stdout.write(JSON.stringify({jsonrpc:'2.0',id:m.id,result})+'\n');
}`
	server := filepath.Join(directory, "server.mjs")
	if err := os.WriteFile(server, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	definition, _ := json.Marshal(map[string]any{"command": node, "args": []string{server}, "enabled": false})
	config := filepath.Join(directory, "mcp.json")
	if err := writeMcpConfig(config, map[string]any{"mcpServers": map[string]any{"fixture": json.RawMessage(definition)}}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(config)
	result, err := newMcpConfigService(directory, nil).TestMcpServer(domain.TestMcpServerRequest{Name: "fixture", Definition: string(definition)})
	if err != nil || !strings.Contains(result.Output, "search") {
		t.Fatalf("native MCP probe: %#v, %v", result, err)
	}
	after, _ := os.ReadFile(config)
	if string(after) != string(before) {
		t.Fatal("connection test changed saved MCP config")
	}
}
