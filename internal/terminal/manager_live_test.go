package terminal

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestManagerRunsInstalledInteractiveShell(t *testing.T) {
	if os.Getenv("PI_DESK_LIVE_TEST") != "1" {
		t.Skip("set PI_DESK_LIVE_TEST=1 to run against the installed interactive shell")
	}
	cwd := t.TempDir()
	events := make(chan Event, 32)
	manager := NewManager(context.Background(), func(event Event) { events <- event })
	t.Cleanup(manager.Shutdown)
	state, err := manager.Start(StartConfig{ThreadID: "shell-smoke", CWD: cwd, Columns: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	shell := strings.ToLower(filepath.Base(state.Shell))
	command := "printf 'pi-desk-%s\\n' 'terminal-smoke'\n"
	if runtime.GOOS == "windows" {
		if strings.HasPrefix(shell, "cmd") {
			command = "echo pi-desk-terminal^smoke\r"
		} else {
			command = "Write-Output ('pi-desk-' + 'terminal-smoke')\r"
		}
	}
	var output bytes.Buffer
	startupDeadline := time.After(10 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Type == "output" {
				_, _ = output.Write(event.Data)
				goto shellReady
			}
			if event.Type == "exit" {
				t.Fatalf("interactive shell exited during startup: %s", event.Error)
			}
		case <-startupDeadline:
			t.Fatal("interactive shell did not produce startup output")
		}
	}

shellReady:
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Type == "output" {
				_, _ = output.Write(event.Data)
			}
			if bytes.Contains(output.Bytes(), []byte("pi-desk-terminal-smoke")) {
				if err := manager.Stop("shell-smoke"); err != nil {
					t.Fatal(err)
				}
				return
			}
			if event.Type == "exit" {
				t.Fatalf("interactive shell exited before returning output: %s", event.Error)
			}
		case <-ticker.C:
			if err := manager.Write("shell-smoke", []byte(command)); err != nil {
				t.Fatal(err)
			}
		case <-deadline:
			_ = manager.Stop("shell-smoke")
			t.Fatalf("interactive shell did not execute input; output: %q", output.String())
		}
	}
}
