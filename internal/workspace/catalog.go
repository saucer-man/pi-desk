package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/natefinch/atomic"
)

const stateVersion = 6

const (
	maxDesktopThreads    = 500
	maxScheduledTasks    = 100
	maxDraftBytes        = 1 << 20
	maxThreadTitleLen    = 200
	maxScheduledNameLen  = 200
	maxScheduledModelLen = 256
)

type Record struct {
	ID           string
	Name         string
	Path         string // Local-only compatibility projection; always empty for SSH.
	Location     Location
	Trust        string
	AddedAt      time.Time
	LastOpenedAt time.Time
}

type ThreadRecord struct {
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

type DesktopRecord struct {
	ActiveThreadID string                `json:"activeThreadId,omitempty"`
	Threads        []ThreadRecord        `json:"threads"`
	ScheduledTasks []ScheduledTaskRecord `json:"scheduledTasks,omitempty"`
	Preferences    *PreferencesRecord    `json:"preferences,omitempty"`
	Window         *WindowRecord         `json:"window,omitempty"`
}

type ScheduledTaskRecord struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Prompt        string `json:"prompt"`
	WorkspaceID   string `json:"workspaceId"`
	ModelProvider string `json:"modelProvider,omitempty"`
	ModelID       string `json:"modelId,omitempty"`
	ModelName     string `json:"modelName,omitempty"`
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
	Frequency     string `json:"frequency"`
	Time          string `json:"time,omitempty"`
	Weekday       int    `json:"weekday,omitempty"`
	RunAt         string `json:"runAt,omitempty"`
	Enabled       bool   `json:"enabled"`
	NextRunAt     string `json:"nextRunAt,omitempty"`
	LastRunAt     string `json:"lastRunAt,omitempty"`
	LastThreadID  string `json:"lastThreadId,omitempty"`
	LastStatus    string `json:"lastStatus,omitempty"`
	LastError     string `json:"lastError,omitempty"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

type WindowRecord struct {
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximized bool `json:"maximized"`
	Valid     bool `json:"valid"`
}

type PreferencesRecord struct {
	Appearance           string `json:"appearance"`
	Language             string `json:"language"`
	FontFamily           string `json:"fontFamily"`
	FontSize             int    `json:"fontSize"`
	LightCodeTheme       string `json:"lightCodeTheme,omitempty"`
	DarkCodeTheme        string `json:"darkCodeTheme,omitempty"`
	ShowCodeLineNumbers  bool   `json:"showCodeLineNumbers,omitempty"`
	WrapCodeLines        bool   `json:"wrapCodeLines,omitempty"`
	CodeFontSize         int    `json:"codeFontSize,omitempty"`
	OfflineMode          bool   `json:"offlineMode"`
	ProxyEnabled         bool   `json:"proxyEnabled"`
	ProxyURL             string `json:"proxyUrl,omitempty"`
	StreamingBehavior    string `json:"streamingBehavior"`
	SidebarCollapsed     bool   `json:"sidebarCollapsed"`
	SidebarWidth         int    `json:"sidebarWidth,omitempty"`
	InspectorOpen        bool   `json:"inspectorOpen"`
	InspectorWidth       int    `json:"inspectorWidth,omitempty"`
	InspectorTab         string `json:"inspectorTab"`
	PanelState           string `json:"panelState,omitempty"`
	NotificationsEnabled bool   `json:"notificationsEnabled"`
	UpdateChecksEnabled  bool   `json:"updateChecksEnabled"`
	CloseToTray          bool   `json:"closeToTray"`
	WorkspaceApplication string `json:"workspaceApplication,omitempty"`
}

type stateFile struct {
	Version    int                    `json:"version"`
	Targets    []TargetRecord         `json:"targets,omitempty"`
	Workspaces []stateWorkspaceRecord `json:"workspaces"`
	Desktop    DesktopRecord          `json:"desktop"`
}

type Catalog struct {
	path string
	now  func() time.Time

	mu      sync.RWMutex
	loaded  bool
	records []Record
	targets []TargetRecord
	desktop DesktopRecord
}

func DefaultStatePath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user configuration directory: %w", err)
	}
	return filepath.Join(directory, "pi-desk", "state.json"), nil
}

func NewCatalog(path string) *Catalog {
	return &Catalog{path: path, now: time.Now}
}

func (catalog *Catalog) Load() error {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	return catalog.loadLocked()
}

func (catalog *Catalog) List() ([]Record, error) {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.loadLocked(); err != nil {
		return nil, err
	}
	records := slices.Clone(catalog.records)
	slices.SortFunc(records, func(a, b Record) int {
		return b.LastOpenedAt.Compare(a.LastOpenedAt)
	})
	return records, nil
}

func (catalog *Catalog) ResolveID(id string) (Record, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Record{}, errors.New("workspace id is required")
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.loadLocked(); err != nil {
		return Record{}, err
	}
	for _, record := range catalog.records {
		if record.ID == id {
			return record, nil
		}
	}
	return Record{}, errors.New("workspace is not registered")
}

func (catalog *Catalog) ResolvePath(path string) (Record, error) {
	canonical, err := CanonicalDirectory(path)
	if err != nil {
		return Record{}, err
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.loadLocked(); err != nil {
		return Record{}, err
	}
	for _, record := range catalog.records {
		if pathKey(record.Path) == pathKey(canonical) {
			return record, nil
		}
	}
	return Record{}, errors.New("workspace is not registered")
}

func (catalog *Catalog) Add(path, trust string) (Record, error) {
	canonical, err := CanonicalDirectory(path)
	if err != nil {
		return Record{}, err
	}
	if trust != "approve" && trust != "deny" {
		return Record{}, errors.New("workspace trust must be approve or deny")
	}

	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.loadLocked(); err != nil {
		return Record{}, err
	}
	now := catalog.now().UTC()
	records := slices.Clone(catalog.records)
	for index := range records {
		if pathKey(records[index].Path) != pathKey(canonical) {
			continue
		}
		records[index].Trust = trust
		records[index].LastOpenedAt = now
		if err := catalog.saveLocked(records, catalog.desktop); err != nil {
			return Record{}, err
		}
		catalog.records = records
		return records[index], nil
	}

	id, err := newIdentity("workspace")
	if err != nil {
		return Record{}, err
	}
	record := Record{
		ID:           id,
		Name:         filepath.Base(canonical),
		Path:         canonical,
		Location:     Location{Kind: KindLocal, Local: LocalLocation{CanonicalPath: canonical}},
		Trust:        trust,
		AddedAt:      now,
		LastOpenedAt: now,
	}
	records = append(records, record)
	if err := catalog.saveLocked(records, catalog.desktop); err != nil {
		return Record{}, err
	}
	catalog.records = records
	return record, nil
}

func (catalog *Catalog) Remove(id string) error {
	return catalog.RemoveAfter(id, nil)
}

// RemoveAfter invokes beforeRemove after validation and before catalog state is
// persisted. Remote workspace owners use it to revoke capabilities first.
func (catalog *Catalog) RemoveAfter(id string, beforeRemove func(Record) error) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("workspace id is required")
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.loadLocked(); err != nil {
		return err
	}
	records := slices.Clone(catalog.records)
	index := slices.IndexFunc(records, func(record Record) bool { return record.ID == id })
	if index < 0 {
		return errors.New("workspace not found")
	}
	removed := records[index]
	records = slices.Delete(records, index, index+1)
	desktop := catalog.desktop
	desktop.Threads = slices.DeleteFunc(slices.Clone(desktop.Threads), func(thread ThreadRecord) bool {
		if thread.WorkspaceID == removed.ID {
			return true
		}
		return removed.Location.Kind == KindLocal && thread.WorkspaceID == "" && pathKey(thread.WorkspacePath) == pathKey(removed.Path)
	})
	desktop.ScheduledTasks = slices.DeleteFunc(slices.Clone(desktop.ScheduledTasks), func(task ScheduledTaskRecord) bool {
		return task.WorkspaceID == removed.ID
	})
	if desktop.ActiveThreadID != "" && !slices.ContainsFunc(desktop.Threads, func(thread ThreadRecord) bool {
		return thread.ID == desktop.ActiveThreadID
	}) {
		desktop.ActiveThreadID = ""
	}
	if beforeRemove != nil {
		if err := beforeRemove(removed); err != nil {
			return err
		}
	}
	if err := catalog.saveLocked(records, desktop); err != nil {
		return err
	}
	catalog.records = records
	catalog.desktop = desktop
	return nil
}

func (catalog *Catalog) ForgetSession(path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return errors.New("session path is required")
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.loadLocked(); err != nil {
		return err
	}
	desktop := catalog.desktop
	desktop.Threads = slices.DeleteFunc(slices.Clone(desktop.Threads), func(thread ThreadRecord) bool {
		return thread.SessionPath != "" && pathKey(thread.SessionPath) == pathKey(path)
	})
	if desktop.ActiveThreadID != "" && !slices.ContainsFunc(desktop.Threads, func(thread ThreadRecord) bool {
		return thread.ID == desktop.ActiveThreadID
	}) {
		desktop.ActiveThreadID = ""
	}
	if err := catalog.saveLocked(catalog.records, desktop); err != nil {
		return err
	}
	catalog.desktop = desktop
	return nil
}

func (catalog *Catalog) Desktop() (DesktopRecord, error) {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.loadLocked(); err != nil {
		return DesktopRecord{}, err
	}
	result := catalog.desktop
	result.Threads = cloneThreads(catalog.desktop.Threads)
	result.ScheduledTasks = slices.Clone(catalog.desktop.ScheduledTasks)
	if catalog.desktop.Preferences != nil {
		preferences := *catalog.desktop.Preferences
		result.Preferences = &preferences
	}
	return result, nil
}

func (catalog *Catalog) SaveDesktop(desktop DesktopRecord) error {
	if desktop.Preferences != nil {
		preferences := *desktop.Preferences
		if strings.TrimSpace(preferences.Appearance) == "" {
			preferences.Appearance = "light"
		}
		if strings.TrimSpace(preferences.Language) == "" {
			preferences.Language = "zh-CN"
		}
		if strings.TrimSpace(preferences.FontFamily) == "" {
			preferences.FontFamily = "default"
		}
		if preferences.FontSize == 0 {
			preferences.FontSize = 14
		}
		desktop.Preferences = &preferences
	}
	if err := validateDesktop(desktop); err != nil {
		return err
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.loadLocked(); err != nil {
		return err
	}
	workspaceIDs := make(map[string]Record, len(catalog.records))
	workspacePaths := make(map[string]string, len(catalog.records))
	for _, record := range catalog.records {
		workspaceIDs[record.ID] = record
		if record.Location.Kind == KindLocal {
			workspacePaths[pathKey(record.Path)] = record.ID
		}
	}
	threadIDs := make(map[string]struct{}, len(desktop.Threads))
	for index := range desktop.Threads {
		thread := &desktop.Threads[index]
		knownWorkspace := false
		if thread.WorkspaceID != "" {
			workspaceRecord, exists := workspaceIDs[thread.WorkspaceID]
			if !exists {
				return fmt.Errorf("thread %s references an unknown workspace", thread.ID)
			}
			knownWorkspace = true
			if workspaceRecord.Location.Kind == KindSSH {
				if strings.TrimSpace(thread.WorkspacePath) != "" {
					return fmt.Errorf("thread %s projects an SSH workspace as a local path", thread.ID)
				}
			} else if pathKey(thread.WorkspacePath) != pathKey(workspaceRecord.Path) {
				return fmt.Errorf("thread %s workspace identity does not match its local path", thread.ID)
			}
		} else if workspaceID, ok := workspacePaths[pathKey(thread.WorkspacePath)]; ok {
			thread.WorkspaceID = workspaceID
			knownWorkspace = true
		}
		if !knownWorkspace && thread.SessionPath == "" {
			return fmt.Errorf("thread %s references an unknown workspace", thread.ID)
		}
		if _, exists := threadIDs[thread.ID]; exists {
			return fmt.Errorf("duplicate thread id %s", thread.ID)
		}
		threadIDs[thread.ID] = struct{}{}
	}
	for _, task := range desktop.ScheduledTasks {
		workspaceRecord, exists := workspaceIDs[task.WorkspaceID]
		if !exists {
			return fmt.Errorf("scheduled task %s references an unknown workspace", task.ID)
		}
		if workspaceRecord.Location.Kind != KindLocal || workspaceRecord.Trust != "approve" {
			return fmt.Errorf("scheduled task %s requires a trusted local workspace", task.ID)
		}
	}
	if desktop.ActiveThreadID != "" {
		if _, ok := threadIDs[desktop.ActiveThreadID]; !ok {
			return errors.New("active thread is not present in desktop state")
		}
	}
	desktop.Threads = cloneThreads(desktop.Threads)
	desktop.ScheduledTasks = slices.Clone(desktop.ScheduledTasks)
	if desktop.Preferences != nil {
		preferences := *desktop.Preferences
		desktop.Preferences = &preferences
	}
	if err := catalog.saveLocked(catalog.records, desktop); err != nil {
		return err
	}
	catalog.desktop = desktop
	return nil
}

func (catalog *Catalog) Window() (WindowRecord, error) {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.loadLocked(); err != nil {
		return WindowRecord{}, err
	}
	if catalog.desktop.Window == nil {
		return WindowRecord{}, nil
	}
	return *catalog.desktop.Window, nil
}

func (catalog *Catalog) SaveWindow(window WindowRecord) error {
	if window.Width < 0 || window.Height < 0 {
		return errors.New("window dimensions cannot be negative")
	}
	if window.Valid && (window.Width < 980 || window.Height < 680) {
		return errors.New("window dimensions are below the supported minimum")
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.loadLocked(); err != nil {
		return err
	}
	desktop := catalog.desktop
	desktop.Window = &window
	if err := catalog.saveLocked(catalog.records, desktop); err != nil {
		return err
	}
	catalog.desktop = desktop
	return nil
}

func CanonicalDirectory(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("workspace path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve workspace path: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve workspace links: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("inspect workspace: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("workspace path is not a directory")
	}
	return filepath.Clean(canonical), nil
}

func (catalog *Catalog) loadLocked() error {
	if catalog.loaded {
		return nil
	}
	data, err := os.ReadFile(catalog.path)
	if errors.Is(err, os.ErrNotExist) {
		catalog.records = nil
		catalog.loaded = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("read workspace catalog: %w", err)
	}
	var state stateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("decode workspace catalog: %w", err)
	}
	if state.Version < 1 || state.Version > stateVersion {
		return fmt.Errorf("unsupported workspace catalog version %d", state.Version)
	}
	if state.Version == 1 && state.Desktop.Preferences != nil {
		state.Desktop.Preferences.NotificationsEnabled = true
		state.Desktop.Preferences.UpdateChecksEnabled = true
	}
	if state.Version < stateVersion && state.Desktop.Preferences != nil {
		if state.Version < 3 {
			state.Desktop.Preferences.Appearance = "dark"
		}
		if state.Version < 4 {
			state.Desktop.Preferences.CloseToTray = true
		}
	}
	targets := slices.Clone(state.Targets)
	if state.Version < 6 {
		targets = nil
	}
	targetIDs := make(map[string]TargetRecord, len(targets))
	targetAliases := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if err := validateTarget(target); err != nil {
			return fmt.Errorf("invalid SSH target state: %w", err)
		}
		if _, duplicate := targetIDs[target.ID]; duplicate {
			return errors.New("duplicate SSH target identity")
		}
		aliasKey := targetAliasKey(target.HostAlias)
		if _, duplicate := targetAliases[aliasKey]; duplicate {
			return errors.New("duplicate SSH target alias")
		}
		targetIDs[target.ID] = target
		targetAliases[aliasKey] = struct{}{}
	}

	records := make([]Record, 0, len(state.Workspaces))
	workspaceIDs := make(map[string]struct{}, len(state.Workspaces))
	localPaths := make(map[string]struct{}, len(state.Workspaces))
	for _, persisted := range state.Workspaces {
		record, err := persisted.toRecord(state.Version)
		if err != nil {
			return fmt.Errorf("invalid workspace state: %w", err)
		}
		if state.Version < 6 {
			record.ID, err = newIdentity("workspace")
			if err != nil {
				return err
			}
		}
		if err := validateRecord(record); err != nil {
			return fmt.Errorf("invalid workspace state: %w", err)
		}
		if _, duplicate := workspaceIDs[record.ID]; duplicate {
			return errors.New("duplicate workspace identity")
		}
		workspaceIDs[record.ID] = struct{}{}
		if record.Location.Kind == KindLocal {
			key := pathKey(record.Path)
			if _, duplicate := localPaths[key]; duplicate {
				return errors.New("duplicate local workspace path")
			}
			localPaths[key] = struct{}{}
		} else {
			target, ok := targetIDs[record.Location.SSH.TargetID]
			if !ok || target.HostKey != record.Location.SSH.HostKeyBinding {
				return errors.New("SSH workspace target binding is invalid")
			}
		}
		records = append(records, record)
	}
	desktop := state.Desktop
	desktop.Threads = cloneThreads(state.Desktop.Threads)
	desktop.ScheduledTasks = slices.Clone(state.Desktop.ScheduledTasks)
	if state.Version < 6 {
		for index := range desktop.Threads {
			if workspaceID, ok := workspaceIDForLocalPath(records, desktop.Threads[index].WorkspacePath); ok {
				desktop.Threads[index].WorkspaceID = workspaceID
			}
		}
	}

	catalog.records = records
	catalog.targets = targets
	catalog.desktop = desktop
	if state.Version < stateVersion {
		if err := catalog.saveStateLocked(records, targets, desktop); err != nil {
			return fmt.Errorf("migrate workspace catalog to version %d: %w", stateVersion, err)
		}
	}
	catalog.loaded = true
	return nil
}

func (catalog *Catalog) saveLocked(records []Record, desktop DesktopRecord) error {
	return catalog.saveStateLocked(records, catalog.targets, desktop)
}

func (catalog *Catalog) saveStateLocked(records []Record, targets []TargetRecord, desktop DesktopRecord) error {
	persisted := make([]stateWorkspaceRecord, 0, len(records))
	for _, record := range records {
		encoded, err := stateRecordFromRecord(record)
		if err != nil {
			return fmt.Errorf("encode workspace state: %w", err)
		}
		persisted = append(persisted, encoded)
	}
	for _, target := range targets {
		if err := validateTarget(target); err != nil {
			return fmt.Errorf("encode SSH target state: %w", err)
		}
	}
	data, err := json.MarshalIndent(stateFile{
		Version: stateVersion, Targets: targets, Workspaces: persisted, Desktop: desktop,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode workspace catalog: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(catalog.path), 0o700); err != nil {
		return fmt.Errorf("create workspace catalog directory: %w", err)
	}
	data = append(data, '\n')
	if err := atomic.WriteFile(catalog.path, strings.NewReader(string(data))); err != nil {
		return fmt.Errorf("write workspace catalog: %w", err)
	}
	if err := os.Chmod(catalog.path, 0o600); err != nil {
		return fmt.Errorf("restrict workspace catalog permissions: %w", err)
	}
	return nil
}

func validateDesktop(desktop DesktopRecord) error {
	if len(desktop.Threads) > maxDesktopThreads {
		return fmt.Errorf("desktop state exceeds %d threads", maxDesktopThreads)
	}
	for _, thread := range desktop.Threads {
		if strings.TrimSpace(thread.ID) == "" {
			return errors.New("thread id is required")
		}
		if strings.TrimSpace(thread.Title) == "" || len([]rune(thread.Title)) > maxThreadTitleLen {
			return fmt.Errorf("thread %s has an invalid title", thread.ID)
		}
		if len(thread.Draft) > maxDraftBytes {
			return fmt.Errorf("thread %s draft exceeds 1 MiB", thread.ID)
		}
		if thread.Trust != "approve" && thread.Trust != "deny" {
			return fmt.Errorf("thread %s has an invalid trust mode", thread.ID)
		}
		switch thread.Status {
		case "idle", "starting", "running", "attention":
		default:
			return fmt.Errorf("thread %s has an invalid status", thread.ID)
		}
	}
	if len(desktop.ScheduledTasks) > maxScheduledTasks {
		return fmt.Errorf("desktop state exceeds %d scheduled tasks", maxScheduledTasks)
	}
	taskIDs := make(map[string]struct{}, len(desktop.ScheduledTasks))
	for _, task := range desktop.ScheduledTasks {
		if strings.TrimSpace(task.ID) == "" {
			return errors.New("scheduled task id is required")
		}
		if _, exists := taskIDs[task.ID]; exists {
			return fmt.Errorf("duplicate scheduled task id %s", task.ID)
		}
		taskIDs[task.ID] = struct{}{}
		if strings.TrimSpace(task.Name) == "" || len([]rune(task.Name)) > maxScheduledNameLen {
			return fmt.Errorf("scheduled task %s has an invalid name", task.ID)
		}
		if strings.TrimSpace(task.Prompt) == "" || len(task.Prompt) > maxDraftBytes {
			return fmt.Errorf("scheduled task %s has an invalid prompt", task.ID)
		}
		if strings.TrimSpace(task.WorkspaceID) == "" {
			return fmt.Errorf("scheduled task %s workspace id is required", task.ID)
		}
		if (task.ModelProvider == "") != (task.ModelID == "") ||
			(task.ModelProvider != "" && (!validScheduledModelIdentifier(task.ModelProvider) || !validScheduledModelIdentifier(task.ModelID))) ||
			len([]rune(task.ModelName)) > maxScheduledModelLen || strings.ContainsAny(task.ModelName, "\r\n") {
			return fmt.Errorf("scheduled task %s has an invalid model", task.ID)
		}
		if task.ThinkingLevel != "" && (task.ModelProvider == "" || !validScheduledThinkingLevel(task.ThinkingLevel)) {
			return fmt.Errorf("scheduled task %s has an invalid thinking level", task.ID)
		}
		switch task.Frequency {
		case "once":
			if !validScheduledTimestamp(task.RunAt) {
				return fmt.Errorf("scheduled task %s has an invalid run time", task.ID)
			}
		case "hourly", "daily", "weekdays":
			if !validScheduledClock(task.Time) {
				return fmt.Errorf("scheduled task %s has an invalid clock time", task.ID)
			}
		case "weekly":
			if !validScheduledClock(task.Time) || task.Weekday < 0 || task.Weekday > 6 {
				return fmt.Errorf("scheduled task %s has an invalid weekly schedule", task.ID)
			}
		default:
			return fmt.Errorf("scheduled task %s has an invalid frequency", task.ID)
		}
		for _, value := range []string{task.NextRunAt, task.LastRunAt, task.CreatedAt, task.UpdatedAt} {
			if value != "" && !validScheduledTimestamp(value) {
				return fmt.Errorf("scheduled task %s has an invalid timestamp", task.ID)
			}
		}
		if task.Enabled && task.NextRunAt == "" {
			return fmt.Errorf("scheduled task %s requires a next run time while enabled", task.ID)
		}
		switch task.LastStatus {
		case "", "started", "failed":
		default:
			return fmt.Errorf("scheduled task %s has an invalid last status", task.ID)
		}
	}
	if desktop.Preferences != nil {
		preferences := desktop.Preferences
		switch preferences.Appearance {
		case "dark", "light", "system":
		default:
			return errors.New("invalid appearance preference")
		}
		switch preferences.Language {
		case "zh-CN", "en":
		default:
			return errors.New("invalid language preference")
		}
		switch preferences.FontFamily {
		case "default", "system", "serif", "mono":
		default:
			return errors.New("invalid font family preference")
		}
		if preferences.FontSize < 12 || preferences.FontSize > 18 {
			return errors.New("invalid font size preference")
		}
		validCodeTheme := func(value string) bool {
			switch value {
			case "", "github-light", "github-dark", "vitesse-light", "vitesse-dark", "minimal-light", "minimal-dark", "github-hc-light", "github-hc-dark", "catppuccin-latte", "catppuccin-mocha":
				return true
			default:
				return false
			}
		}
		if !validCodeTheme(preferences.LightCodeTheme) || !validCodeTheme(preferences.DarkCodeTheme) {
			return errors.New("invalid code theme preference")
		}
		if preferences.CodeFontSize != 0 && (preferences.CodeFontSize < 10 || preferences.CodeFontSize > 18) {
			return errors.New("invalid code font size preference")
		}
		switch preferences.StreamingBehavior {
		case "steer", "followUp":
		default:
			return errors.New("invalid streaming behavior preference")
		}
		switch preferences.InspectorTab {
		case "changes", "context", "terminal", "browser":
		default:
			return errors.New("invalid inspector tab preference")
		}
		if preferences.SidebarWidth != 0 && (preferences.SidebarWidth < 180 || preferences.SidebarWidth > 560) {
			return errors.New("invalid sidebar width preference")
		}
		if preferences.InspectorWidth != 0 && (preferences.InspectorWidth < 240 || preferences.InspectorWidth > 840) {
			return errors.New("invalid inspector width preference")
		}
		workspaceApplication := strings.TrimSpace(preferences.WorkspaceApplication)
		if len(workspaceApplication) > 64 || strings.ContainsFunc(workspaceApplication, func(character rune) bool {
			return (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-'
		}) {
			return errors.New("invalid workspace application preference")
		}
		proxyURL := strings.TrimSpace(preferences.ProxyURL)
		if len(proxyURL) > 2048 {
			return errors.New("proxy URL exceeds 2048 bytes")
		}
		if preferences.ProxyEnabled {
			parsed, err := url.Parse(proxyURL)
			if err != nil || parsed.Host == "" {
				return errors.New("proxy URL must include a scheme and host")
			}
			switch strings.ToLower(parsed.Scheme) {
			case "http", "https", "socks5", "socks5h":
			default:
				return errors.New("proxy URL scheme must be http, https, socks5, or socks5h")
			}
			if parsed.User != nil {
				return errors.New("proxy credentials cannot be persisted")
			}
		}
	}
	return nil
}

func validScheduledTimestamp(value string) bool {
	_, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	return err == nil
}

func validScheduledClock(value string) bool {
	_, err := time.Parse("15:04", strings.TrimSpace(value))
	return err == nil
}

func validScheduledModelIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= maxScheduledModelLen && !strings.ContainsAny(value, "\r\n\t ")
}

func validScheduledThinkingLevel(value string) bool {
	switch strings.TrimSpace(value) {
	case "off", "minimal", "low", "medium", "high", "xhigh", "max":
		return true
	default:
		return false
	}
}

func cloneThreads(threads []ThreadRecord) []ThreadRecord {
	return slices.Clone(threads)
}

func workspaceIDForLocalPath(records []Record, workspacePath string) (string, bool) {
	key := pathKey(workspacePath)
	for _, record := range records {
		if record.Location.Kind == KindLocal && pathKey(record.Path) == key {
			return record.ID, true
		}
	}
	return "", false
}

func pathKey(path string) string {
	clean := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}
