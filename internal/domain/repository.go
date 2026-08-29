package domain

type RepositoryRequest struct {
	WorkspaceID   string `json:"workspaceId,omitempty"`
	WorkspacePath string `json:"workspacePath,omitempty"`
}

type RepositoryFileRequest struct {
	WorkspaceID   string `json:"workspaceId,omitempty"`
	WorkspacePath string `json:"workspacePath,omitempty"`
	Path          string `json:"path"`
}

type RepositorySaveFileRequest struct {
	WorkspaceID   string `json:"workspaceId,omitempty"`
	WorkspacePath string `json:"workspacePath,omitempty"`
	Path          string `json:"path"`
	OutputPath    string `json:"outputPath"`
}

type RepositoryFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type GitChangedFile struct {
	Path           string `json:"path"`
	OriginalPath   string `json:"originalPath,omitempty"`
	IndexStatus    string `json:"indexStatus"`
	WorktreeStatus string `json:"worktreeStatus"`
}

type GitStatus struct {
	IsRepository bool             `json:"isRepository"`
	Branch       string           `json:"branch,omitempty"`
	Detached     bool             `json:"detached,omitempty"`
	Ahead        int              `json:"ahead,omitempty"`
	Behind       int              `json:"behind,omitempty"`
	Files        []GitChangedFile `json:"files"`
}

type RepositorySnapshot struct {
	Files     []RepositoryFile `json:"files"`
	Truncated bool             `json:"truncated,omitempty"`
	Git       GitStatus        `json:"git"`
}

type RepositoryFileDiff struct {
	Path      string `json:"path"`
	Staged    string `json:"staged,omitempty"`
	Working   string `json:"working,omitempty"`
	Content   string `json:"content,omitempty"`
	Binary    bool   `json:"binary,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

type RepositoryFilePreview struct {
	Path         string `json:"path"`
	AbsolutePath string `json:"absolutePath"`
	Content      string `json:"content,omitempty"`
	MediaType    string `json:"mediaType,omitempty"`
	DataURL      string `json:"dataUrl,omitempty"`
	Size         int64  `json:"size"`
	Binary       bool   `json:"binary,omitempty"`
	Truncated    bool   `json:"truncated,omitempty"`
}

type GitBranch struct {
	Name         string `json:"name"`
	FullName     string `json:"fullName"`
	Remote       bool   `json:"remote,omitempty"`
	Current      bool   `json:"current,omitempty"`
	Upstream     string `json:"upstream,omitempty"`
	Commit       string `json:"commit"`
	WorktreePath string `json:"worktreePath,omitempty"`
}

type GitBranchInventory struct {
	Branches []GitBranch `json:"branches"`
}
