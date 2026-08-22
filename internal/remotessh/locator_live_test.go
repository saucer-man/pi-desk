package remotessh

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestDiscoverInstalledSSHConfig(t *testing.T) {
	if os.Getenv("PI_DESK_LIVE_TEST") != "1" {
		t.Skip("set PI_DESK_LIVE_TEST=1 to statically inspect the installed SSH config")
	}

	if _, err := DiscoverSSHConfig(DiscoveryOptions{}); err != nil {
		t.Fatalf("discover installed SSH config: %v", err)
	}
}

func TestLocatorWithInstalledOpenSSH(t *testing.T) {
	if os.Getenv("PI_DESK_LIVE_TEST") != "1" {
		t.Skip("set PI_DESK_LIVE_TEST=1 to probe the installed OpenSSH client")
	}
	assertLiveOpenSSH(t, NewLocator())
}

func TestLocatorWithPathOpenSSH(t *testing.T) {
	if os.Getenv("PI_DESK_LIVE_TEST") != "1" {
		t.Skip("set PI_DESK_LIVE_TEST=1 to probe the PATH OpenSSH client")
	}
	// A non-Windows locator bypasses the System32 preference while retaining
	// the real runner's platform-specific process environment.
	assertLiveOpenSSH(t, newLocator(osCommandRunner{}, "linux", ""))
}

func assertLiveOpenSSH(t *testing.T, locator *Locator) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := locator.Probe(ctx)
	if err != nil {
		t.Fatalf("probe installed OpenSSH: %v", err)
	}
	if info.Executable == "" || info.Version == "" {
		t.Fatalf("installed OpenSSH returned incomplete info: %#v", info)
	}

	config, err := locator.PreflightConfig(ctx, "localhost")
	if err != nil {
		if errors.Is(err, ErrSSHEnvironmentUnsafe) {
			t.Fatalf("installed OpenSSH profile has unsafe environment settings: %v", err)
		}
		t.Fatalf("run installed OpenSSH config preflight: %v", err)
	}
	if config.Fingerprint == "" || config.HostName == "" || config.Port == 0 {
		t.Fatalf("preflight returned incomplete effective config: %#v", config)
	}
}
