package piruntime

import (
	"context"
	"os"
	"testing"
	"time"

	"pi-desk/internal/pirpc"
)

func TestExecStarterWithInstalledPi(t *testing.T) {
	if os.Getenv("PI_DESK_LIVE_TEST") != "1" {
		t.Skip("set PI_DESK_LIVE_TEST=1 to run against the installed Pi CLI")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	process, err := NewExecStarter(NewLocator()).Start(ctx, StartConfig{
		ThreadID:      "live-test",
		Workspace:     t.TempDir(),
		Trust:         TrustDeny,
		NoSession:     true,
		Offline:       true,
		DisableThemes: true,
	})
	if err != nil {
		t.Fatalf("start installed Pi: %v", err)
	}
	client := pirpc.NewClient(process, 1, nil)
	t.Cleanup(func() { _ = client.Close() })

	response, err := client.Call(ctx, map[string]any{"type": "get_state"})
	if err != nil {
		t.Fatalf("get_state from installed Pi: %v\nstderr: %s", err, client.Diagnostics())
	}
	if len(response.Data) == 0 {
		t.Fatal("installed Pi returned an empty state")
	}
}
