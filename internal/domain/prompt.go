package domain

type PromptTemplateScope string

const (
	PromptTemplateScopeGlobal  PromptTemplateScope = "global"
	PromptTemplateScopeProject PromptTemplateScope = "project"
)

type PromptTemplateSummary struct {
	Scope        PromptTemplateScope `json:"scope"`
	Name         string              `json:"name"`
	Description  string              `json:"description,omitempty"`
	ArgumentHint string              `json:"argumentHint,omitempty"`
	Path         string              `json:"path"`
}

type PromptTemplate struct {
	PromptTemplateSummary
	Content string `json:"content"`
}

// PromptTemplateSnapshot maps directly to Pi's native prompt template directories.
// Project templates are available only when the selected workspace is trusted.
type PromptTemplateSnapshot struct {
	GlobalDirectory  string                  `json:"globalDirectory"`
	ProjectDirectory string                  `json:"projectDirectory,omitempty"`
	ProjectEnabled   bool                    `json:"projectEnabled"`
	ProjectNotice    string                  `json:"projectNotice,omitempty"`
	Templates        []PromptTemplateSummary `json:"templates"`
}

type ListPromptTemplatesRequest struct {
	WorkspacePath string `json:"workspacePath,omitempty"`
}

type PromptTemplateRequest struct {
	Scope         PromptTemplateScope `json:"scope"`
	WorkspacePath string              `json:"workspacePath,omitempty"`
	Name          string              `json:"name"`
}

type UpsertPromptTemplateRequest struct {
	Scope         PromptTemplateScope `json:"scope"`
	WorkspacePath string              `json:"workspacePath,omitempty"`
	OriginalName  string              `json:"originalName,omitempty"`
	Name          string              `json:"name"`
	Content       string              `json:"content"`
}
