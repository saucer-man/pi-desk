//go:build windows

package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/wailsapp/go-webview2/webviewloader"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
)

// WebView2 interfaces are COM ABI vtables. Only the slots used here are bound;
// pages never load the Wails runtime or host objects. Only this dedicated
// environment uses native CDP; no raw remote-debugging listener is exposed.
// All interface access, including destruction, is confined to Wails' STA thread.
func comCall(object uintptr, slot int, args ...uintptr) error {
	if object == 0 {
		return errors.New("browser page is closed")
	}
	vt := *(*uintptr)(unsafe.Pointer(object))
	method := *(*uintptr)(unsafe.Pointer(vt + uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	hr, _, _ := syscall.SyscallN(method, append([]uintptr{object}, args...)...)
	if int32(hr) < 0 {
		return fmt.Errorf("WebView2 HRESULT 0x%08x", uint32(hr))
	}
	return nil
}

type nativeCallback struct {
	vt   *[4]uintptr
	refs atomic.Int32
	fn   func(uintptr, uintptr)
}

var callbacks sync.Map // COM retains pointers; keep Go callbacks rooted until Release.
var callbackVTable = [4]uintptr{
	windows.NewCallback(func(this, iid, out uintptr) uintptr {
		if out == 0 {
			return 0x80004003
		}
		*(*uintptr)(unsafe.Pointer(out)) = 0
		// Callbacks implement IUnknown and their single Invoke interface. WebView2
		// consumes the interface supplied at registration without other interfaces.
		unknown := windows.GUID{Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
		if *(*windows.GUID)(unsafe.Pointer(iid)) != unknown {
			return 0x80004002
		}
		*(*uintptr)(unsafe.Pointer(out)) = this
		(*nativeCallback)(unsafe.Pointer(this)).refs.Add(1)
		return 0
	}),
	windows.NewCallback(func(this uintptr) uintptr { return uintptr((*nativeCallback)(unsafe.Pointer(this)).refs.Add(1)) }),
	windows.NewCallback(func(this uintptr) uintptr {
		n := (*nativeCallback)(unsafe.Pointer(this)).refs.Add(-1)
		if n == 0 {
			callbacks.Delete(this)
		}
		return uintptr(n)
	}),
	windows.NewCallback(func(this, a, b uintptr) uintptr { (*nativeCallback)(unsafe.Pointer(this)).fn(a, b); return 0 }),
}

func callback(fn func(uintptr, uintptr)) *nativeCallback {
	c := &nativeCallback{vt: &callbackVTable, fn: fn}
	c.refs.Store(1)
	callbacks.Store(uintptr(unsafe.Pointer(c)), c)
	return c
}
func (c *nativeCallback) ptr() uintptr { return uintptr(unsafe.Pointer(c)) }
func (c *nativeCallback) release() {
	if c.refs.Add(-1) == 0 {
		callbacks.Delete(c.ptr())
	}
}
func comString(object uintptr, slot int) string {
	var value *uint16
	if comCall(object, slot, uintptr(unsafe.Pointer(&value))) != nil {
		return ""
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(value))
	return windows.UTF16PtrToString(value)
}
func comBool(object uintptr, slot int) bool {
	var value int32
	return comCall(object, slot, uintptr(unsafe.Pointer(&value))) == nil && value != 0
}

type NativeState struct {
	URL          string `json:"url"`
	Title        string `json:"title"`
	Loading      bool   `json:"loading"`
	CanGoBack    bool   `json:"canGoBack"`
	CanGoForward bool   `json:"canGoForward"`
	Error        string `json:"error,omitempty"`
}
type NativeHost struct {
	hwnd         func() uintptr
	profile      string
	environment  uintptr
	initializing bool
	waiting      []func(error)
}
type NativePage struct {
	host             *NativeHost
	container        w32.HWND
	controller, core uintptr
	loading          bool
	changed          func(NativeState)
}

func NewNativeHost(hwnd func() uintptr, profile string) *NativeHost {
	return &NativeHost{hwnd: hwnd, profile: profile}
}
func (h *NativeHost) EnvironmentCompleted(code webviewloader.HRESULT, environment *webviewloader.ICoreWebView2Environment) webviewloader.HRESULT {
	var err error
	if code < 0 || environment == nil {
		err = fmt.Errorf("create browser environment: 0x%08x", uint32(code))
	} else {
		h.environment = uintptr(unsafe.Pointer(environment))
		_ = comCall(h.environment, 1)
	}
	h.initializing = false
	waiters := h.waiting
	h.waiting = nil
	for _, done := range waiters {
		done(err)
	}
	return 0
}
func (h *NativeHost) ready(done func(error)) {
	if h.environment != 0 {
		done(nil)
		return
	}
	h.waiting = append(h.waiting, done)
	if h.initializing {
		return
	}
	h.initializing = true
	err := webviewloader.CreateCoreWebView2EnvironmentWithOptions(h, webviewloader.WithUserDataFolder(h.profile))
	if err != nil {
		h.initializing = false
		waiters := h.waiting
		h.waiting = nil
		for _, notify := range waiters {
			notify(err)
		}
	}
}
func (h *NativeHost) NewPage(ctx context.Context, changed func(NativeState)) (*NativePage, error) {
	type result struct {
		page *NativePage
		err  error
	}
	results := make(chan result, 1)
	application.InvokeAsync(func() {
		h.ready(func(err error) {
			if err != nil {
				results <- result{err: err}
				return
			}
			if ctx.Err() != nil {
				results <- result{err: ctx.Err()}
				return
			}
			parent := h.hwnd()
			if parent == 0 {
				results <- result{err: errors.New("main window is not ready")}
				return
			}
			// Give each page its own sibling surface above the full-window Wails
			// WebView. Sharing its parent directly leaves Chromium's child behind it.
			class, _ := windows.UTF16PtrFromString("STATIC")
			container := w32.CreateWindowEx(0, class, nil, w32.WS_CHILD|w32.WS_CLIPCHILDREN|w32.WS_CLIPSIBLINGS, 0, 0, 1000, 700, w32.HWND(parent), 0, 0, nil)
			if container == 0 {
				results <- result{err: errors.New("create browser host window failed")}
				return
			}
			cb := callback(func(code, controller uintptr) {
				if int32(code) < 0 || controller == 0 {
					w32.DestroyWindow(container)
					results <- result{err: fmt.Errorf("create browser controller: 0x%08x", uint32(code))}
					return
				}
				_ = comCall(controller, 1)
				p := &NativePage{host: h, container: container, controller: controller, changed: changed}
				err := comCall(controller, 25, uintptr(unsafe.Pointer(&p.core)))
				if err == nil {
					err = p.configure()
				}
				if ctx.Err() != nil {
					err = ctx.Err()
				}
				if err != nil {
					p.close()
					results <- result{err: err}
					return
				}
				results <- result{page: p}
			})
			err = comCall(h.environment, 3, uintptr(container), cb.ptr())
			cb.release()
			if err != nil {
				w32.DestroyWindow(container)
				results <- result{err: err}
			}
		})
	})
	select {
	case result := <-results:
		return result.page, result.err
	case <-ctx.Done():
		go func() {
			result := <-results
			if result.page != nil {
				result.page.Close()
			}
		}()
		return nil, ctx.Err()
	}
}
func (p *NativePage) event(slot int, fn func(uintptr, uintptr)) error {
	cb := callback(fn)
	defer cb.release()
	var token int64
	return comCall(p.core, slot, cb.ptr(), uintptr(unsafe.Pointer(&token)))
}
func (p *NativePage) configure() error {
	if err := comCall(p.controller, 4, 0); err != nil {
		return err
	}
	// A stable offscreen viewport permits background Agent work without showing
	// an external window or changing the currently visible inspector tab.
	initialBounds := [4]int32{0, 0, 1000, 700}
	if err := comCall(p.controller, 6, uintptr(unsafe.Pointer(&initialBounds))); err != nil {
		return err
	}
	var settings uintptr
	if err := comCall(p.core, 3, uintptr(unsafe.Pointer(&settings))); err != nil {
		return err
	}
	for _, slot := range []int{6, 10, 12, 16} {
		_ = comCall(settings, slot, 0)
	} // web messages, status, DevTools, host objects
	_ = comCall(settings, 2)
	for _, slot := range []int{11, 13, 46} {
		if err := p.event(slot, func(_, _ uintptr) { p.notify("") }); err != nil {
			return err
		}
	}
	if err := p.event(7, func(_, args uintptr) {
		uri := comString(args, 3)
		if !ValidPageURL(uri) {
			_ = comCall(args, 8, 1)
			p.notify("不支持此网页地址；不会在外部窗口打开。")
			return
		}
		p.loading = true
		p.notify("")
	}); err != nil {
		return err
	}
	if err := p.event(15, func(_, args uintptr) {
		p.loading = false
		if !comBool(args, 3) {
			p.notify("页面加载失败，可检查地址后重试。")
		} else {
			p.notify("")
		}
	}); err != nil {
		return err
	}
	if err := p.event(25, func(_, _ uintptr) { p.notify("网页进程异常，请关闭此标签后重新打开。") }); err != nil {
		return err
	}
	return p.event(44, func(_, args uintptr) {
		_ = comCall(args, 6, 1) // Handled: never let WebView2 spawn a top-level window.
		uri := comString(args, 3)
		if !ValidPageURL(uri) {
			p.notify("此登录或新窗口地址暂不支持，未打开外部窗口。")
			return
		}
		// Website links stay in their originating tab. Queue navigation after
		// this event returns; never block WebView2's STA waiting on onUI.
		application.InvokeAsync(func() {
			if p.core != 0 {
				if err := p.navigate(uri); err != nil {
					p.notify("无法打开网页：" + err.Error())
				}
			}
		})
	})
}
func (p *NativePage) notify(message string) {
	if p.core != 0 && p.changed != nil {
		p.changed(NativeState{URL: comString(p.core, 4), Title: comString(p.core, 48), Loading: p.loading, CanGoBack: comBool(p.core, 38), CanGoForward: comBool(p.core, 39), Error: message})
	}
}
func (p *NativePage) bounds(x, y, width, height int32, visible bool) error {
	if !visible {
		w32.ShowWindow(p.container, w32.SW_HIDE)
		return comCall(p.controller, 4, 0)
	}
	if p.container == 0 || !w32.SetWindowPos(p.container, w32.HWND_TOP, int(x), int(y), int(width), int(height), w32.SWP_NOACTIVATE|w32.SWP_SHOWWINDOW) {
		return errors.New("position browser host window failed")
	}
	rect := [4]int32{0, 0, width, height}
	if err := comCall(p.controller, 6, uintptr(unsafe.Pointer(&rect))); err != nil {
		return err
	}
	_ = comCall(p.controller, 23) // NotifyParentWindowPositionChanged: IME/menu placement follows the host.
	var show uintptr
	if visible {
		show = 1
	}
	return comCall(p.controller, 4, show)
}
func (p *NativePage) Bounds(ctx context.Context, x, y, width, height int32, visible bool) error {
	return p.onUI(ctx, func() error { return p.bounds(x, y, width, height, visible) })
}
func (p *NativePage) onUI(ctx context.Context, fn func() error) error {
	done := make(chan error, 1)
	application.InvokeAsync(func() {
		if ctx.Err() != nil {
			done <- ctx.Err()
			return
		}
		done <- fn()
	})
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (p *NativePage) Navigate(ctx context.Context, url string) error {
	return p.onUI(ctx, func() error { return p.navigate(url) })
}
func (p *NativePage) navigate(url string) error {
	if !ValidPageURL(url) {
		return errors.New("only http, https and about:blank are supported")
	}
	value, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	err = comCall(p.core, 5, uintptr(unsafe.Pointer(value)))
	runtime.KeepAlive(value)
	return err
}
func (p *NativePage) Command(ctx context.Context, command string) error {
	slot, ok := map[string]int{"back": 40, "forward": 41, "reload": 31, "stop": 43, "devtools": 51}[command]
	if !ok {
		return errors.New("invalid browser command")
	}
	return p.onUI(ctx, func() error { return comCall(p.core, slot) })
}
func (p *NativePage) Call(ctx context.Context, method string, params json.RawMessage, allowed func() bool) (json.RawMessage, error) {
	type result struct {
		data json.RawMessage
		err  error
	}
	done := make(chan result, 1)
	application.InvokeAsync(func() {
		if allowed != nil && !allowed() {
			done <- result{err: errors.New("browser connection expired; request a new connection")}
			return
		}
		// Hidden WebView2 surfaces do not complete screenshot capture. Never reveal
		// the page automatically: background work must not cover the user's panel.
		if method == "Page.captureScreenshot" && !comBool(p.controller, 3) {
			done <- result{err: errors.New("browser tab is hidden; open this tab for screenshots, or use browser_read and element refs in the background")}
			return
		}
		if ctx.Err() != nil {
			done <- result{err: ctx.Err()}
			return
		}
		name, err := windows.UTF16PtrFromString(method)
		if err != nil {
			done <- result{err: err}
			return
		}
		payload, err := windows.UTF16PtrFromString(string(params))
		if err != nil {
			done <- result{err: err}
			return
		}
		cb := callback(func(code, value uintptr) {
			if int32(code) < 0 {
				done <- result{err: fmt.Errorf("browser command failed: 0x%08x", uint32(code))}
				return
			}
			done <- result{data: json.RawMessage(windows.UTF16PtrToString((*uint16)(unsafe.Pointer(value))))}
		})
		err = comCall(p.core, 36, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(payload)), cb.ptr())
		cb.release()
		runtime.KeepAlive(name)
		runtime.KeepAlive(payload)
		if err != nil {
			done <- result{err: err}
		}
	})
	select {
	case r := <-done:
		return r.data, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (p *NativePage) close() {
	if p.controller != 0 {
		_ = comCall(p.controller, 24)
		_ = comCall(p.controller, 2)
		p.controller = 0
	}
	if p.core != 0 {
		_ = comCall(p.core, 2)
		p.core = 0
	}
	if p.container != 0 {
		w32.DestroyWindow(p.container)
		p.container = 0
	}
}
func (p *NativePage) Close() { application.InvokeAsync(p.close) }

// Resolve identity from this COM page, never from the first /json/list result.
func (p *NativePage) CDPTarget(ctx context.Context) (string, error) {
	data, err := p.Call(ctx, "Target.getTargetInfo", json.RawMessage(`{}`), nil)
	if err != nil {
		return "", err
	}
	var result struct {
		TargetInfo struct {
			TargetID string `json:"targetId"`
		} `json:"targetInfo"`
	}
	if json.Unmarshal(data, &result) != nil || result.TargetInfo.TargetID == "" {
		return "", errors.New("WebView2 did not return its CDP target")
	}
	return result.TargetInfo.TargetID, nil
}

// Subscribe through WebView2, not a remote-debugging socket. The callback must
// not block the STA. The returned unsubscribe can be called from any goroutine.
func (p *NativePage) cdpEvent(ctx context.Context, name string, event func(json.RawMessage)) (func(), error) {
	var receiver uintptr
	var token int64
	var once sync.Once
	stop := func() {
		once.Do(func() {
			application.InvokeAsync(func() {
				if receiver != 0 {
					_ = comCall(receiver, 4, uintptr(token))
					_ = comCall(receiver, 2)
				}
			})
		})
	}
	err := p.onUI(ctx, func() error {
		name16, err := windows.UTF16PtrFromString(name)
		if err != nil {
			return err
		}
		err = comCall(p.core, 42, uintptr(unsafe.Pointer(name16)), uintptr(unsafe.Pointer(&receiver)))
		runtime.KeepAlive(name16)
		if err != nil {
			return err
		}
		cb := callback(func(_, args uintptr) { event(json.RawMessage(comString(args, 3))) })
		defer cb.release()
		return comCall(receiver, 3, cb.ptr(), uintptr(unsafe.Pointer(&token)))
	})
	if err != nil {
		stop()
		return nil, err
	}
	return stop, nil
}

// Recheck the connection lifetime on the STA before dispatching queued commands.
func (p *NativePage) DispatchCDP(ctx context.Context, method string, allowed func() bool, send func() error) error {
	return p.onUI(ctx, func() error {
		if p.core == 0 || !allowed() {
			return errors.New("browser connection expired; request a new connection")
		}
		if method == "Page.captureScreenshot" && !comBool(p.controller, 3) {
			return errors.New("browser tab is hidden; open this tab for screenshots")
		}
		return send()
	})
}
