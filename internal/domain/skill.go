package domain

type SkillScope string

const (
	SkillScopeGlobal  SkillScope = "global"
	SkillScopeProject SkillScope = "project"
)

type ManagedSkillSummary struct {
	Scope         SkillScope `json:"scope"`
	Name          string     `json:"name"`
	Description   string     `json:"description,omitempty"`
	RootDirectory string     `json:"rootDirectory"`
	RelativePath  string     `json:"relativePath"`
	Path          string     `json:"path"`
	Directory     string     `json:"directory"`
	Kind          string     `json:"kind"`
	Enabled       bool       `json:"enabled"`
	Warnings      []string   `json:"warnings,omitempty"`
}

type ManagedSkill struct {
	ManagedSkillSummary
	Content string `json:"content"`
}

// ManagedSkillSnapshot reads the directories Pi natively scans. It deliberately
// does not register a separate skill catalog.
type ManagedSkillSnapshot struct {
	GlobalDirectory   string                `json:"globalDirectory"`
	GlobalDirectories []string              `json:"globalDirectories"`
	ProjectDirectory  string                `json:"projectDirectory,omitempty"`
	ProjectEnabled    bool                  `json:"projectEnabled"`
	ProjectNotice     string                `json:"projectNotice,omitempty"`
	Skills            []ManagedSkillSummary `json:"skills"`
}

type ListManagedSkillsRequest struct {
	WorkspacePath string `json:"workspacePath,omitempty"`
}

type ManagedSkillRequest struct {
	Scope         SkillScope `json:"scope"`
	WorkspacePath string     `json:"workspacePath,omitempty"`
	RootDirectory string     `json:"rootDirectory,omitempty"`
	RelativePath  string     `json:"relativePath"`
}

type CreateManagedSkillRequest struct {
	Scope         SkillScope `json:"scope"`
	WorkspacePath string     `json:"workspacePath,omitempty"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
}

type UpdateManagedSkillRequest struct {
	Scope         SkillScope `json:"scope"`
	WorkspacePath string     `json:"workspacePath,omitempty"`
	RootDirectory string     `json:"rootDirectory,omitempty"`
	RelativePath  string     `json:"relativePath"`
	Content       string     `json:"content"`
}
