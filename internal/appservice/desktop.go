package appservice

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"pi-desk/internal/domain"
	"pi-desk/internal/workspace"

	"github.com/natefinch/atomic"
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/mod/semver"
)

const (
	appVersion               = "1.2.0"
	wailsVersion             = "v3.0.0-beta.16"
	updateManifestURL        = "https://api.github.com/repos/saucer-man/pi-desk/releases/latest"
	maxDiagnosticConfigBytes = 4 << 20
	// Pi's Node CLI can take several seconds to initialise on Windows while
	// npm shims and package caches are cold. Bootstrap never waits for this
	// asynchronous probe, so prefer a reliable status over a false negative.
	runtimeProbeTimeout = 8 * time.Second
)

type RuntimeProber interface {
	Probe(ctx context.Context) domain.PiRuntimeStatus
}

type DesktopService struct {
	runtimeProber RuntimeProber
	catalog       *workspace.Catalog
	updateURL     string

	debugMu      sync.Mutex
	debugEnabled bool
}

func NewDesktopService(runtimeProber RuntimeProber, catalogs ...*workspace.Catalog) *DesktopService {
	var catalog *workspace.Catalog
	if len(catalogs) > 0 {
		catalog = catalogs[0]
	}
	return &DesktopService{runtimeProber: runtimeProber, catalog: catalog, updateURL: updateManifestURL}
}

func (service *DesktopService) GetBootstrapState() domain.BootstrapState {
	workingDirectory, _ := os.Getwd()
	state := domain.BootstrapState{
		ProductName:      "Pi Desk",
		AppVersion:       appVersion,
		WailsVersion:     wailsVersion,
		WorkingDirectory: workingDirectory,
		Runtime: domain.PiRuntimeStatus{
			State:   domain.RuntimeChecking,
			Message: "Pi runtime check is pending",
		},
	}
	if service.catalog != nil {
		if window, err := service.catalog.Window(); err == nil {
			state.Window = domain.WindowState{X: window.X, Y: window.Y, Width: window.Width, Height: window.Height, Maximized: window.Maximized, Valid: window.Valid}
		}
	}
	return state
}

// CheckRuntime performs the potentially slow Pi CLI version check after the
// initial desktop state has been returned to the frontend.
func (service *DesktopService) CheckRuntime() domain.PiRuntimeStatus {
	ctx, cancel := context.WithTimeout(context.Background(), runtimeProbeTimeout)
	defer cancel()
	return service.runtimeProber.Probe(ctx)
}

func (service *DesktopService) SaveWindowState(state domain.WindowState) error {
	if service.catalog == nil {
		return errors.New("desktop catalog is unavailable")
	}
	return service.catalog.SaveWindow(workspace.WindowRecord{X: state.X, Y: state.Y, Width: state.Width, Height: state.Height, Maximized: state.Maximized, Valid: state.Valid})
}

type diagnosticFile struct {
	Path       string    `json:"path"`
	Exists     bool      `json:"exists"`
	Size       int64     `json:"size,omitempty"`
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// Only operation names and outcomes are retained, never RPC arguments or raw errors.
type diagnosticOperation struct {
	Operation  string    `json:"operation"`
	Stage      string    `json:"stage"`
	StartedAt  time.Time `json:"startedAt"`
	DurationMS int64     `json:"durationMs"`
}

var recentOperations struct {
	sync.Mutex
	entries []*diagnosticOperation
}

func beginDiagnosticOperation(name string) func(error) {
	for _, char := range name {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("/_-.", char)) {
			name = "unknown"
			break
		}
	}
	if len(name) > 80 {
		name = "unknown"
	}
	entry := &diagnosticOperation{Operation: name, Stage: "running", StartedAt: time.Now().UTC()}
	recentOperations.Lock()
	recentOperations.entries = append(recentOperations.entries, entry)
	if len(recentOperations.entries) > 100 {
		recentOperations.entries = recentOperations.entries[len(recentOperations.entries)-100:]
	}
	recentOperations.Unlock()
	return func(err error) {
		recentOperations.Lock()
		defer recentOperations.Unlock()
		entry.DurationMS = time.Since(entry.StartedAt).Milliseconds()
		entry.Stage = "completed"
		if errors.Is(err, context.DeadlineExceeded) {
			entry.Stage = "timeout"
		} else if errors.Is(err, context.Canceled) {
			entry.Stage = "cancelled"
		} else if err != nil {
			entry.Stage = "failed"
		}
	}
}
func diagnosticOperations() []diagnosticOperation {
	recentOperations.Lock()
	defer recentOperations.Unlock()
	result := make([]diagnosticOperation, 0, len(recentOperations.entries))
	for _, entry := range recentOperations.entries {
		copy := *entry
		if copy.Stage == "running" {
			copy.DurationMS = time.Since(copy.StartedAt).Milliseconds()
		}
		result = append(result, copy)
	}
	return result
}

type diagnosticReport struct {
	RecentOperations []diagnosticOperation  `json:"recentOperations"`
	GeneratedAt      time.Time              `json:"generatedAt"`
	PiDeskVersion    string                 `json:"piDeskVersion"`
	WailsVersion     string                 `json:"wailsVersion"`
	GoVersion        string                 `json:"goVersion"`
	OS               string                 `json:"os"`
	Architecture     string                 `json:"architecture"`
	WorkingDirectory string                 `json:"workingDirectory"`
	PiAgentDirectory string                 `json:"piAgentDirectory"`
	Runtime          domain.PiRuntimeStatus `json:"runtime"`
	WorkspaceCounts  map[string]int         `json:"workspaceCounts"`
	Files            []diagnosticFile       `json:"files"`
	Errors           []string               `json:"errors,omitempty"`
}

func (service *DesktopService) ExportDiagnostics(outputPath string) error {
	outputPath = filepath.Clean(strings.TrimSpace(outputPath))
	if !filepath.IsAbs(outputPath) || !strings.EqualFold(filepath.Ext(outputPath), ".zip") {
		return errors.New("diagnostic output path must be an absolute .zip file")
	}
	agentDirectory, err := defaultPiAgentDirectory()
	if err != nil {
		return err
	}
	workingDirectory, _ := os.Getwd()
	report := diagnosticReport{
		GeneratedAt: time.Now().UTC(), PiDeskVersion: appVersion, WailsVersion: wailsVersion,
		RecentOperations: diagnosticOperations(),
		GoVersion:        runtime.Version(), OS: runtime.GOOS, Architecture: runtime.GOARCH,
		WorkingDirectory: workingDirectory, PiAgentDirectory: agentDirectory,
		Runtime: service.CheckRuntime(), WorkspaceCounts: map[string]int{"local": 0, "ssh": 0},
	}
	if service.catalog != nil {
		if records, listErr := service.catalog.List(); listErr != nil {
			report.Errors = append(report.Errors, "list workspaces: "+listErr.Error())
		} else {
			for _, record := range records {
				report.WorkspaceCounts[string(record.Location.Kind)]++
			}
		}
	}
	for _, name := range []string{"settings.json", "models.json", "mcp.json"} {
		report.Files = append(report.Files, inspectDiagnosticFile(filepath.Join(agentDirectory, name)))
	}

	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, keys := range map[string][]string{
		"config/settings.json": {"theme", "defaultProvider", "defaultModel", "defaultThinkingLevel", "modelThinkingLevels", "hideThinkingBlock", "showCacheMissNotices", "cacheWarming", "thinkingBudgets", "images", "compaction", "branchSummary", "retry", "enabledModels", "warnings"},
		"config/models.json":   {"providers"},
	} {
		content, exists, sanitizeErr := sanitizedDiagnosticJSON(filepath.Join(agentDirectory, filepath.Base(name)), keys)
		if sanitizeErr != nil {
			report.Errors = append(report.Errors, filepath.Base(name)+": "+sanitizeErr.Error())
			continue
		}
		if exists {
			if err := writeDiagnosticZipFile(writer, name, content); err != nil {
				return err
			}
		}
	}
	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode diagnostics: %w", err)
	}
	reportJSON = append(reportJSON, '\n')
	if err := writeDiagnosticZipFile(writer, "diagnostics.json", reportJSON); err != nil {
		return err
	}
	readme := []byte("Pi Desk diagnostic bundle\n\nContains runtime/system metadata and sanitized Pi settings and model configuration.\nSession transcripts, MCP configuration contents, API keys, headers, environment values, cookies, passwords, tokens, and proxy credentials are excluded.\nIncludes up to 100 recent operation names, stages and durations from memory; no arguments or raw error messages. Pi Desk does not persist an application log file.\n")
	if err := writeDiagnosticZipFile(writer, "README.txt", readme); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish diagnostic archive: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return fmt.Errorf("create diagnostic directory: %w", err)
	}
	if err := atomic.WriteFile(outputPath, bytes.NewReader(archive.Bytes())); err != nil {
		return fmt.Errorf("write diagnostic bundle: %w", err)
	}
	if err := os.Chmod(outputPath, 0o600); err != nil {
		return fmt.Errorf("protect diagnostic bundle: %w", err)
	}
	return nil
}

func inspectDiagnosticFile(path string) diagnosticFile {
	result := diagnosticFile{Path: path}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return result
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Exists, result.Size, result.ModifiedAt = info.Mode().IsRegular(), info.Size(), info.ModTime().UTC()
	return result
}

func sanitizedDiagnosticJSON(path string, keys []string) ([]byte, bool, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if len(content) > maxDiagnosticConfigBytes {
		return nil, false, errors.New("file exceeds the 4 MiB safety limit")
	}
	var source map[string]any
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&source); err != nil || source == nil {
		return nil, false, errors.New("file must contain a JSON object")
	}
	if err := requireJSONEnd(decoder); err != nil {
		return nil, false, err
	}
	filtered := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, exists := source[key]; exists {
			filtered[key] = redactDiagnosticValue(key, value)
		}
	}
	data, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return nil, false, err
	}
	return append(data, '\n'), true, nil
}

func redactDiagnosticValue(key string, value any) any {
	if diagnosticSecretKey(key) {
		return "[REDACTED]"
	}
	if key == "providers" {
		return diagnosticModelProviders(value)
	}
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for childKey, childValue := range typed {
			result[childKey] = redactDiagnosticValue(childKey, childValue)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, childValue := range typed {
			result[index] = redactDiagnosticValue("", childValue)
		}
		return result
	case string:
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(typed)), "bearer ") {
			return "[REDACTED]"
		}
		if parsed, err := url.Parse(typed); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
			parsed.User = nil
			query := parsed.Query()
			for queryKey := range query {
				if diagnosticSecretKey(queryKey) {
					query.Set(queryKey, "[REDACTED]")
				}
			}
			parsed.RawQuery = query.Encode()
			return parsed.String()
		}
	}
	return value
}

func diagnosticModelProviders(value any) any {
	providers, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	result := make(map[string]any, len(providers))
	for providerID, value := range providers {
		provider, ok := value.(map[string]any)
		if !ok {
			continue
		}
		summary := map[string]any{}
		for _, key := range []string{"baseUrl", "api"} {
			if field, exists := provider[key]; exists {
				summary[key] = redactDiagnosticValue(key, field)
			}
		}
		_, summary["apiKeyConfigured"] = provider["apiKey"]
		_, summary["headersConfigured"] = provider["headers"]
		if models, ok := provider["models"].([]any); ok {
			safeModels := make([]any, 0, len(models))
			for _, value := range models {
				model, ok := value.(map[string]any)
				if !ok {
					continue
				}
				safeModel := map[string]any{}
				for _, key := range []string{"id", "name", "api", "contextWindow", "maxTokens", "reasoning", "input", "inputLimits", "thinkingLevelMap", "promptCache", "cost"} {
					if field, exists := model[key]; exists {
						safeModel[key] = redactDiagnosticValue(key, field)
					}
				}
				safeModels = append(safeModels, safeModel)
			}
			summary["models"] = safeModels
		}
		result[providerID] = summary
	}
	return result
}

func diagnosticSecretKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(key))
	return normalized == "headers" || normalized == "env" || normalized == "proxy" || normalized == "httpproxy" ||
		strings.HasSuffix(normalized, "apikey") || strings.HasSuffix(normalized, "token") ||
		strings.Contains(normalized, "secret") || strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "credential") || strings.Contains(normalized, "authorization") ||
		strings.Contains(normalized, "cookie") || strings.Contains(normalized, "privatekey")
}

func writeDiagnosticZipFile(writer *zip.Writer, name string, content []byte) error {
	entry, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
	if err != nil {
		return fmt.Errorf("create diagnostic entry %s: %w", name, err)
	}
	if _, err := entry.Write(content); err != nil {
		return fmt.Errorf("write diagnostic entry %s: %w", name, err)
	}
	return nil
}

func (service *DesktopService) ToggleDebugMode() bool {
	service.debugMu.Lock()
	defer service.debugMu.Unlock()
	window, found := application.Get().Window.GetByName("main")
	if !found {
		return service.debugEnabled
	}
	if service.debugEnabled {
		_ = closeDevToolsWindow(window.NativeWindow())
		service.debugEnabled = false
		return false
	}
	window.OpenDevTools()
	service.debugEnabled = true
	return true
}

type updateManifest struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	Notes   string `json:"notes"`
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
}

func (service *DesktopService) CheckForUpdates() domain.UpdateCheckResult {
	result := domain.UpdateCheckResult{Status: "error", CurrentVersion: appVersion, CheckedAt: time.Now().UTC()}
	parsed, err := url.Parse(service.updateURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && !isLocalHTTP(parsed)) {
		result.Status, result.Message = "error", "Update source must use HTTPS"
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, service.updateURL, nil)
	if err != nil {
		result.Status, result.Message = "error", fmt.Sprintf("Create update request: %v", err)
		return result
	}
	request.Header.Set("Accept", "application/json")
	client := http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		result.Status, result.Message = "error", fmt.Sprintf("Check for updates: %v", err)
		return result
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		result.Status, result.Message = "error", fmt.Sprintf("Update source returned HTTP %d", response.StatusCode)
		return result
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		result.Status, result.Message = "error", fmt.Sprintf("Read update manifest: %v", err)
		return result
	}
	var manifest updateManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		result.Status, result.Message = "error", fmt.Sprintf("Decode update manifest: %v", err)
		return result
	}
	result.LatestVersion = strings.TrimSpace(manifest.Version)
	if result.LatestVersion == "" {
		result.LatestVersion = strings.TrimSpace(manifest.TagName)
	}
	result.URL = strings.TrimSpace(manifest.URL)
	if result.URL == "" {
		result.URL = strings.TrimSpace(manifest.HTMLURL)
	}
	result.Notes = strings.TrimSpace(manifest.Notes)
	if result.Notes == "" {
		result.Notes = strings.TrimSpace(manifest.Body)
	}
	if result.LatestVersion == "" {
		result.Status, result.Message = "error", "Update manifest has no version"
		return result
	}
	latestVersion := normalizeSemver(result.LatestVersion)
	currentVersion := normalizeSemver(appVersion)
	if !semver.IsValid(latestVersion) {
		result.Status, result.Message = "error", "Update manifest has an invalid semantic version"
		return result
	}
	if semver.Compare(latestVersion, currentVersion) > 0 {
		result.Status, result.Message = "available", "A newer Pi Desk version is available"
	} else {
		result.Status, result.Message = "current", "Pi Desk is up to date"
	}
	return result
}

func isLocalHTTP(parsed *url.URL) bool {
	if parsed.Scheme != "http" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func normalizeSemver(value string) string {
	value = strings.TrimSpace(value)
	if value != "" && !strings.HasPrefix(value, "v") {
		value = "v" + value
	}
	return value
}
