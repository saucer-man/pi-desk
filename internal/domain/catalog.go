package domain

import (
	"encoding/json"
	"time"
)

type WorkspaceSummary struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	Kind         string    `json:"kind"`
	TargetID     string    `json:"targetId,omitempty"`
	RemoteRoot   string    `json:"remoteRoot,omitempty"`
	Trust        string    `json:"trust"`
	AddedAt      time.Time `json:"addedAt"`
	LastOpenedAt time.Time `json:"lastOpenedAt"`
}

type RemoteAliasSummary struct {
	Name  string `json:"name"`
	Risky bool   `json:"risky"`
}

type RemoteTargetSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	HostAlias string `json:"hostAlias"`
}

type ConnectRemoteTargetRequest struct {
	TargetID  string `json:"targetId,omitempty"`
	Name      string `json:"name,omitempty"`
	HostAlias string `json:"hostAlias,omitempty"`
}

type PrepareRemoteRootRequest struct {
	TargetID      string `json:"targetId"`
	Name          string `json:"name"`
	RequestedRoot string `json:"requestedRoot"`
}

type RemoteRootCandidate struct {
	Token            string `json:"token"`
	TargetID         string `json:"targetId"`
	HostAlias        string `json:"hostAlias"`
	HostKeyAlgorithm string `json:"hostKeyAlgorithm"`
	HostKeySHA256    string `json:"hostKeySha256"`
	CanonicalRoot    string `json:"canonicalRoot"`
	Device           string `json:"device"`
	Inode            string `json:"inode"`
}

type DecideRemoteRootRequest struct {
	Token string `json:"token"`
	Trust string `json:"trust"`
}

type RemoteTargetRequest struct {
	TargetID string `json:"targetId"`
}

type ResumeRemoteWorkspaceRequest struct {
	WorkspaceID string `json:"workspaceId"`
}

type AddWorkspaceRequest struct {
	Path  string `json:"path"`
	Trust string `json:"trust"`
}

type RenameWorkspaceRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type WorkspaceRequest struct {
	ID string `json:"id"`
}

type WorkspaceApplication struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	IconDataURL string `json:"iconDataUrl"`
}

type OpenWorkspaceWithRequest struct {
	WorkspaceID   string `json:"workspaceId"`
	ApplicationID string `json:"applicationId"`
}

type PickWorkspaceRequest struct {
	InitialPath string `json:"initialPath,omitempty"`
}

type ListSessionsRequest struct {
	WorkspacePath string `json:"workspacePath,omitempty"`
}

type DeleteSessionRequest struct {
	Path string `json:"path"`
}

type SessionSnapshotRequest struct {
	Path string `json:"path"`
}

type RestoreOrphanSessionRequest struct {
	Path        string `json:"path"`
	WorkspaceID string `json:"workspaceId"`
}

type SessionModel struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
}

type SessionSnapshot struct {
	Messages     []json.RawMessage `json:"messages"`
	Model        *SessionModel     `json:"model,omitempty"`
	MessageCount int               `json:"messageCount"`
}

type SessionTokenUsage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
	Reasoning  int64 `json:"reasoning"`
	Total      int64 `json:"total"`
}

type SessionModelUsage struct {
	Provider          string            `json:"provider"`
	Model             string            `json:"model"`
	AssistantMessages int               `json:"assistantMessages"`
	Tokens            SessionTokenUsage `json:"tokens"`
	Cost              float64           `json:"cost"`
}

type SessionUsageSummary struct {
	Sessions          int                 `json:"sessions"`
	Messages          int                 `json:"messages"`
	UserMessages      int                 `json:"userMessages"`
	AssistantMessages int                 `json:"assistantMessages"`
	ToolResults       int                 `json:"toolResults"`
	Tokens            SessionTokenUsage   `json:"tokens"`
	Cost              float64             `json:"cost"`
	Models            []SessionModelUsage `json:"models"`
}

type DeletedSession struct {
	RecoveryPath string `json:"recoveryPath"`
}

type OrphanSessionSummary struct {
	ID                string    `json:"id"`
	Path              string    `json:"path"`
	AnchorWorkspaceID string    `json:"anchorWorkspaceId"`
	TargetID          string    `json:"targetId,omitempty"`
	RemoteRoot        string    `json:"remoteRoot,omitempty"`
	Name              string    `json:"name,omitempty"`
	Title             string    `json:"title"`
	FirstMessage      string    `json:"firstMessage"`
	CreatedAt         time.Time `json:"createdAt"`
	ModifiedAt        time.Time `json:"modifiedAt"`
	MessageCount      int       `json:"messageCount"`
}

type ExportOrphanSessionRequest struct {
	Path       string `json:"path"`
	OutputPath string `json:"outputPath"`
}

type SessionSummary struct {
	ID                string    `json:"id"`
	Path              string    `json:"path"`
	CWD               string    `json:"cwd"`
	AnchorWorkspaceID string    `json:"anchorWorkspaceId,omitempty"`
	Name              string    `json:"name,omitempty"`
	Title             string    `json:"title"`
	FirstMessage      string    `json:"firstMessage"`
	CreatedAt         time.Time `json:"createdAt"`
	ModifiedAt        time.Time `json:"modifiedAt"`
	MessageCount      int       `json:"messageCount"`
	ParentSessionPath string    `json:"parentSessionPath,omitempty"`
}

type DesktopThreadState struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	WorkspaceID   string `json:"workspaceId,omitempty"`
	WorkspacePath string `json:"workspacePath"`
	Trust         string `json:"trust"`
	Status        string `json:"status"`
	SessionPath   string `json:"sessionPath,omitempty"`
	Draft         string `json:"draft,omitempty"`
	CreatedAt     string `json:"createdAt,omitempty"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
	Unread        bool   `json:"unread,omitempty"`
}

type DesktopPreferences struct {
	Appearance           string `json:"appearance"`
	Language             string `json:"language"`
	OfflineMode          bool   `json:"offlineMode"`
	ProxyEnabled         bool   `json:"proxyEnabled"`
	ProxyURL             string `json:"proxyUrl,omitempty"`
	StreamingBehavior    string `json:"streamingBehavior"`
	SidebarCollapsed     bool   `json:"sidebarCollapsed"`
	SidebarWidth         int    `json:"sidebarWidth,omitempty"`
	InspectorOpen        bool   `json:"inspectorOpen"`
	InspectorWidth       int    `json:"inspectorWidth,omitempty"`
	InspectorTab         string `json:"inspectorTab"`
	NotificationsEnabled bool   `json:"notificationsEnabled"`
	UpdateChecksEnabled  bool   `json:"updateChecksEnabled"`
	CloseToTray          bool   `json:"closeToTray"`
	WorkspaceApplication string `json:"workspaceApplication,omitempty"`
}

type DesktopState struct {
	ActiveThreadID string               `json:"activeThreadId,omitempty"`
	Threads        []DesktopThreadState `json:"threads"`
	Preferences    *DesktopPreferences  `json:"preferences,omitempty"`
}
