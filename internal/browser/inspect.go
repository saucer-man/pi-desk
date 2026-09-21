package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Inspector owns one page-target connection, never a browser-wide connection.
// Commands use the same STA ownership check as normal embedded browser input.
type Inspector struct {
	client      *Client
	page        *NativePage
	allowed     func() bool
	lifetime    context.Context
	cancel      context.CancelFunc
	closed      atomic.Bool
	op          sync.Mutex
	mu          sync.Mutex
	sequence    uint64
	events      map[string][]inspectEvent
	dropped     map[string]int
	paused      json.RawMessage
	breakpoints map[string]json.RawMessage
	xhr         map[string]bool
	enabled     map[string]bool
}

type inspectEvent struct {
	Sequence uint64          `json:"sequence"`
	Method   string          `json:"method"`
	Params   json.RawMessage `json:"params"`
}

func NewInspector(ctx context.Context, page *NativePage, allowed func() bool) (*Inspector, error) {
	i := &Inspector{page: page, allowed: allowed, events: map[string][]inspectEvent{}, dropped: map[string]int{}, breakpoints: map[string]json.RawMessage{}, xhr: map[string]bool{}, enabled: map[string]bool{}}
	i.lifetime, i.cancel = context.WithCancel(context.Background())
	conn, err := newNativeCDP(ctx, page, allowed)
	if err != nil {
		i.cancel()
		return nil, err
	}
	i.client = &Client{conn: conn, pending: make(map[int64]chan cdpMessage), done: make(chan struct{}), OnEvent: i.event}
	go i.client.read()
	if !allowed() {
		i.Close()
		return nil, errors.New("page control changed")
	}
	return i, nil
}

func (i *Inspector) Active() bool {
	if i.closed.Load() || !i.allowed() {
		return false
	}
	select {
	case <-i.client.Done():
		return false
	default:
		return true
	}
}

// Close never waits on the UI thread. Disable only this connection's domains;
// retained user pages and other conversations' debugging sessions stay alive.
func (i *Inspector) Close() {
	if i == nil || i.closed.Swap(true) {
		return
	}
	i.cancel()
	go func() {
		i.op.Lock()
		defer i.op.Unlock()
		defer i.client.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		for pattern := range i.xhr {
			_ = i.client.call(ctx, "DOMDebugger.removeXHRBreakpoint", map[string]any{"url": pattern}, nil, nil)
		}
		if i.enabled["Debugger"] {
			_ = i.client.call(ctx, "Debugger.disable", map[string]any{}, nil, nil)
		}
	}()
}

func inspectKind(method string) string {
	switch {
	case method == "Debugger.scriptParsed":
		return "scripts"
	case strings.HasPrefix(method, "Network.webSocket"):
		return "websocket"
	case strings.HasPrefix(method, "Network."):
		return "network"
	case method == "Runtime.consoleAPICalled" || method == "Runtime.exceptionThrown" || method == "Log.entryAdded":
		return "console"
	case strings.HasPrefix(method, "Runtime.executionContext"):
		return "contexts"
	}
	return ""
}

func (i *Inspector) event(method string, params json.RawMessage) {
	// The reader may emit events before dialEvents returns the client pointer.
	if i.closed.Load() || !i.allowed() {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if method == "Debugger.paused" {
		i.paused = append(json.RawMessage(nil), params...)
	}
	if method == "Debugger.resumed" {
		i.paused = nil
	}
	if method == "Runtime.executionContextsCleared" {
		i.events["contexts"] = nil
		i.events["scripts"] = nil
		i.paused = nil
	}
	kind := inspectKind(method)
	if kind == "" {
		return
	}
	i.sequence++
	// Bounded metadata, not a HAR archive. Response bodies stay in Chromium and
	// are fetched on demand. Eviction is reported to callers, never silent.
	if len(params) > 256<<10 {
		i.dropped[kind]++
		return
	}
	items := append(i.events[kind], inspectEvent{i.sequence, method, params})
	bytes := 0
	for _, item := range items {
		bytes += len(item.Params)
	}
	for len(items) > 1000 || bytes > 4<<20 {
		bytes -= len(items[0].Params)
		items[0] = inspectEvent{}
		items = items[1:]
		i.dropped[kind]++
	}
	i.events[kind] = items
}

func (i *Inspector) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(i.lifetime, cancel)
	defer stop()
	defer cancel()
	var result json.RawMessage
	err := i.client.call(ctx, method, params, &result, func(send func() error) error {
		return i.page.DispatchCDP(ctx, method, i.Active, send)
	})
	if !i.Active() {
		return nil, errors.New("browser connection expired; request a new connection")
	}
	return result, err
}

func (i *Inspector) enable(ctx context.Context, domain string) error {
	if i.enabled[domain] {
		return nil
	}
	params := map[string]any{}
	if domain == "Network" {
		params = map[string]any{"maxTotalBufferSize": 8 << 20, "maxResourceBufferSize": 2 << 20, "maxPostDataSize": 64 << 10}
	}
	if _, err := i.call(ctx, domain+".enable", params); err != nil {
		return err
	}
	i.enabled[domain] = true
	return nil
}

// Request is an internal broker action, not a general CDP escape hatch.
func (i *Inspector) Request(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	i.op.Lock()
	defer i.op.Unlock()
	if !i.Active() {
		return nil, errors.New("debug connection expired; request a new connection")
	}
	select {
	case <-i.client.Done():
		return nil, errors.New("debug connection closed; start a new capture")
	default:
	}
	var p map[string]any
	if json.Unmarshal(raw, &p) != nil || p == nil {
		return nil, errors.New("invalid inspection parameters")
	}
	switch method {
	case "start":
		kind, _ := p["kind"].(string)
		domains := map[string][]string{"scripts": {"Runtime", "Debugger"}, "debug": {"Runtime", "Debugger"}, "network": {"Network"}, "websocket": {"Network"}, "console": {"Runtime", "Log"}, "contexts": {"Runtime"}}
		selected, ok := domains[kind]
		if !ok {
			return nil, errors.New("invalid capture kind")
		}
		for _, domain := range selected {
			if err := i.enable(ctx, domain); err != nil {
				return nil, err
			}
		}
		return map[string]any{"started": kind, "note": "Capture starts now, not retroactively. Trigger/reload the page AFTER starting capture. Buffers are bounded."}, nil
	case "events", "clear":
		var q struct {
			Kind      string `json:"kind"`
			Since     uint64 `json:"since"`
			Limit     int    `json:"limit"`
			Filter    string `json:"filter"`
			RequestID string `json:"requestId"`
		}
		if json.Unmarshal(raw, &q) != nil {
			return nil, errors.New("invalid event query")
		}
		switch q.Kind {
		case "scripts", "network", "websocket", "console", "contexts":
		default:
			return nil, errors.New("invalid capture kind")
		}
		if q.Limit <= 0 || q.Limit > 100 {
			q.Limit = 100
		}
		i.mu.Lock()
		defer i.mu.Unlock()
		if method == "clear" {
			i.events[q.Kind] = nil
			i.dropped[q.Kind] = 0
			return map[string]any{"cleared": q.Kind, "cursor": i.sequence}, nil
		}
		items := []inspectEvent{}
		cursor := q.Since
		more := false
		for _, event := range i.events[q.Kind] {
			if event.Sequence <= q.Since {
				continue
			}
			if len(items) >= q.Limit {
				more = true
				break
			}
			cursor = event.Sequence
			if q.Filter != "" && !strings.Contains(strings.ToLower(string(event.Params)), strings.ToLower(q.Filter)) {
				continue
			}
			if q.RequestID != "" {
				var request struct {
					ID string `json:"requestId"`
				}
				_ = json.Unmarshal(event.Params, &request)
				if request.ID != q.RequestID {
					continue
				}
			}
			items = append(items, event)
		}
		return map[string]any{"events": items, "cursor": cursor, "hasMore": more, "dropped": i.dropped[q.Kind]}, nil
	case "state":
		i.mu.Lock()
		defer i.mu.Unlock()
		data, err := json.Marshal(map[string]any{"paused": i.paused, "breakpoints": i.breakpoints, "xhrBreakpoints": i.xhr})
		return json.RawMessage(data), err
	}
	// Only page-local debugging/inspection; lifecycle, global cache and cookies
	// deliberately remain unavailable on this connection.
	domain := ""
	switch method {
	case "Debugger.getScriptSource", "Debugger.searchInContent", "Debugger.setBreakpoint", "Debugger.setBreakpointByUrl", "Debugger.removeBreakpoint", "Debugger.pause", "Debugger.resume", "Debugger.stepOver", "Debugger.stepInto", "Debugger.stepOut", "Debugger.evaluateOnCallFrame", "Debugger.setPauseOnExceptions":
		domain = "Debugger"
	case "DOMDebugger.setXHRBreakpoint", "DOMDebugger.removeXHRBreakpoint":
		domain = "Debugger"
	case "Network.getResponseBody", "Network.getRequestPostData":
		domain = "Network"
	case "Runtime.evaluate", "Runtime.getProperties", "Runtime.releaseObject", "Runtime.releaseObjectGroup":
		domain = "Runtime"
	case "Page.getFrameTree":
	default:
		return nil, fmt.Errorf("unsupported inspection method: %s", method)
	}
	if domain != "" {
		if err := i.enable(ctx, domain); err != nil {
			return nil, err
		}
	}
	result, err := i.call(ctx, method, p)
	if err != nil {
		return nil, err
	}
	switch method {
	case "Debugger.setBreakpoint", "Debugger.setBreakpointByUrl":
		var value struct {
			ID string `json:"breakpointId"`
		}
		if json.Unmarshal(result, &value) == nil && value.ID != "" {
			i.breakpoints[value.ID] = append(json.RawMessage(nil), raw...)
		}
	case "Debugger.removeBreakpoint":
		delete(i.breakpoints, fmt.Sprint(p["breakpointId"]))
	case "DOMDebugger.setXHRBreakpoint":
		i.xhr[fmt.Sprint(p["url"])] = true
	case "DOMDebugger.removeXHRBreakpoint":
		delete(i.xhr, fmt.Sprint(p["url"]))
	}
	return result, nil
}
