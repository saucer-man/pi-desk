package appservice

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"pi-desk/internal/domain"
	"pi-desk/internal/workspace"

	"github.com/natefinch/atomic"
)

const (
	maxMcpConfigBytes = 4 << 20
	maxMcpServerName  = 120
)

// McpConfigService edits Pi's global mcp.json. Imported host and project
// configurations are deliberately outside Pi Desk's writable surface.
type McpConfigService struct {
	agentDirectory    string
	agentDirectoryErr error
	workspaces        promptWorkspaceResolver
	mu                sync.Mutex
}

func NewMcpConfigService(catalog *workspace.Catalog) *McpConfigService {
	directory, err := defaultPiAgentDirectory()
	return &McpConfigService{agentDirectory: directory, agentDirectoryErr: err, workspaces: catalog}
}

func newMcpConfigService(agentDirectory string, workspaces promptWorkspaceResolver) *McpConfigService {
	return &McpConfigService{agentDirectory: agentDirectory, workspaces: workspaces}
}

func (service *McpConfigService) ListMcpServers(request domain.ListMcpServersRequest) (domain.McpConfigSnapshot, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	globalPath, err := service.globalPath()
	if err != nil {
		return domain.McpConfigSnapshot{}, err
	}
	globalServers, err := listMcpServers(globalPath, domain.McpConfigScopeGlobal)
	if err != nil {
		return domain.McpConfigSnapshot{}, err
	}
	snapshot := domain.McpConfigSnapshot{GlobalPath: globalPath, Servers: globalServers}
	projectPath, notice, enabled := service.projectDirectory(request.WorkspacePath)
	snapshot.ProjectPath = projectPath
	snapshot.ProjectNotice = notice
	snapshot.ProjectEnabled = enabled
	if enabled {
		projectServers, err := listMcpServers(projectPath, domain.McpConfigScopeProject)
		if err != nil {
			return domain.McpConfigSnapshot{}, err
		}
		snapshot.Servers = append(snapshot.Servers, projectServers...)
	}
	sortMcpServers(snapshot.Servers)
	return snapshot, nil
}

func (service *McpConfigService) GetMcpServer(request domain.McpServerRequest) (domain.McpServer, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	path, err := service.pathFor(request.Scope, request.WorkspacePath)
	if err != nil {
		return domain.McpServer{}, err
	}
	name, err := validMcpServerName(request.Name)
	if err != nil {
		return domain.McpServer{}, err
	}
	_, servers, err := readMcpConfig(path)
	if err != nil {
		return domain.McpServer{}, err
	}
	definition, ok := servers[name]
	if !ok {
		return domain.McpServer{}, fmt.Errorf("MCP server %q was not found", name)
	}
	formatted, err := formatMcpDefinition(definition)
	if err != nil {
		return domain.McpServer{}, err
	}
	return domain.McpServer{McpServerSummary: summarizeMcpServer(request.Scope, name, definition), Definition: formatted}, nil
}

func (service *McpConfigService) UpsertMcpServer(request domain.UpsertMcpServerRequest) (domain.McpServer, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	path, err := service.pathFor(request.Scope, request.WorkspacePath)
	if err != nil {
		return domain.McpServer{}, err
	}
	name, err := validMcpServerName(request.Name)
	if err != nil {
		return domain.McpServer{}, err
	}
	definition, formatted, err := parseMcpDefinition(request.Definition)
	if err != nil {
		return domain.McpServer{}, err
	}
	raw, servers, err := readMcpConfig(path)
	if err != nil {
		return domain.McpServer{}, err
	}
	originalName := strings.TrimSpace(request.OriginalName)
	if originalName != "" {
		originalName, err = validMcpServerName(originalName)
		if err != nil {
			return domain.McpServer{}, err
		}
		if _, ok := servers[originalName]; !ok {
			return domain.McpServer{}, fmt.Errorf("MCP server %q was not found", originalName)
		}
		if originalName != name {
			if _, exists := servers[name]; exists {
				return domain.McpServer{}, fmt.Errorf("MCP server %q already exists", name)
			}
			delete(servers, originalName)
		}
	} else if _, exists := servers[name]; exists {
		return domain.McpServer{}, fmt.Errorf("MCP server %q already exists", name)
	}
	servers[name] = definition
	raw["mcpServers"] = servers
	delete(raw, "mcp-servers")
	if err := writeMcpConfig(path, raw); err != nil {
		return domain.McpServer{}, err
	}
	return domain.McpServer{McpServerSummary: summarizeMcpServer(request.Scope, name, definition), Definition: formatted}, nil
}

func (service *McpConfigService) DeleteMcpServer(request domain.McpServerRequest) error {
	service.mu.Lock()
	defer service.mu.Unlock()

	path, err := service.pathFor(request.Scope, request.WorkspacePath)
	if err != nil {
		return err
	}
	name, err := validMcpServerName(request.Name)
	if err != nil {
		return err
	}
	raw, servers, err := readMcpConfig(path)
	if err != nil {
		return err
	}
	if _, ok := servers[name]; !ok {
		return fmt.Errorf("MCP server %q was not found", name)
	}
	delete(servers, name)
	raw["mcpServers"] = servers
	delete(raw, "mcp-servers")
	return writeMcpConfig(path, raw)
}

func (service *McpConfigService) globalPath() (string, error) {
	if service.agentDirectoryErr != nil {
		return "", service.agentDirectoryErr
	}
	if strings.TrimSpace(service.agentDirectory) == "" {
		return "", errors.New("locate Pi agent directory")
	}
	return filepath.Join(filepath.Clean(service.agentDirectory), "mcp.json"), nil
}

func (service *McpConfigService) pathFor(scope domain.McpConfigScope, workspacePath string) (string, error) {
	if scope == domain.McpConfigScopeGlobal {
		return service.globalPath()
	}
	if scope != domain.McpConfigScopeProject {
		return "", errors.New("MCP scope must be global or project")
	}
	if service.workspaces == nil {
		return "", errors.New("Pi Desk manages only global MCP configuration")
	}
	path, notice, enabled := service.projectDirectory(workspacePath)
	if !enabled {
		if notice == "" {
			notice = "project MCP is unavailable"
		}
		return "", errors.New(notice)
	}
	return path, nil
}

func (service *McpConfigService) projectDirectory(workspacePath string) (string, string, bool) {
	if strings.TrimSpace(workspacePath) == "" {
		return "", "select a workspace to manage project MCP", false
	}
	if service.workspaces == nil {
		return "", "workspace catalog is unavailable", false
	}
	record, err := service.workspaces.ResolvePath(strings.TrimSpace(workspacePath))
	if err != nil {
		return "", err.Error(), false
	}
	if record.Trust != "approve" {
		return "", "approve this workspace before managing project MCP", false
	}
	return filepath.Join(record.Path, ".pi", "mcp.json"), "", true
}

func readMcpConfig(path string) (map[string]any, map[string]any, error) {
	raw := map[string]any{}
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return raw, map[string]any{}, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read MCP config: %w", err)
	}
	if len(content) > maxMcpConfigBytes {
		return nil, nil, fmt.Errorf("MCP config exceeds the %d MiB safety limit", maxMcpConfigBytes>>20)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return nil, nil, fmt.Errorf("parse MCP config: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, nil, fmt.Errorf("parse MCP config: %w", err)
	}
	if raw == nil {
		return nil, nil, errors.New("MCP config root must be an object")
	}
	value, ok := raw["mcpServers"]
	if !ok {
		value = raw["mcp-servers"]
	}
	if value == nil {
		return raw, map[string]any{}, nil
	}
	servers, ok := value.(map[string]any)
	if !ok {
		return nil, nil, errors.New("MCP config mcpServers must be an object")
	}
	return raw, servers, nil
}

func listMcpServers(path string, scope domain.McpConfigScope) ([]domain.McpServerSummary, error) {
	_, servers, err := readMcpConfig(path)
	if err != nil {
		return nil, err
	}
	result := make([]domain.McpServerSummary, 0, len(servers))
	for name, definition := range servers {
		if _, err := validMcpServerName(name); err != nil {
			continue
		}
		if _, ok := definition.(map[string]any); !ok {
			continue
		}
		result = append(result, summarizeMcpServer(scope, name, definition))
	}
	sortMcpServers(result)
	return result, nil
}

func summarizeMcpServer(scope domain.McpConfigScope, name string, definition any) domain.McpServerSummary {
	entry, _ := definition.(map[string]any)
	transport := "custom"
	endpoint := ""
	if value, ok := entry["command"].(string); ok && strings.TrimSpace(value) != "" {
		transport, endpoint = "stdio", value
	} else if value, ok := entry["url"].(string); ok && strings.TrimSpace(value) != "" {
		transport, endpoint = "http", value
	} else if value, ok := entry["socket"].(string); ok && strings.TrimSpace(value) != "" {
		transport, endpoint = "socket", value
	}
	disabled, _ := entry["disabled"].(bool)
	return domain.McpServerSummary{Scope: scope, Name: name, Transport: transport, Endpoint: endpoint, Disabled: disabled}
}

func sortMcpServers(servers []domain.McpServerSummary) {
	sort.Slice(servers, func(left, right int) bool {
		if servers[left].Scope != servers[right].Scope {
			return servers[left].Scope < servers[right].Scope
		}
		return strings.ToLower(servers[left].Name) < strings.ToLower(servers[right].Name)
	})
}

func parseMcpDefinition(content string) (map[string]any, string, error) {
	if len(content) > maxMcpConfigBytes {
		return nil, "", fmt.Errorf("MCP server definition exceeds the %d MiB safety limit", maxMcpConfigBytes>>20)
	}
	if !utf8.ValidString(content) {
		return nil, "", errors.New("MCP server definition must be valid UTF-8")
	}
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.UseNumber()
	definition := map[string]any{}
	if err := decoder.Decode(&definition); err != nil {
		return nil, "", fmt.Errorf("parse MCP server definition: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, "", fmt.Errorf("parse MCP server definition: %w", err)
	}
	if len(definition) == 0 {
		return nil, "", errors.New("MCP server definition cannot be empty")
	}
	if transportCount(definition) != 1 {
		return nil, "", errors.New("MCP server definition needs exactly one of command, url, or socket")
	}
	formatted, err := formatMcpDefinition(definition)
	return definition, formatted, err
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON content")
		}
		return err
	}
	return nil
}

func transportCount(definition map[string]any) int {
	count := 0
	for _, key := range []string{"command", "url", "socket"} {
		if value, ok := definition[key].(string); ok && strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count
}

func formatMcpDefinition(definition any) (string, error) {
	content, err := json.MarshalIndent(definition, "", "  ")
	if err != nil {
		return "", fmt.Errorf("format MCP server definition: %w", err)
	}
	return string(content) + "\n", nil
}

func writeMcpConfig(path string, raw map[string]any) error {
	content, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("format MCP config: %w", err)
	}
	content = append(content, '\n')
	if len(content) > maxMcpConfigBytes {
		return fmt.Errorf("MCP config exceeds the %d MiB safety limit", maxMcpConfigBytes>>20)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create MCP config directory: %w", err)
	}
	if err := atomic.WriteFile(path, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("write MCP config: %w", err)
	}
	return nil
}

func validMcpServerName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || utf8.RuneCountInString(name) > maxMcpServerName {
		return "", fmt.Errorf("MCP server name must contain 1 to %d characters", maxMcpServerName)
	}
	for _, character := range name {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '-' || character == '_' || character == '.' {
			continue
		}
		return "", errors.New("MCP server name may contain only letters, numbers, dots, hyphens, and underscores")
	}
	return name, nil
}
