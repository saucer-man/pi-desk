package appservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"pi-desk/internal/domain"
	"pi-desk/internal/workspace"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/mod/semver"
)

const (
	appVersion        = "0.1.0"
	wailsVersion      = "v3.0.0-beta.6"
	updateManifestURL = "https://api.github.com/repos/saucer-man/pi-desk/releases/latest"
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
