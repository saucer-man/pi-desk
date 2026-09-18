package domain

type McpConfigScope string

const (
	McpConfigScopeGlobal  McpConfigScope = "global"
	McpConfigScopeProject McpConfigScope = "project"
)

type McpServerSummary struct {
	Scope     McpConfigScope `json:"scope"`
	Name      string         `json:"name"`
	Transport string         `json:"transport"`
	Endpoint  string         `json:"endpoint,omitempty"`
	Disabled  bool           `json:"disabled"`
}

// McpConfigSnapshot exposes Pi-owned writable layers plus the effective,
// read-only configuration resolved by pi-mcp-adapter.
type McpConfigSnapshot struct {
	GlobalPath       string               `json:"globalPath"`
	ProjectPath      string               `json:"projectPath,omitempty"`
	ProjectEnabled   bool                 `json:"projectEnabled"`
	ProjectNotice    string               `json:"projectNotice,omitempty"`
	AdapterNotice    string               `json:"adapterNotice,omitempty"`
	Servers          []McpServerSummary   `json:"servers"`
	EffectiveServers []McpEffectiveServer `json:"effectiveServers"`
	Sources          []McpConfigSource    `json:"sources"`
}

type ListMcpServersRequest struct {
	WorkspacePath string `json:"workspacePath,omitempty"`
}

type McpServerRequest struct {
	Scope         McpConfigScope `json:"scope"`
	WorkspacePath string         `json:"workspacePath,omitempty"`
	Name          string         `json:"name"`
}

type McpServer struct {
	McpServerSummary
	Definition string `json:"definition"`
}

type McpEffectiveServer struct {
	McpServerSummary
	Definition string `json:"definition"`
}

type McpConfigSource struct {
	ID          string         `json:"id"`
	Label       string         `json:"label"`
	Path        string         `json:"path"`
	Scope       McpConfigScope `json:"scope"`
	Kind        string         `json:"kind"`
	Exists      bool           `json:"exists"`
	ServerCount int            `json:"serverCount"`
}

type UpsertMcpServerRequest struct {
	Scope         McpConfigScope `json:"scope"`
	WorkspacePath string         `json:"workspacePath,omitempty"`
	OriginalName  string         `json:"originalName,omitempty"`
	Name          string         `json:"name"`
	Definition    string         `json:"definition"`
}

type TestMcpServerRequest struct {
	WorkspacePath string `json:"workspacePath,omitempty"`
	Definition    string `json:"definition"`
}

type McpToolDetail struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema string `json:"inputSchema,omitempty"`
}

type McpServerTestResult struct {
	Transport       string          `json:"transport"`
	ProtocolVersion string          `json:"protocolVersion,omitempty"`
	ServerName      string          `json:"serverName,omitempty"`
	ServerVersion   string          `json:"serverVersion,omitempty"`
	Capabilities    []string        `json:"capabilities"`
	Tools           []McpToolDetail `json:"tools"`
	Resources       []string        `json:"resources"`
	Prompts         []string        `json:"prompts"`
	ToolCount       int             `json:"toolCount"`
	ResourceCount   int             `json:"resourceCount"`
	PromptCount     int             `json:"promptCount"`
	DurationMillis  int64           `json:"durationMillis"`
}

// McpEngineStatus describes the pi-mcp-adapter package that connects Pi to MCP
// servers, plus shared config files the adapter also reads.
type McpEngineStatusRequest struct {
	WorkspacePath string `json:"workspacePath,omitempty"`
}

type McpEngineStatus struct {
	Source        string   `json:"source,omitempty"`
	Installed     bool     `json:"installed"`
	Enabled       bool     `json:"enabled"`
	ShadowedPaths []string `json:"shadowedPaths,omitempty"`
}

// McpImportCandidate is a server found in another host's configuration file
// (Claude Code, Claude Desktop, Cursor, VS Code). Host carries a display name.
type McpImportCandidate struct {
	Host       string `json:"host"`
	Path       string `json:"path"`
	Name       string `json:"name"`
	Definition string `json:"definition"`
}
