package remotessh

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveSSHPlatformProbe(t *testing.T) {
	hostAlias := strings.TrimSpace(os.Getenv("PI_DESK_SSH_LIVE_TARGET"))
	if hostAlias == "" {
		t.Skip("set PI_DESK_SSH_LIVE_TARGET to run the real platform probe")
	}

	locator := newLiveLocator(t)
	target, err := NewTarget(hostAlias)
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := NewConnectionSupervisor(locator, target)
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	ready, err := supervisor.Connect(ctx)
	if err != nil {
		t.Fatalf("live SSH connect failed: %v", err)
	}
	if ready.State != ConnectionReady || ready.Generation == 0 {
		t.Fatalf("live SSH connection is not ready: %#v", ready)
	}

	platform, err := supervisor.ProbePlatform(ctx, ready.Generation)
	if err != nil {
		t.Fatalf("live SSH platform probe failed: %v", err)
	}
	if platform.OS != "linux" || platform.Arch == "" {
		t.Fatalf("unexpected live SSH platform: %#v", platform)
	}
}
