package appservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"pi-desk/internal/domain"
	"pi-desk/internal/workspace"

	"github.com/natefinch/atomic"
)

const (
	maxMcpConfigBytes = 4 << 20
	maxMcpServerName  = 120
	mcpTestTimeout    = 30 * time.Second
)

// McpConfigService edits Pi's global and trusted-workspace MCP configuration.
// Connection handling is delegated to the native Pi CLI.
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
	name := strings.TrimSpace(request.Name)
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
	if transportCount(definition) == 0 {
		if request.Scope != domain.McpConfigScopeProject {
			return domain.McpServer{}, errors.New("global MCP server needs command or url")
		}
		_, global, readErr := readMcpConfig(filepath.Join(service.agentDirectory, "mcp.json"))
		if readErr != nil {
			return domain.McpServer{}, readErr
		}
		if _, exists := global[name]; !exists {
			return domain.McpServer{}, errors.New("project MCP override needs a global server with the same name")
		}
	}
	raw, servers, err := readMcpConfig(path)
	if err != nil {
		return domain.McpServer{}, err
	}
	originalName := strings.TrimSpace(request.OriginalName)
	if originalName != "" {
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
	name := strings.TrimSpace(request.Name)
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

// TestMcpServer checks an unsaved definition using Pi's native MCP client.
func (service *McpConfigService) TestMcpServer(request domain.TestMcpServerRequest) (domain.McpServerTestResult, error) {
	definition, _, err := parseMcpDefinition(request.Definition)
	if err != nil {
		return domain.McpServerTestResult{}, err
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = "pi-desk-test"
	}
	if _, err := validMcpServerName(name); err != nil {
		return domain.McpServerTestResult{}, err
	}
	if transportCount(definition) == 0 {
		if request.Scope != domain.McpConfigScopeProject || request.WorkspacePath == "" {
			return domain.McpServerTestResult{}, errors.New("MCP override needs a trusted project")
		}
		_, global, readErr := readMcpConfig(filepath.Join(service.agentDirectory, "mcp.json"))
		if readErr != nil {
			return domain.McpServerTestResult{}, readErr
		}
		base, exists := global[name].(map[string]any)
		if !exists {
			return domain.McpServerTestResult{}, errors.New("MCP override has no global server")
		}
		for key, value := range definition {
			base[key] = value
		}
		definition = base
	}
	cwd := service.agentDirectory
	if request.WorkspacePath != "" {
		path, notice, enabled := service.projectDirectory(request.WorkspacePath)
		if !enabled && request.Scope == domain.McpConfigScopeProject {
			return domain.McpServerTestResult{}, errors.New(notice)
		}
		if enabled {
			cwd = filepath.Dir(filepath.Dir(path))
		}
	}
	if _, ok := definition["command"]; ok {
		serverCwd, _ := definition["cwd"].(string)
		if serverCwd == "" {
			definition["cwd"] = cwd
		} else if !filepath.IsAbs(serverCwd) && !strings.HasPrefix(serverCwd, "~") && !strings.Contains(serverCwd, "${") {
			definition["cwd"] = filepath.Join(cwd, serverCwd)
		}
	}
	definition["enabled"] = true
	directory, err := os.MkdirTemp("", "pi-desk-mcp-test-")
	if err != nil {
		return domain.McpServerTestResult{}, err
	}
	defer os.RemoveAll(directory)
	if err := writeMcpConfig(filepath.Join(directory, "mcp.json"), map[string]any{"mcpServers": map[string]any{name: definition}}); err != nil {
		return domain.McpServerTestResult{}, err
	}
	// Use existing OAuth credentials without modifying the user's credential file.
	auth, err := os.ReadFile(filepath.Join(service.agentDirectory, "mcp-auth.json"))
	if err == nil {
		if err := os.WriteFile(filepath.Join(directory, "mcp-auth.json"), auth, 0o600); err != nil {
			return domain.McpServerTestResult{}, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return domain.McpServerTestResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), mcpTestTimeout)
	defer cancel()
	started := time.Now()
	output, err := runPiProbe(ctx, directory, directory, "", "mcp", "list")
	if err != nil {
		return domain.McpServerTestResult{}, fmt.Errorf("MCP connection test failed: %w", err)
	}
	return domain.McpServerTestResult{Output: output, DurationMillis: time.Since(started).Milliseconds()}, nil
}

// ListImportableMcpServers scans other hosts' MCP configuration files (JSON
// only; Codex's TOML is deliberately out of scope) for importable servers.
func (service *McpConfigService) ListImportableMcpServers() ([]domain.McpImportCandidate, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	candidates := []domain.McpImportCandidate{}
	for _, source := range mcpImportSources() {
		for name, definition := range readMcpImportServers(source.path, source.rootKey) {
			entry, ok := definition.(map[string]any)
			if !ok || transportCount(entry) != 1 {
				continue
			}
			if _, err := validMcpServerName(name); err != nil {
				continue
			}
			formatted, err := formatMcpDefinition(entry)
			if err != nil {
				continue
			}
			candidates = append(candidates, domain.McpImportCandidate{Host: source.host, Path: source.path, Name: name, Definition: formatted})
		}
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].Host != candidates[right].Host {
			return candidates[left].Host < candidates[right].Host
		}
		return strings.ToLower(candidates[left].Name) < strings.ToLower(candidates[right].Name)
	})
	return candidates, nil
}

type mcpImportSource struct {
	host    string
	path    string
	rootKey string
}

func mcpImportSources() []mcpImportSource {
	sources := []mcpImportSource{}
	if home, err := os.UserHomeDir(); err == nil {
		sources = append(sources,
			mcpImportSource{host: "Claude Code", path: filepath.Join(home, ".claude.json"), rootKey: "mcpServers"},
			mcpImportSource{host: "Cursor", path: filepath.Join(home, ".cursor", "mcp.json"), rootKey: "mcpServers"},
		)
	}
	if configDir, err := os.UserConfigDir(); err == nil {
		sources = append(sources,
			mcpImportSource{host: "Claude Desktop", path: filepath.Join(configDir, "Claude", "claude_desktop_config.json"), rootKey: "mcpServers"},
			mcpImportSource{host: "VS Code", path: filepath.Join(configDir, "Code", "User", "mcp.json"), rootKey: "servers"},
		)
	}
	return sources
}

// readMcpImportServers is best-effort: missing files, size overruns, and
// malformed roots all simply yield no candidates. Cursor and VS Code configs
// are often JSONC, so comments are stripped as a fallback before giving up.
func readMcpImportServers(path string, rootKey string) map[string]any {
	content, err := os.ReadFile(path)
	if err != nil || len(content) > maxMcpConfigBytes {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	var root map[string]any
	if decoder.Decode(&root) != nil || root == nil {
		decoder = json.NewDecoder(bytes.NewReader(stripJSONComments(content)))
		decoder.UseNumber()
		if decoder.Decode(&root) != nil || root == nil {
			return nil
		}
	}
	servers, _ := root[rootKey].(map[string]any)
	return servers
}

// stripJSONComments removes // and /* */ comments outside of JSON strings.
func stripJSONComments(content []byte) []byte {
	out := make([]byte, 0, len(content))
	inString, escaped, inLine, inBlock := false, false, false, false
	for i := 0; i < len(content); i++ {
		character := content[i]
		if inLine {
			if character == '\n' {
				inLine = false
				out = append(out, character)
			}
			continue
		}
		if inBlock {
			if character == '*' && i+1 < len(content) && content[i+1] == '/' {
				inBlock = false
				i++
			}
			continue
		}
		if inString {
			out = append(out, character)
			switch {
			case escaped:
				escaped = false
			case character == '\\':
				escaped = true
			case character == '"':
				inString = false
			}
			continue
		}
		switch {
		case character == '"':
			inString = true
		case character == '/' && i+1 < len(content) && content[i+1] == '/':
			inLine = true
			i++
			continue
		case character == '/' && i+1 < len(content) && content[i+1] == '*':
			inBlock = true
			i++
			continue
		}
		out = append(out, character)
	}
	return out
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
	if record.Location.Kind == workspace.KindSSH {
		return "", "MCP configuration requires a local workspace", false
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
	disabled := entry["enabled"] == false || entry["disabled"] == true
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
	if transportCount(definition) > 1 || definition["socket"] != nil || definition["type"] == "sse" || definition["httpTransport"] == "sse" {
		return nil, "", errors.New("native Pi MCP needs exactly one of command or url; socket and SSE are unsupported")
	}
	if disabled, exists := definition["disabled"]; exists {
		if _, configured := definition["enabled"]; !configured {
			definition["enabled"] = disabled != true
		}
		delete(definition, "disabled")
	}
	if transportCount(definition) == 0 {
		for key := range definition {
			if key != "enabled" && key != "exposure" && key != "toolExposure" {
				return nil, "", errors.New("MCP server needs command or url; project overrides can only set enabled, exposure, and toolExposure")
			}
		}
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
		if character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return "", errors.New("MCP server name may contain only letters, numbers, hyphens, and underscores")
	}
	return name, nil
}
