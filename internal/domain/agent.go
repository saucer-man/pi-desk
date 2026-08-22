package domain

type StartSessionRequest struct {
	ThreadID       string `json:"threadId"`
	Workspace      string `json:"workspace"`
	WorkspaceID    string `json:"workspaceId,omitempty"`
	SessionPath    string `json:"sessionPath,omitempty"`
	SessionName    string `json:"sessionName,omitempty"`
	Trust          string `json:"trust"`
	NoSession      bool   `json:"noSession,omitempty"`
	Offline        bool   `json:"offline,omitempty"`
	DisableThemes  bool   `json:"disableThemes,omitempty"`
	DisableSkills  bool   `json:"disableSkills,omitempty"`
	DisablePlugins bool   `json:"disablePlugins,omitempty"`
	ProxyURL       string `json:"proxyUrl,omitempty"`
}

type LiveSession struct {
	ThreadID   string `json:"threadId"`
	Generation uint64 `json:"generation"`
	StateJSON  string `json:"stateJson"`
}

type CommandResult struct {
	Command  string `json:"command"`
	DataJSON string `json:"dataJson,omitempty"`
}

type SessionBranchEntry struct {
	ID        string `json:"id"`
	ParentID  string `json:"parentId,omitempty"`
	Type      string `json:"type"`
	Timestamp string `json:"timestamp,omitempty"`
	Role      string `json:"role,omitempty"`
	Text      string `json:"text,omitempty"`
	Label     string `json:"label,omitempty"`
}

type SessionBranches struct {
	Entries []SessionBranchEntry `json:"entries"`
	LeafID  string               `json:"leafId,omitempty"`
}

type PromptRequest struct {
	ThreadID          string         `json:"threadId"`
	Message           string         `json:"message"`
	StreamingBehavior string         `json:"streamingBehavior,omitempty"`
	Images            []ImageContent `json:"images,omitempty"`
}

type ImageContent struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MIMEType string `json:"mimeType"`
}

type ThreadRequest struct {
	ThreadID string `json:"threadId"`
}

type ToggleRequest struct {
	ThreadID string `json:"threadId"`
	Enabled  bool   `json:"enabled"`
}

type QueueModeRequest struct {
	ThreadID string `json:"threadId"`
	Mode     string `json:"mode"`
}

type BashRequest struct {
	ThreadID           string `json:"threadId"`
	Command            string `json:"command"`
	ExcludeFromContext bool   `json:"excludeFromContext,omitempty"`
}

type ModelRequest struct {
	ThreadID string `json:"threadId"`
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

type ThinkingRequest struct {
	ThreadID string `json:"threadId"`
	Level    string `json:"level"`
}

type CompactRequest struct {
	ThreadID           string `json:"threadId"`
	CustomInstructions string `json:"customInstructions,omitempty"`
}

type SessionNameRequest struct {
	ThreadID string `json:"threadId"`
	Name     string `json:"name"`
}

type SessionForkRequest struct {
	ThreadID string `json:"threadId"`
	EntryID  string `json:"entryId"`
}

type SessionMessageRequest struct {
	ThreadID string `json:"threadId"`
	Path     string `json:"path"`
	EntryID  string `json:"entryId"`
	Text     string `json:"text,omitempty"`
	Before   bool   `json:"before,omitempty"`
}

type ExportSessionRequest struct {
	ThreadID   string `json:"threadId"`
	OutputPath string `json:"outputPath,omitempty"`
}

type ExtensionUIResponseRequest struct {
	ThreadID  string `json:"threadId"`
	RequestID string `json:"requestId"`
	Value     string `json:"value,omitempty"`
	Confirmed *bool  `json:"confirmed,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`
}
