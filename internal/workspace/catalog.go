package workspace

import (
	"crypto/sha256"
	"encoding/hex"
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

const stateVersion = 5

const (
	maxDesktopThreads = 500
	maxDraftBytes     = 1 << 20
	maxThreadTitleLen = 200
)

type Record struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	Trust        string    `json:"trust"`
	AddedAt      time.Time `json:"addedAt"`
	LastOpenedAt time.Time `json:"lastOpenedAt"`
}

type ThreadRecord struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
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
	ActiveThreadID string             `json:"activeThreadId,omitempty"`
	Threads        []ThreadRecord     `json:"threads"`
	Preferences    *PreferencesRecord `json:"preferences,omitempty"`
	Window         *WindowRecord      `json:"window,omitempty"`
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

type stateFile struct {
	Version    int           `json:"version"`
	Workspaces []Record      `json:"workspaces"`
	Desktop    DesktopRecord `json:"desktop"`
}

type Catalog struct {
	path string
	now  func() time.Time

	mu      sync.RWMutex
	loaded  bool
	records []Record
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

	record := Record{
		ID:           workspaceID(canonical),
		Name:         filepath.Base(canonical),
		Path:         canonical,
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
	removedPath := records[index].Path
	records = slices.Delete(records, index, index+1)
	desktop := catalog.desktop
	desktop.Threads = slices.DeleteFunc(slices.Clone(desktop.Threads), func(thread ThreadRecord) bool {
		return pathKey(thread.WorkspacePath) == pathKey(removedPath)
	})
	if desktop.ActiveThreadID != "" && !slices.ContainsFunc(desktop.Threads, func(thread ThreadRecord) bool {
		return thread.ID == desktop.ActiveThreadID
	}) {
		desktop.ActiveThreadID = ""
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
	if catalog.desktop.Preferences != nil {
		preferences := *catalog.desktop.Preferences
		result.Preferences = &preferences
	}
	return result, nil
}

func (catalog *Catalog) SaveDesktop(desktop DesktopRecord) error {
	if desktop.Preferences != nil && (strings.TrimSpace(desktop.Preferences.Appearance) == "" || strings.TrimSpace(desktop.Preferences.Language) == "") {
		preferences := *desktop.Preferences
		if strings.TrimSpace(preferences.Appearance) == "" {
			preferences.Appearance = "light"
		}
		if strings.TrimSpace(preferences.Language) == "" {
			preferences.Language = "zh-CN"
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
	workspacePaths := make(map[string]struct{}, len(catalog.records))
	for _, record := range catalog.records {
		workspacePaths[pathKey(record.Path)] = struct{}{}
	}
	threadIDs := make(map[string]struct{}, len(desktop.Threads))
	for _, thread := range desktop.Threads {
		if _, ok := workspacePaths[pathKey(thread.WorkspacePath)]; !ok && thread.SessionPath == "" {
			return fmt.Errorf("thread %s references an unknown workspace", thread.ID)
		}
		if _, exists := threadIDs[thread.ID]; exists {
			return fmt.Errorf("duplicate thread id %s", thread.ID)
		}
		threadIDs[thread.ID] = struct{}{}
	}
	if desktop.ActiveThreadID != "" {
		if _, ok := threadIDs[desktop.ActiveThreadID]; !ok {
			return errors.New("active thread is not present in desktop state")
		}
	}
	desktop.Threads = cloneThreads(desktop.Threads)
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
	catalog.records = slices.Clone(state.Workspaces)
	catalog.desktop = state.Desktop
	catalog.desktop.Threads = cloneThreads(state.Desktop.Threads)
	catalog.loaded = true
	return nil
}

func (catalog *Catalog) saveLocked(records []Record, desktop DesktopRecord) error {
	data, err := json.MarshalIndent(stateFile{
		Version: stateVersion, Workspaces: records, Desktop: desktop,
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
		switch preferences.StreamingBehavior {
		case "steer", "followUp":
		default:
			return errors.New("invalid streaming behavior preference")
		}
		switch preferences.InspectorTab {
		case "changes", "context", "terminal":
		default:
			return errors.New("invalid inspector tab preference")
		}
		if preferences.SidebarWidth != 0 && (preferences.SidebarWidth < 220 || preferences.SidebarWidth > 480) {
			return errors.New("invalid sidebar width preference")
		}
		if preferences.InspectorWidth != 0 && (preferences.InspectorWidth < 280 || preferences.InspectorWidth > 720) {
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

func cloneThreads(threads []ThreadRecord) []ThreadRecord {
	return slices.Clone(threads)
}

func workspaceID(path string) string {
	sum := sha256.Sum256([]byte(pathKey(path)))
	return "workspace-" + hex.EncodeToString(sum[:12])
}

func pathKey(path string) string {
	clean := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}
