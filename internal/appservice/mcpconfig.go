package appservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"pi-desk/internal/domain"
	"pi-desk/internal/processutil"
	"pi-desk/internal/workspace"

	"github.com/natefinch/atomic"
)

const (
	maxMcpConfigBytes = 4 << 20
	maxMcpServerName  = 120
	// pi-mcp-adapter is Pi's de facto MCP connection engine; Pi Desk edits its
	// configuration and manages the package installation from the frontend.
	mcpAdapterPackageFragment = "pi-mcp-adapter"
	mcpTestTimeout            = 20 * time.Second
)

const mcpTestClientScript = `
import { homedir } from "node:os";
import { isAbsolute, join, resolve } from "node:path";
import { createConnection } from "node:net";
import { pathToFileURL } from "node:url";

let source = "";
for await (const chunk of process.stdin) source += chunk;
const definition = JSON.parse(source);
const sdkRoot = process.argv[1];
const sdk = await import(pathToFileURL(join(sdkRoot, "dist", "index.mjs")).href);
const stdio = await import(pathToFileURL(join(sdkRoot, "dist", "stdio.mjs")).href);
const interpolate = value => String(value).replace(/\$\{([^}]+)\}/g, (_, name) => process.env[name] ?? "");
const expandPath = value => value === "~" ? homedir() : value.startsWith("~/") || value.startsWith("~\\") ? join(homedir(), value.slice(2)) : isAbsolute(value) ? value : resolve(value);
const names = values => values.map(value => String(value).slice(0, 240)).sort().slice(0, 200);
const toolDetails = tools => tools.map(item => ({
  name: String(item.name).slice(0, 240),
  description: String(item.description ?? "").slice(0, 2000),
  inputSchema: item.inputSchema ? JSON.stringify(item.inputSchema, null, 2).slice(0, 12000) : "",
})).sort((left, right) => left.name.localeCompare(right.name)).slice(0, 200);
let client;
let transport;
let stderrTail = "";

class SocketTransport {
  socket;
  buffer = new sdk.ReadBuffer();
  constructor(path) { this.path = path; }
  async start() {
    await new Promise((accept, reject) => {
      const socket = createConnection(this.path);
      this.socket = socket;
      let connected = false;
      socket.once("connect", () => { connected = true; accept(); });
      socket.on("data", chunk => {
        try {
          this.buffer.append(chunk);
          for (let message; (message = this.buffer.readMessage()) !== null;) this.onmessage?.(message);
        } catch (error) { this.onerror?.(error instanceof Error ? error : new Error(String(error))); }
      });
      socket.on("error", error => { if (!connected) reject(error); this.onerror?.(error); });
      socket.on("close", () => { this.buffer.clear(); this.onclose?.(); });
    });
  }
  async send(message) {
    if (!this.socket || this.socket.destroyed) throw new Error("MCP socket is not connected");
    await new Promise((accept, reject) => this.socket.write(sdk.serializeMessage(message), error => error ? reject(error) : accept()));
  }
  async close() { this.buffer.clear(); this.socket?.destroy(); }
}

const makeClient = () => new sdk.Client({ name: "pi-desk-mcp-debugger", version: "1.0.0" });
const requestOptions = { timeout: 15_000 };
const connectHttp = async () => {
  const headers = {};
  for (const [name, value] of Object.entries(definition.headers ?? {})) headers[name] = interpolate(value);
  if (definition.auth === "bearer") {
    const token = definition.bearerTokenEnv ? process.env[definition.bearerTokenEnv] : definition.bearerToken;
    if (token) headers.Authorization = "Bearer " + interpolate(token);
  }
  const options = Object.keys(headers).length ? { requestInit: { headers } } : {};
  const url = new URL(interpolate(definition.url));
  const choices = definition.httpTransport === "sse"
    ? [sdk.SSEClientTransport]
    : definition.httpTransport === "streamable-http"
      ? [sdk.StreamableHTTPClientTransport]
      : [sdk.StreamableHTTPClientTransport, sdk.SSEClientTransport];
  const errors = [];
  for (const Transport of choices) {
    const nextClient = makeClient();
    const nextTransport = new Transport(url, options);
    try {
      await nextClient.connect(nextTransport, requestOptions);
      return [nextClient, nextTransport];
    } catch (error) {
      errors.push(error instanceof Error ? error.message : String(error));
      await nextClient.close().catch(() => {});
    }
  }
  throw new Error(errors.join("; "));
};

try {
  if (definition.command) {
    const env = { ...process.env };
    for (const [name, value] of Object.entries(definition.env ?? {})) env[name] = interpolate(value);
    transport = new stdio.StdioClientTransport({
      command: definition.command,
      args: (definition.args ?? []).map(interpolate),
      env,
      cwd: definition.cwd ? expandPath(interpolate(definition.cwd)) : process.cwd(),
      stderr: "pipe",
    });
    transport.stderr?.on("data", chunk => { stderrTail = (stderrTail + String(chunk)).slice(-4000); });
    client = makeClient();
    await client.connect(transport, requestOptions);
  } else if (definition.url) {
    [client, transport] = await connectHttp();
  } else {
    transport = new SocketTransport(expandPath(interpolate(definition.socket)));
    client = makeClient();
    await client.connect(transport, requestOptions);
  }

  const capabilities = client.getServerCapabilities() ?? {};
  const [toolResult, resourceResult, promptResult] = await Promise.all([
    capabilities.tools ? client.listTools(undefined, requestOptions) : { tools: [] },
    capabilities.resources ? client.listResources(undefined, requestOptions) : { resources: [] },
    capabilities.prompts ? client.listPrompts(undefined, requestOptions) : { prompts: [] },
  ]);
  const server = client.getServerVersion() ?? {};
  const tools = toolResult.tools ?? [];
  const resources = resourceResult.resources ?? [];
  const prompts = promptResult.prompts ?? [];
  process.stdout.write(JSON.stringify({
    transport: definition.command ? "stdio" : definition.url ? "http" : "socket",
    protocolVersion: client.getNegotiatedProtocolVersion?.() ?? "",
    serverName: server.name ?? "",
    serverVersion: server.version ?? "",
    capabilities: Object.keys(capabilities).sort(),
    tools: toolDetails(tools),
    resources: names(resources.map(item => item.name || item.uri)),
    prompts: names(prompts.map(item => item.name)),
    toolCount: tools.length,
    resourceCount: resources.length,
    promptCount: prompts.length,
  }));
} catch (error) {
  const message = error instanceof Error ? error.message : String(error);
  process.stderr.write(message + (stderrTail.trim() ? "\n\nServer stderr:\n" + stderrTail.trim() : ""));
  process.exitCode = 1;
} finally {
  await client?.close().catch(() => {});
}
`

const mcpConfigSnapshotScript = `
import { pathToFileURL } from "node:url";
const adapter = await import(pathToFileURL(process.argv[1]).href);
const overridePath = process.argv[2];
const cwd = process.argv[3];
const config = adapter.loadMcpConfig(overridePath, cwd);
const discovery = adapter.getMcpDiscoverySummary(overridePath, cwd);
const provenance = typeof adapter.getServerProvenance === "function"
  ? Object.fromEntries(adapter.getServerProvenance(overridePath, cwd))
  : {};
const sources = [
  ...discovery.sources,
  ...(discovery.imports || [])
    .filter(source => discovery.hostConfigDiscovery === "on" || (config.imports || []).includes(source.kind))
    .map(source => ({ ...source, id: "host-" + source.kind, label: source.kind, exists: true, scope: "global", kind: "host" })),
  ...discovery.agentPlugins.map((plugin, index) => ({
    id: "agent-plugin-" + index,
    label: plugin.name || "Agent Plugin",
    path: plugin.path,
    exists: true,
    scope: "global",
    kind: "plugin",
    serverCount: plugin.serverCount,
  })),
  ...(config.claudePlugins || []).filter(plugin => plugin.mcp).map((plugin, index) => ({
    id: "claude-plugin-" + index,
    label: "Claude Plugin",
    path: plugin.path,
    exists: true,
    scope: "project",
    kind: "plugin",
    serverCount: 0,
  })),
];
process.stdout.write(JSON.stringify({ servers: config.mcpServers || {}, provenance, sources }));
`

// McpConfigService edits Pi's global and trusted-workspace MCP configuration.
// Imported host configurations remain outside Pi Desk's writable surface.
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
	projectRoot := ""
	if enabled {
		projectRoot = filepath.Dir(filepath.Dir(projectPath))
	}
	effective, sources, err := service.loadAdapterMcpConfig(globalPath, projectRoot)
	if err != nil {
		snapshot.AdapterNotice = err.Error()
	} else {
		snapshot.EffectiveServers = effective
		snapshot.Sources = sources
	}
	return snapshot, nil
}

type adapterMcpSnapshot struct {
	Servers    map[string]any                     `json:"servers"`
	Provenance map[string]adapterServerProvenance `json:"provenance"`
	Sources    []domain.McpConfigSource           `json:"sources"`
}

type adapterServerProvenance struct {
	Kind string `json:"kind"`
}

func (service *McpConfigService) loadAdapterMcpConfig(globalPath string, projectRoot string) ([]domain.McpEffectiveServer, []domain.McpConfigSource, error) {
	modulePath := filepath.Join(service.agentDirectory, "npm", "node_modules", "pi-mcp-adapter", "dist", "config.js")
	if info, err := os.Stat(modulePath); err != nil || !info.Mode().IsRegular() {
		return nil, nil, nil
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return nil, nil, errors.New("Node.js is required to read pi-mcp-adapter configuration")
	}
	cwd := projectRoot
	if cwd == "" {
		cwd = service.agentDirectory
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, "--input-type=module", "--eval", mcpConfigSnapshotScript, modulePath, globalPath, cwd)
	processutil.ConfigureBackground(command)
	command.Dir = cwd
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, nil, errors.New("reading pi-mcp-adapter configuration timed out")
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, nil, fmt.Errorf("read pi-mcp-adapter configuration: %s", message)
	}
	if stdout.Len() > maxMcpConfigBytes {
		return nil, nil, fmt.Errorf("pi-mcp-adapter configuration exceeds the %d MiB safety limit", maxMcpConfigBytes>>20)
	}
	loaded := adapterMcpSnapshot{}
	if err := json.Unmarshal(stdout.Bytes(), &loaded); err != nil {
		return nil, nil, fmt.Errorf("parse pi-mcp-adapter configuration: %w", err)
	}
	effective := make([]domain.McpEffectiveServer, 0, len(loaded.Servers))
	for name, definition := range loaded.Servers {
		if _, err := validMcpServerName(name); err != nil {
			continue
		}
		if _, ok := definition.(map[string]any); !ok {
			continue
		}
		formatted, err := formatMcpDefinition(definition)
		if err != nil {
			continue
		}
		scope := domain.McpConfigScopeGlobal
		if loaded.Provenance[name].Kind == "project" {
			scope = domain.McpConfigScopeProject
		}
		effective = append(effective, domain.McpEffectiveServer{McpServerSummary: summarizeMcpServer(scope, name, definition), Definition: formatted})
	}
	sort.Slice(effective, func(left, right int) bool {
		return strings.ToLower(effective[left].Name) < strings.ToLower(effective[right].Name)
	})
	if projectRoot == "" {
		globalSources := loaded.Sources[:0]
		for _, source := range loaded.Sources {
			if source.Scope != domain.McpConfigScopeProject {
				globalSources = append(globalSources, source)
			}
		}
		loaded.Sources = globalSources
	}
	return effective, loaded.Sources, nil
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

// TestMcpServer starts the current editor definition without saving it, then
// asks the same MCP client library used by pi-mcp-adapter for server metadata.
func (service *McpConfigService) TestMcpServer(request domain.TestMcpServerRequest) (domain.McpServerTestResult, error) {
	_, definition, err := parseMcpDefinition(request.Definition)
	if err != nil {
		return domain.McpServerTestResult{}, err
	}
	clientPackage, err := service.mcpClientPackageDirectory()
	if err != nil {
		return domain.McpServerTestResult{}, err
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return domain.McpServerTestResult{}, errors.New("Node.js is required to test MCP servers")
	}

	ctx, cancel := context.WithTimeout(context.Background(), mcpTestTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, node, "--input-type=module", "--eval", mcpTestClientScript, clientPackage)
	processutil.ConfigureBackground(command)
	command.Stdin = strings.NewReader(definition)
	if root := service.workspaceRoot(request.WorkspacePath); root != "" {
		command.Dir = root
	}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	started := time.Now()
	if err := command.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return domain.McpServerTestResult{}, fmt.Errorf("MCP connection test timed out after %s", mcpTestTimeout)
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return domain.McpServerTestResult{}, fmt.Errorf("MCP connection test failed: %s", message)
	}

	result := domain.McpServerTestResult{}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return domain.McpServerTestResult{}, fmt.Errorf("read MCP test result: %w", err)
	}
	result.DurationMillis = time.Since(started).Milliseconds()
	return result, nil
}

func (service *McpConfigService) mcpClientPackageDirectory() (string, error) {
	if service.agentDirectoryErr != nil {
		return "", service.agentDirectoryErr
	}
	for _, candidate := range []string{
		filepath.Join(service.agentDirectory, "npm", "node_modules", "@modelcontextprotocol", "client"),
		filepath.Join(service.agentDirectory, "npm", "node_modules", "pi-mcp-adapter", "node_modules", "@modelcontextprotocol", "client"),
	} {
		if info, err := os.Stat(filepath.Join(candidate, "dist", "index.mjs")); err == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
	}
	return "", errors.New("pi-mcp-adapter is not installed or is incomplete; install or update the MCP connection engine first")
}

// GetMcpEngineStatus reports whether pi-mcp-adapter is installed as a global
// Pi package and which shared config files pi-mcp-adapter also reads alongside
// the Pi-owned global and project override files this service edits.
func (service *McpConfigService) GetMcpEngineStatus(request domain.McpEngineStatusRequest) (domain.McpEngineStatus, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	status := domain.McpEngineStatus{Enabled: true}
	if service.agentDirectoryErr != nil || strings.TrimSpace(service.agentDirectory) == "" {
		return status, nil
	}
	packages, err := listPiPackages(filepath.Join(filepath.Clean(service.agentDirectory), "settings.json"), domain.PiPackageScopeGlobal)
	if err != nil {
		return domain.McpEngineStatus{}, err
	}
	for _, pkg := range packages {
		if strings.Contains(strings.ToLower(pkg.Source), mcpAdapterPackageFragment) {
			status.Source, status.Installed, status.Enabled = pkg.Source, true, pkg.Enabled
			break
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		status.ShadowedPaths = appendExistingFiles(status.ShadowedPaths,
			filepath.Join(home, ".config", "mcp", "mcp.json"),
			filepath.Join(home, ".agents", "mcp.json"),
			filepath.Join(home, ".agents", "mcp", "mcp.json"),
		)
	}
	if root := service.workspaceRoot(request.WorkspacePath); root != "" {
		status.ShadowedPaths = appendExistingFiles(status.ShadowedPaths,
			filepath.Join(root, ".mcp.json"),
		)
	}
	return status, nil
}

func (service *McpConfigService) workspaceRoot(workspacePath string) string {
	if strings.TrimSpace(workspacePath) == "" || service.workspaces == nil {
		return ""
	}
	record, err := service.workspaces.ResolvePath(strings.TrimSpace(workspacePath))
	if err != nil || record.Location.Kind != workspace.KindLocal {
		return ""
	}
	return record.Path
}

func appendExistingFiles(paths []string, candidates ...string) []string {
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			paths = append(paths, candidate)
		}
	}
	return paths
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
