package appservice

import (
	"context"
	"net/http"
	"net/http/httptest"
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
		_, _ = response.Write([]byte(`{"tag_name":"1.1.0","html_url":"https://example.com/releases/1.1.0","body":"Bug fixes"}`))
	}))
	defer server.Close()
	service := NewDesktopService(fakeProber{}, nil)
	service.updateURL = server.URL
	result := service.CheckForUpdates()
	if result.Status != "available" || result.LatestVersion != "1.1.0" || result.URL == "" {
		t.Fatalf("unexpected available update: %#v", result)
	}

	server.Config.Handler = http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"version":"1.0.1"}`))
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
	manifest := `{"version":"1.0.1-beta.1"}`
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(manifest))
	}))
	defer server.Close()
	service := NewDesktopService(fakeProber{}, nil)
	service.updateURL = server.URL

	if result := service.CheckForUpdates(); result.Status != "current" {
		t.Fatalf("expected an older prerelease to be current, got %#v", result)
	}
	manifest = `{"version":"1.0.2-beta.1"}`
	if result := service.CheckForUpdates(); result.Status != "available" {
		t.Fatalf("expected a newer prerelease to be available, got %#v", result)
	}
	manifest = `{"version":"release-next"}`
	if result := service.CheckForUpdates(); result.Status != "error" {
		t.Fatalf("expected an invalid semantic version error, got %#v", result)
	}
}
