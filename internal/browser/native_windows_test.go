//go:build windows

package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/gorilla/websocket"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
)

// This tests real COM creation, navigation and CDP on a hidden native window.
// It does not replace the separate mouse/IME/visual acceptance checks.
func TestNativeWebViewIntegration(t *testing.T) {
	if os.Getenv("PI_DESK_NATIVE_TEST") != "1" {
		t.Skip("set PI_DESK_NATIVE_TEST=1 for real WebView2 integration")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, 2); err != nil {
		t.Fatal(err)
	}
	defer windows.CoUninitialize()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/frame" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<input id="nested"><button onclick="fetch('/api')">Frame fetch</button>`)
			return
		}
		if r.URL.Path == "/probe.js" {
			w.Header().Set("Content-Type", "application/javascript")
			fmt.Fprint(w, "function inspectProbe(value) {\n  const answer = value + 1;\n  return answer;\n}\n")
			return
		}
		if r.URL.Path == "/ws" {
			upgrader := websocket.Upgrader{}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			kind, data, err := conn.ReadMessage()
			if err == nil {
				_ = conn.WriteMessage(kind, data)
			}
			return
		}
		if r.URL.Path == "/api" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true}`)
			return
		}
		fmt.Fprint(w, `<!doctype html><title>Embedded test</title><input id="field"><a target="_blank" href="/child">Child</a><p>Native page</p>`)
		fmt.Fprint(w, `<script src="/probe.js"></script>`)
		fmt.Fprint(w, `<select id="choice"><option value="a">Alpha</option><option value="b">Beta</option></select><button id="load" onclick="fetch('/api').then(r=>r.json()).then(v=>{document.querySelector('#out').textContent=JSON.stringify(v);console.log('network done')})">Load</button><output id="out"></output>`)
		fmt.Fprintf(w, "<p>Path: %s end</p>", r.URL.Path)
	}))
	defer server.Close()
	app := application.New(application.Options{Name: "Pi Desk native test"})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{Title: "Pi Desk native test", Hidden: true, HTML: "<html><body>Native integration test</body></html>"})
	host := NewNativeHost(func() uintptr { return uintptr(window.NativeWindow()) }, t.TempDir())
	results := make(chan error, 1)
	go func() {
		// Wait for the application message pump and native parent to initialise.
		time.Sleep(time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		defer app.Quit()
		page, err := host.NewPage(ctx, nil)
		if err != nil {
			results <- err
			return
		}
		defer page.Close()
		targetID, e := page.CDPTarget(ctx)
		if e != nil {
			results <- e
			return
		}
		t.Logf("native CDP target resolved without remote port: %s", targetID)
		if _, _, err := ReadPortFile(filepath.Join(host.profile, "EBWebView")); err == nil {
			results <- fmt.Errorf("embedded WebView exposed a raw debugging port")
			return
		}
		if err = page.Navigate(ctx, server.URL); err != nil {
			results <- err
			return
		}
		for ctx.Err() == nil {
			result, e := page.Call(ctx, "Runtime.evaluate", json.RawMessage(`{"expression":"document.title","returnByValue":true}`), nil)
			if e != nil {
				results <- e
				return
			}
			var parsed struct {
				Result struct {
					Value string `json:"value"`
				} `json:"result"`
			}
			_ = json.Unmarshal(result, &parsed)
			if parsed.Result.Value == "Embedded test" {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if ctx.Err() != nil {
			results <- ctx.Err()
			return
		}
		if err = page.Bounds(ctx, 10, 30, 600, 400, true); err != nil {
			results <- err
			return
		}
		if err = page.onUI(ctx, func() error {
			if w32.GetWindow(w32.HWND(host.hwnd()), w32.GW_CHILD) != page.container {
				return fmt.Errorf("browser container is behind the Wails surface")
			}
			var bounds [4]int32
			if err := comCall(page.controller, 5, uintptr(unsafe.Pointer(&bounds))); err != nil {
				return err
			}
			if bounds != [4]int32{0, 0, 600, 400} {
				return fmt.Errorf("page bounds must be container-relative: %v", bounds)
			}
			return nil
		}); err != nil {
			results <- err
			return
		}
		if err = page.Bounds(ctx, 0, 0, 0, 0, false); err != nil {
			results <- err
			return
		}
		result, err := page.Call(ctx, "Runtime.evaluate", json.RawMessage(`{"expression":"innerWidth>0 && innerHeight>0 && typeof window._wails==='undefined' && typeof window.go==='undefined'","returnByValue":true}`), nil)
		if err != nil {
			results <- err
			return
		}
		var value struct {
			Result struct {
				Value bool `json:"value"`
			} `json:"result"`
		}
		if json.Unmarshal(result, &value) != nil || !value.Result.Value {
			results <- fmt.Errorf("viewport/host isolation check failed: %s", result)
			return
		}
		result, err = page.Call(ctx, "Runtime.evaluate", json.RawMessage(`{"expression":"navigator.webdriver === false","returnByValue":true}`), nil)
		value.Result.Value = false
		if err != nil || json.Unmarshal(result, &value) != nil || !value.Result.Value {
			results <- fmt.Errorf("unexpected webdriver automation flag: %s, %v", result, err)
			return
		}
		t.Log("navigator.webdriver is false; persistent profile uses no raw CDP port")
		_, err = page.Call(ctx, "Input.insertText", json.RawMessage(`{"text":"forbidden"}`), func() bool { return false })
		if err == nil {
			results <- fmt.Errorf("takeover guard allowed queued input")
			return
		}
		_, err = page.Call(ctx, "Runtime.evaluate", json.RawMessage(`{"expression":"document.querySelector('#field').focus()"}`), nil)
		if err != nil {
			results <- err
			return
		}
		_, err = page.Call(ctx, "Input.insertText", json.RawMessage(`{"text":"中文 native input"}`), nil)
		if err != nil {
			results <- err
			return
		}
		result, err = page.Call(ctx, "Runtime.evaluate", json.RawMessage(`{"expression":"document.querySelector('#field').value === '中文 native input'","returnByValue":true}`), nil)
		value.Result.Value = false
		_ = json.Unmarshal(result, &value)
		if err != nil || !value.Result.Value {
			results <- fmt.Errorf("same-page text input failed: %s, %v", result, err)
			return
		}
		waitPath := func(path string) error {
			waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			params, _ := json.Marshal(map[string]any{"expression": fmt.Sprintf("location.pathname === %q && document.readyState === 'complete'", path), "returnByValue": true})
			for waitCtx.Err() == nil {
				result, e := page.Call(waitCtx, "Runtime.evaluate", params, nil)
				value.Result.Value = false
				if e == nil && json.Unmarshal(result, &value) == nil && value.Result.Value {
					return nil
				}
				time.Sleep(50 * time.Millisecond)
			}
			return fmt.Errorf("original tab did not navigate to %s: %w", path, waitCtx.Err())
		}
		for _, expression := range []string{"document.querySelector('a').click()", "window.open('/child','_blank')"} {
			params, _ := json.Marshal(map[string]any{"expression": expression, "userGesture": true})
			_, err = page.Call(ctx, "Runtime.evaluate", params, nil)
			if err == nil {
				err = waitPath("/child")
			}
			if err == nil {
				currentID, e := page.CDPTarget(ctx)
				if e != nil || currentID != targetID {
					err = fmt.Errorf("link changed browser target: %s, %v", currentID, e)
				}
			}
			for _, step := range []struct{ command, path string }{{"back", "/"}, {"forward", "/child"}, {"back", "/"}} {
				if err == nil {
					err = page.Command(ctx, step.command)
				}
				if err == nil {
					err = waitPath(step.path)
				}
			}
			if err != nil {
				results <- fmt.Errorf("same-tab navigation (%s): %w", expression, err)
				return
			}
		}
		if err == nil && (os.Getenv("PI_DESK_EXTENSION_TEST") == "1" || os.Getenv("PI_DESK_PLAYWRIGHT_CLI") != "") {
			var taken atomic.Bool
			inspector, inspectErr := NewInspector(ctx, page, func() bool { return !taken.Load() })
			if inspectErr != nil {
				results <- inspectErr
				return
			}
			defer inspector.Close()
			stop, e := ServeAgent(func(ctx context.Context, c AgentCommand) (any, error) {
				if c.TabID != "native-test" || c.ThreadID != "native-test" {
					return nil, fmt.Errorf("wrong test tab")
				}
				if c.Action == "ensure" {
					return map[string]string{"tabId": c.TabID}, nil
				}
				if c.Action == "inspect" {
					if c.Method == "stop" {
						inspector.Close()
						return true, nil
					}
					return inspector.Request(ctx, c.Method, c.Params)
				}
				if c.Action == "connect" {
					return &CDPAccess{Page: page, Allowed: func() bool { return !taken.Load() }}, nil
				}
				if c.Action != "call" {
					return nil, fmt.Errorf("unexpected test action")
				}
				if e := ValidateAgentCommand(c); e != nil {
					return nil, e
				}
				return page.Call(ctx, c.Method, c.Params, nil)
			})
			if e != nil {
				results <- e
				return
			}
			defer stop()
			if cli := os.Getenv("PI_DESK_PLAYWRIGHT_CLI"); cli != "" {
				application.InvokeAsync(func() { window.Show() })
				if e = page.Bounds(ctx, 0, 0, 900, 600, true); e != nil {
					results <- e
					return
				}
				dir := t.TempDir()
				run := func(args ...string) (string, error) {
					command := exec.CommandContext(ctx, "node", append([]string{cli, fmt.Sprintf("-s=pi-desk-native-test-%d", os.Getpid())}, args...)...)
					command.Dir = dir
					command.Env = append(os.Environ(), "NO_UPDATE_NOTIFIER=1")
					out, e := command.CombinedOutput()
					t.Logf("CLI %s: %s", args[0], out)
					if e == nil && strings.Contains(string(out), "### Error") {
						e = fmt.Errorf("CLI returned an error")
					}
					return string(out), e
				}
				defer func() { _, _ = run("detach") }()
				_, err = run("attach", "--cdp="+AgentCDPURL("native-test", "native-test", 0))
				if err == nil {
					_, err = run("run-code", `async page => { await page.locator('#field').fill('Playwright 中文'); await page.locator('#choice').selectOption('b'); await page.locator('#load').hover(); const response=page.waitForResponse(r=>r.url().endsWith('/api')); await page.locator('#load').click(); if (!(await (await response).json()).ok) throw Error('body'); if (await page.locator('#field').inputValue() !== 'Playwright 中文' || await page.locator('#choice').inputValue() !== 'b') throw Error('input/select'); console.log('PW_NATIVE_PASS'); }`)
				}
				if err == nil {
					var out string
					out, err = run("requests")
					if err == nil && !strings.Contains(out, "/api") {
						err = fmt.Errorf("network request not recorded")
					}
				}
				if err == nil {
					_, err = run("request", "1")
				}
				if err == nil {
					_, err = run("console")
				}
				if err == nil {
					_, err = run("run-code", `async page => { if(page.context().pages().length !== 1) throw Error('other tab leaked'); let blocked=false; try { await page.context().newCDPSession(page); } catch(e) { blocked=String(e).includes('blocked'); } if(!blocked) throw Error('browser-wide session allowed'); }`)
				}
				if err == nil {
					var out string
					out, err = run("response-body", "1")
					if err == nil && !strings.Contains(out, `"ok":true`) && !strings.Contains(out, `"ok": true`) {
						err = fmt.Errorf("response body missing: %s", out)
					}
				}
				if err == nil {
					_, err = run("run-code", `async page => { if(await page.evaluate(()=>navigator.webdriver)) throw Error('webdriver enabled'); await page.evaluate(()=>{const f=document.createElement('iframe'); f.src=location.origin.replace('127.0.0.1','localhost')+'/frame'; document.body.append(f);}); await page.frameLocator('iframe').locator('#nested').fill('跨域 iframe'); if(await page.frameLocator('iframe').locator('#nested').inputValue() !== '跨域 iframe') throw Error('iframe routing'); }`)
				}
				if err == nil {
					taken.Store(true)
					_, e = run("run-code", `async page => { await page.locator('#field').fill('must not run'); }`)
					if e == nil {
						err = fmt.Errorf("takeover allowed Playwright input")
					}
				}
				if err != nil {
					results <- err
					return
				}
			}
			if os.Getenv("PI_DESK_EXTENSION_TEST") == "1" {
				if e = page.Bounds(ctx, 0, 0, 0, 0, false); e != nil {
					results <- e
					return
				}
				command := exec.CommandContext(ctx, "node", "node_modules/vitest/vitest.mjs", "run", "--configLoader", "native", "--pool", "threads", "src/services/browser.test.ts")
				command.Dir = filepath.Join("..", "..", "frontend")
				command.Env = append(os.Environ(), AgentEnvironment("native-test")...)
				command.Env = append(command.Env, "PI_DESK_BROWSER_NATIVE_TEST=1")
				output, e := command.CombinedOutput()
				t.Log(string(output))
				if e != nil {
					err = fmt.Errorf("native extension integration: %w", e)
				}
				if err == nil {
					var revoked atomic.Bool
					debugger, e := NewInspector(ctx, page, func() bool { return !revoked.Load() })
					if e != nil {
						results <- e
						return
					}
					defer debugger.Close()
					_, e = debugger.Request(ctx, "start", json.RawMessage(`{"kind":"debug"}`))
					if e == nil {
						_, e = debugger.Request(ctx, "Runtime.evaluate", json.RawMessage(`{"expression":"setTimeout(()=>{debugger;},30)","returnByValue":true}`))
					}
					for n := 0; e == nil && n < 100; n++ {
						debugger.mu.Lock()
						paused := debugger.paused != nil
						debugger.mu.Unlock()
						if paused {
							break
						}
						time.Sleep(20 * time.Millisecond)
					}
					debugger.mu.Lock()
					paused := debugger.paused != nil
					debugger.mu.Unlock()
					if e != nil || !paused {
						results <- fmt.Errorf("takeover setup did not pause: %v", e)
						return
					}
					revoked.Store(true)
					debugger.Close()
					if _, e = debugger.Request(ctx, "state", json.RawMessage(`{}`)); e == nil {
						results <- fmt.Errorf("inspection survived takeover")
						return
					}
					resumeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					_, e = page.Call(resumeCtx, "Runtime.evaluate", json.RawMessage(`{"expression":"1+1","returnByValue":true}`), nil)
					cancel()
					if e != nil {
						err = fmt.Errorf("takeover left retained page paused: %w", e)
					}
				}
			}
		}
		t.Log("native creation, hidden viewport retention, host isolation, Unicode input, connection guard, same-tab links/window.open and back/forward checked")
		results <- err
	}()
	if err := app.Run(); err != nil {
		t.Fatal(err)
	}
	if err := <-results; err != nil {
		t.Fatal(err)
	}
}
