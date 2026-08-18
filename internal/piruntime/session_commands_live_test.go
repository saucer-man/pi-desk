package piruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pi-desk/internal/pirpc"
)

func TestInstalledPiSessionCommands(t *testing.T) {
	if os.Getenv("PI_DESK_LIVE_TEST") != "1" {
		t.Skip("set PI_DESK_LIVE_TEST=1 to run against the installed Pi CLI")
	}

	workspace := t.TempDir()
	sessionPath := filepath.Join(workspace, "source.jsonl")
	header := map[string]any{
		"type": "session", "version": 3, "id": "live-session", "timestamp": "2026-08-10T08:00:00Z", "cwd": workspace,
	}
	message := map[string]any{
		"type": "message", "id": "user-entry", "parentId": nil, "timestamp": "2026-08-10T08:00:01Z",
		"message": map[string]any{"role": "user", "content": "Inspect the temporary workspace", "timestamp": 1786348801000},
	}
	writeJSONLFixture(t, sessionPath, header, message)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	process, err := NewExecStarter(NewLocator()).Start(ctx, StartConfig{
		ThreadID: "live-session-commands", Workspace: workspace, SessionPath: sessionPath,
		Trust: TrustDeny, Offline: true, DisableThemes: true, DisableSkills: true, DisablePlugins: true,
	})
	if err != nil {
		t.Fatalf("start installed Pi: %v", err)
	}
	client := pirpc.NewClient(process, 1, nil)
	t.Cleanup(func() { _ = client.Close() })

	for _, command := range []map[string]any{{"type": "get_entries"}, {"type": "get_fork_messages"}} {
		response, err := client.Call(ctx, command)
		if err != nil || len(response.Data) == 0 {
			t.Fatalf("%s failed: data=%s err=%v stderr=%s", command["type"], response.Data, err, client.Diagnostics())
		}
	}

	exportPath := filepath.Join(workspace, "export.html")
	if _, err := client.Call(ctx, map[string]any{"type": "export_html", "outputPath": exportPath}); err != nil {
		t.Fatalf("export_html failed: %v\nstderr: %s", err, client.Diagnostics())
	}
	if info, err := os.Stat(exportPath); err != nil || info.Size() == 0 {
		t.Fatalf("exported HTML missing or empty: %v", err)
	}

	if _, err := client.Call(ctx, map[string]any{"type": "clone"}); err != nil {
		t.Fatalf("clone failed: %v\nstderr: %s", err, client.Diagnostics())
	}
	clonedPath := liveSessionPath(t, ctx, client)
	if clonedPath == "" || clonedPath == sessionPath {
		t.Fatalf("clone did not switch session: %q", clonedPath)
	}

	if _, err := client.Call(ctx, map[string]any{"type": "fork", "entryId": "user-entry"}); err != nil {
		t.Fatalf("fork failed: %v\nstderr: %s", err, client.Diagnostics())
	}
	forkedPath := liveSessionPath(t, ctx, client)
	if forkedPath == "" || forkedPath == clonedPath {
		t.Fatalf("fork did not switch session: %q", forkedPath)
	}
}

func writeJSONLFixture(t *testing.T, path string, records ...map[string]any) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func liveSessionPath(t *testing.T, ctx context.Context, client *pirpc.Client) string {
	t.Helper()
	response, err := client.Call(ctx, map[string]any{"type": "get_state"})
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		SessionFile string `json:"sessionFile"`
	}
	if err := json.Unmarshal(response.Data, &state); err != nil {
		t.Fatal(err)
	}
	return state.SessionFile
}
