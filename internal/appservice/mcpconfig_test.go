package appservice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pi-desk/internal/domain"
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
