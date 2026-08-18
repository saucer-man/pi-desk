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

// McpConfigSnapshot exposes only Pi-owned configuration layers. Imported host
// configurations are deliberately not writable from Pi Desk.
type McpConfigSnapshot struct {
	GlobalPath     string             `json:"globalPath"`
	ProjectPath    string             `json:"projectPath,omitempty"`
	ProjectEnabled bool               `json:"projectEnabled"`
	ProjectNotice  string             `json:"projectNotice,omitempty"`
	Servers        []McpServerSummary `json:"servers"`
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

type UpsertMcpServerRequest struct {
	Scope         McpConfigScope `json:"scope"`
	WorkspacePath string         `json:"workspacePath,omitempty"`
	OriginalName  string         `json:"originalName,omitempty"`
	Name          string         `json:"name"`
	Definition    string         `json:"definition"`
}
