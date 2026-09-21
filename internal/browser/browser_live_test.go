package browser

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestLiveManagedBrowserEndToEnd launches the real installed Chrome (or Edge)
// with a dedicated profile and exercises the whole bridge: target discovery,
// CDP calls, screencast frames with acks, and input injection. Gated behind
// PI_DESK_LIVE_TEST=1 like the terminal live test.
func TestLiveManagedBrowserEndToEnd(t *testing.T) {
	if os.Getenv("PI_DESK_LIVE_TEST") != "1" {
		t.Skip("set PI_DESK_LIVE_TEST=1 to run against the installed Chrome/Edge")
	}
	if runtime.GOOS != "windows" {
		t.Skip("managed browser live test runs on Windows only")
	}
	executable := ""
	for _, candidate := range []string{
		filepath.Join(os.Getenv("ProgramFiles"), `Google\Chrome\Application\chrome.exe`),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Google\Chrome\Application\chrome.exe`),
		filepath.Join(os.Getenv("LocalAppData"), `Google\Chrome\Application\chrome.exe`),
		filepath.Join(os.Getenv("ProgramFiles"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Microsoft\Edge\Application\msedge.exe`),
	} {
		if candidate != "" {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				executable = candidate
				break
			}
		}
	}
	if executable == "" {
		t.Skip("no Chrome or Edge installation found")
	}

	profile := t.TempDir()
	command := exec.Command(executable,
		"--remote-debugging-port=0", "--user-data-dir="+profile,
		"--no-first-run", "--no-default-browser-check",
		"--disable-session-crashed-bubble", "--hide-crash-restore-bubble",
		"--window-size=1200,800", "about:blank")
	if err := command.Start(); err != nil {
		t.Fatalf("launch managed browser: %v", err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	})

	var target Target
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		candidate, err := DiscoverPage(profile)
		if err == nil {
			target = candidate
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if target.PageWS == "" {
		t.Fatal("managed browser never exposed a page target")
	}

	client, err := Dial(context.Background(), target.PageWS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	frames := make(chan struct{}, 64)
	client.OnEvent = func(method string, params json.RawMessage) {
		if method == "Page.screencastFrame" {
			var frame struct {
				Metadata struct {
					DeviceWidth  float64 `json:"deviceWidth"`
					DeviceHeight float64 `json:"deviceHeight"`
				} `json:"metadata"`
			}
			if err := json.Unmarshal(params, &frame); err != nil {
				t.Error(err)
			}
			frames <- struct{}{}
		}
	}
	if err := client.Call("Page.enable", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.Call("Emulation.setFocusEmulationEnabled", map[string]bool{"enabled": true}, nil); err != nil {
		t.Fatal(err)
	}

	page := "data:text/html,<title>pi-desk-live</title><h1 id=q>pi-desk-live</h1><input id=f>"
	if err := client.Call("Page.navigate", map[string]string{"url": page}, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1 * time.Second)

	var heading struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := client.Call("Runtime.evaluate", map[string]any{"expression": "document.getElementById('q').textContent", "returnByValue": true}, &heading); err != nil {
		t.Fatal(err)
	}
	if heading.Result.Value != "pi-desk-live" {
		t.Fatalf("unexpected page content %q", heading.Result.Value)
	}

	if err := client.Call("Page.startScreencast", map[string]any{"format": "jpeg", "quality": 60, "maxWidth": 1600, "maxHeight": 1600}, nil); err != nil {
		t.Fatal(err)
	}
	// Screencast is content-triggered: a static page produces exactly one
	// frame. The first frame must arrive, then mutating the DOM must produce
	// another one — which only happens if the reader loop acked the first.
	waitFrame := func(what string) {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			select {
			case <-frames:
				return
			case <-deadline:
				t.Fatalf("screencast never delivered a frame %s", what)
			}
		}
	}
	waitFrame("after startScreencast")
	if err := client.Call("Runtime.evaluate", map[string]any{"expression": "document.body.appendChild(document.createElement('div')).textContent = 'mutation'", "returnByValue": true}, nil); err != nil {
		t.Fatal(err)
	}
	waitFrame("after DOM mutation (ack path)")

	if err := client.Call("Runtime.evaluate", map[string]any{"expression": "document.getElementById('f').focus()", "returnByValue": true}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.Call("Input.insertText", map[string]string{"text": "hello-live"}, nil); err != nil {
		t.Fatal(err)
	}
	var input struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := client.Call("Runtime.evaluate", map[string]any{"expression": "document.getElementById('f').value", "returnByValue": true}, &input); err != nil {
		t.Fatal(err)
	}
	if input.Result.Value != "hello-live" {
		t.Fatalf("input injection failed: field value %q", input.Result.Value)
	}
}
