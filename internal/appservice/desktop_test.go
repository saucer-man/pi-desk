package appservice

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"pi-desk/internal/domain"
)

type fakeProber struct {
	status domain.PiRuntimeStatus
	calls  *int
}

type deadlineProber struct {
	remaining time.Duration
}

func (prober *deadlineProber) Probe(ctx context.Context) domain.PiRuntimeStatus {
	deadline, ok := ctx.Deadline()
	if !ok {
		return domain.PiRuntimeStatus{State: domain.RuntimeError, Message: "missing deadline"}
	}
	prober.remaining = time.Until(deadline)
	return domain.PiRuntimeStatus{State: domain.RuntimeReady}
}

func (prober fakeProber) Probe(context.Context) domain.PiRuntimeStatus {
	if prober.calls != nil {
		(*prober.calls)++
	}
	return prober.status
}

func TestCloseDevToolsWindowRejectsNilParent(t *testing.T) {
	if closeDevToolsWindow(unsafe.Pointer(nil)) {
		t.Fatal("nil parent must not close a window")
	}
}

func TestGetBootstrapState(t *testing.T) {
	calls := 0
	service := NewDesktopService(fakeProber{status: domain.PiRuntimeStatus{
		State:   domain.RuntimeReady,
		Version: "0.84.1",
	}, calls: &calls})

	state := service.GetBootstrapState()

	if state.ProductName != "Pi Desk" || state.Runtime.State != domain.RuntimeChecking || calls != 0 {
		t.Fatalf("unexpected bootstrap state: %#v", state)
	}
}

func TestCheckRuntimeProbesPiAfterBootstrap(t *testing.T) {
	calls := 0
	service := NewDesktopService(fakeProber{status: domain.PiRuntimeStatus{
		State:   domain.RuntimeReady,
		Version: "0.84.1",
	}, calls: &calls})

	status := service.CheckRuntime()

	if status.State != domain.RuntimeReady || status.Version != "0.84.1" || calls != 1 {
		t.Fatalf("unexpected runtime status: %#v (calls=%d)", status, calls)
	}
}

func TestCheckRuntimeAllowsColdWindowsNPMShim(t *testing.T) {
	prober := &deadlineProber{}
	service := NewDesktopService(prober)
	if result := service.CheckRuntime(); result.State != domain.RuntimeReady {
		t.Fatalf("unexpected runtime result: %#v", result)
	}
	if prober.remaining < 7*time.Second || prober.remaining > runtimeProbeTimeout {
		t.Fatalf("unexpected probe budget: %s", prober.remaining)
	}
}

func TestCheckForUpdates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"tag_name":"1.3.0","html_url":"https://example.com/releases/1.3.0","body":"Bug fixes"}`))
	}))
	defer server.Close()
	service := NewDesktopService(fakeProber{}, nil)
	service.updateURL = server.URL
	result := service.CheckForUpdates()
	if result.Status != "available" || result.LatestVersion != "1.3.0" || result.URL == "" {
		t.Fatalf("unexpected available update: %#v", result)
	}

	server.Config.Handler = http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"version":"1.2.0"}`))
	})
	result = service.CheckForUpdates()
	if result.Status != "current" {
		t.Fatalf("expected current version, got %#v", result)
	}
}

func TestCheckForUpdatesRejectsRemoteHTTP(t *testing.T) {
	service := NewDesktopService(fakeProber{}, nil)
	service.updateURL = "http://updates.example.com/latest.json"
	result := service.CheckForUpdates()
	if result.Status != "error" {
		t.Fatalf("expected remote HTTP source rejection, got %#v", result)
	}
}

func TestCheckForUpdatesUsesSemanticVersionOrdering(t *testing.T) {
	manifest := `{"version":"1.2.0-beta.1"}`
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(manifest))
	}))
	defer server.Close()
	service := NewDesktopService(fakeProber{}, nil)
	service.updateURL = server.URL

	if result := service.CheckForUpdates(); result.Status != "current" {
		t.Fatalf("expected an older prerelease to be current, got %#v", result)
	}
	manifest = `{"version":"1.2.1-beta.1"}`
	if result := service.CheckForUpdates(); result.Status != "available" {
		t.Fatalf("expected a newer prerelease to be available, got %#v", result)
	}
	manifest = `{"version":"release-next"}`
	if result := service.CheckForUpdates(); result.Status != "error" {
		t.Fatalf("expected an invalid semantic version error, got %#v", result)
	}
}

func TestExportDiagnosticsRedactsCredentials(t *testing.T) {
	agentDirectory := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDirectory)
	settings := `{"defaultModel":"gpt-test","httpProxy":"https://user:proxy-secret@example.com","packages":[{"source":"https://token:secret@example.com/pkg"}]}`
	models := `{"providers":{"custom":{"baseUrl":"https://user:pass@gateway.example.com/v1?token=query-secret","apiKey":"sk-secret","headers":{"Authorization":"Bearer header-secret"},"future":"provider-future-secret","models":[{"id":"gpt-test","maxTokens":16000,"future":"model-future-secret"}]}}}`
	if err := os.WriteFile(filepath.Join(agentDirectory, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDirectory, "models.json"), []byte(models), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDirectory, "mcp.json"), []byte(`{"mcpServers":{"private":{"env":{"TOKEN":"mcp-secret"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewDesktopService(fakeProber{status: domain.PiRuntimeStatus{State: domain.RuntimeReady, Version: "0.87.0"}})
	outputPath := filepath.Join(t.TempDir(), "pi-desk-diagnostics.zip")
	if err := service.ExportDiagnostics(outputPath); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	entries := map[string]string{}
	for _, file := range reader.File {
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(stream)
		_ = stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[file.Name] = string(content)
	}
	for _, name := range []string{"README.txt", "diagnostics.json", "config/settings.json", "config/models.json"} {
		if entries[name] == "" {
			t.Fatalf("missing diagnostic entry %s: %#v", name, entries)
		}
	}
	all := strings.Join([]string{entries["config/settings.json"], entries["config/models.json"]}, "\n")
	for _, secret := range []string{"proxy-secret", "query-secret", "sk-secret", "header-secret", "mcp-secret", "user:pass", "token:secret", "provider-future-secret", "model-future-secret"} {
		if strings.Contains(all, secret) {
			t.Fatalf("diagnostic bundle leaked %q:\n%s", secret, all)
		}
	}
	if !strings.Contains(all, "gpt-test") || !strings.Contains(all, `"apiKeyConfigured": true`) {
		t.Fatalf("sanitized configuration is incomplete:\n%s", all)
	}
	if !strings.Contains(entries["diagnostics.json"], `"version": "0.87.0"`) || !strings.Contains(entries["diagnostics.json"], "mcp.json") {
		t.Fatalf("diagnostic report is incomplete:\n%s", entries["diagnostics.json"])
	}
}

func TestExportDiagnosticsRejectsUnsafeOutputPath(t *testing.T) {
	service := NewDesktopService(fakeProber{})
	for _, path := range []string{"relative.zip", filepath.Join(t.TempDir(), "diagnostics.txt")} {
		if err := service.ExportDiagnostics(path); err == nil {
			t.Fatalf("expected output path rejection for %s", path)
		}
	}
}

func TestRecentDiagnosticOperationsBoundedAndRedacted(t *testing.T) {
	recentOperations.Lock()
	recentOperations.entries = nil
	recentOperations.Unlock()
	for i := 0; i < 105; i++ {
		beginDiagnosticOperation("pi/get_state")(nil)
	}
	finish := beginDiagnosticOperation("browser/navigate")
	entries := diagnosticOperations()
	if len(entries) != 100 || entries[len(entries)-1].Stage != "running" {
		t.Fatalf("running entries: %#v", entries)
	}
	finish(context.DeadlineExceeded)
	beginDiagnosticOperation("pi/prompt")(errors.New("SECRET_API_KEY prompt and cookies"))
	beginDiagnosticOperation("browser/call")(context.Canceled)
	beginDiagnosticOperation("https://secret.example/?token=SECRET")(nil)
	entries = diagnosticOperations()
	if entries[96].Stage != "timeout" || entries[97].Stage != "failed" || entries[98].Stage != "cancelled" || entries[99].Operation != "unknown" {
		t.Fatalf("outcomes: %#v", entries[96:])
	}
	content, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "SECRET") || strings.Contains(string(content), "cookies") {
		t.Fatal("diagnostic leaked raw error or arguments")
	}
}
