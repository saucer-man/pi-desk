package browser

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// CDPAccess is resolved by the host after checking conversation, tab and epoch.
// Native WebView2 CDP stays inside the host. This is an application control
// boundary, not an OS sandbox against other processes running as the same user.
type CDPAccess struct {
	Page    *NativePage
	Allowed func() bool
}

func cdpKey(secret []byte, query url.Values) string {
	mac := hmac.New(sha256.New, secret)
	for _, name := range []string{"threadId", "tabId", "epoch"} {
		mac.Write([]byte(name + "=" + url.QueryEscape(query.Get(name)) + "&"))
	}
	return hex.EncodeToString(mac.Sum(nil))
}

func AgentCDPURL(thread, tab string, epoch uint64) string {
	broker.RLock()
	defer broker.RUnlock()
	if broker.address == "" {
		return ""
	}
	query := url.Values{"threadId": {thread}, "tabId": {tab}, "epoch": {strconv.FormatUint(epoch, 10)}}
	query.Set("key", cdpKey(broker.secret, query))
	return "ws" + strings.TrimPrefix(broker.address, "http") + "/cdp?" + query.Encode()
}

func serveCDP(ctx context.Context, w http.ResponseWriter, r *http.Request, secret []byte, handler func(context.Context, AgentCommand) (any, error)) {
	query := r.URL.Query()
	provided, err := hex.DecodeString(query.Get("key"))
	expected, _ := hex.DecodeString(cdpKey(secret, query))
	epoch, epochErr := strconv.ParseUint(query.Get("epoch"), 10, 64)
	if r.Method != "GET" || r.Header.Get("Origin") != "" || err != nil || epochErr != nil || !hmac.Equal(provided, expected) {
		http.Error(w, "forbidden", 403)
		return
	}
	result, err := handler(r.Context(), AgentCommand{Action: "connect", ThreadID: query.Get("threadId"), TabID: query.Get("tabId"), Params: mustJSON(map[string]uint64{"epoch": epoch})})
	access, ok := result.(*CDPAccess)
	if err != nil || !ok || access == nil || !access.Allowed() {
		http.Error(w, "browser connection expired; request a new connection", 409)
		return
	}
	setup, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	target, err := access.Page.CDPTarget(setup)
	if err != nil {
		http.Error(w, "browser CDP unavailable", 503)
		return
	}
	upstream, err := newNativeCDP(setup, access.Page, access.Allowed)
	if err != nil {
		http.Error(w, "browser CDP unavailable", 503)
		return
	}
	defer upstream.Close()
	client, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer client.Close()
	proxyCDP(ctx, client, upstream, target, access.Allowed, func(method string, send func() error) error {
		return access.Page.DispatchCDP(ctx, method, access.Allowed, send)
	})
}

type protocolMessage struct {
	ID      int64          `json:"id,omitempty"`
	Method  string         `json:"method,omitempty"`
	Session string         `json:"sessionId,omitempty"`
	Params  map[string]any `json:"params,omitempty"`
	Result  any            `json:"result,omitempty"`
	Error   any            `json:"error,omitempty"`
}

// The browser connection is needed by Playwright's connectOverCDP handshake.
// Root auto-attach is replaced by an exact target attachment; it must never
// auto-attach (or pause) another conversation's pages. Page commands/events then
// use Chromium's real sessions, including this page's out-of-process iframes.
type cdpPolicy struct {
	target   string
	sessions map[string]bool
	pending  map[int64]protocolMessage
	attached bool
}

func (p *cdpPolicy) command(m protocolMessage) (protocolMessage, error) {
	deny := errors.New("CDP operation outside this tab is blocked; use browser_tabs for tab lifecycle")
	if m.ID <= 0 || m.Method == "" || len(p.pending) >= 256 {
		return m, deny
	}
	if _, exists := p.pending[m.ID]; exists {
		return m, errors.New("duplicate CDP command id")
	}
	if m.Session != "" && !p.sessions[m.Session] {
		return m, deny
	}
	if m.Params == nil {
		m.Params = map[string]any{}
	}
	original := m
	switch m.Method {
	case "Browser.getVersion":
		if m.Session != "" {
			return m, deny
		}
	case "Target.setAutoAttach":
		if m.Session == "" {
			if p.attached {
				m.Method = "Target.getTargetInfo"
				m.Params = map[string]any{"targetId": p.target}
			} else {
				p.attached = true
				m.Method = "Target.attachToTarget"
				m.Params = map[string]any{"targetId": p.target, "flatten": true}
			}
		} else {
			m.Params["waitForDebuggerOnStart"] = false
			m.Params["flatten"] = true
			m.Params["filter"] = []any{map[string]any{"type": "iframe"}, map[string]any{"exclude": true}}
		}
	case "Target.getTargetInfo":
		if target, _ := m.Params["targetId"].(string); target != "" && target != p.target {
			return m, deny
		}
		m.Params = map[string]any{"targetId": p.target}
	case "Target.getTargets":
		m.Params = map[string]any{}
	case "Target.attachToTarget":
		if m.Params["targetId"] != p.target {
			return m, deny
		}
		m.Params["flatten"] = true
	case "Target.detachFromTarget":
		session, _ := m.Params["sessionId"].(string)
		if !p.sessions[session] {
			return m, deny
		}
	case "Page.navigate":
		address, _ := m.Params["url"].(string)
		if m.Session == "" || !ValidPageURL(address) {
			return m, deny
		}
	default:
		if m.Session == "" || !pageCDPMethod(m.Method) {
			return m, deny
		}
	}
	p.pending[m.ID] = original
	return m, nil
}

func pageCDPMethod(method string) bool {
	domain, _, _ := strings.Cut(method, ".")
	switch domain {
	case "Runtime", "DOM", "CSS", "Accessibility", "Input", "Log", "Inspector":
		return true
	case "Page":
		switch method {
		case "Page.enable", "Page.disable", "Page.getFrameTree", "Page.getLayoutMetrics", "Page.getNavigationHistory", "Page.navigateToHistoryEntry", "Page.reload", "Page.stopLoading", "Page.captureScreenshot", "Page.createIsolatedWorld", "Page.addScriptToEvaluateOnNewDocument", "Page.removeScriptToEvaluateOnNewDocument", "Page.setLifecycleEventsEnabled", "Page.handleJavaScriptDialog":
			return true
		}
	case "Network":
		switch method {
		case "Network.enable", "Network.disable", "Network.getResponseBody", "Network.getRequestPostData", "Network.setExtraHTTPHeaders", "Network.setCacheDisabled", "Network.setBypassServiceWorker", "Network.emulateNetworkConditions":
			return true
		}
	case "Fetch":
		switch method {
		case "Fetch.enable", "Fetch.disable", "Fetch.continueRequest", "Fetch.continueResponse", "Fetch.continueWithAuth", "Fetch.failRequest", "Fetch.fulfillRequest", "Fetch.getResponseBody":
			return true
		}
	case "Emulation":
		switch method {
		case "Emulation.setFocusEmulationEnabled", "Emulation.setEmulatedMedia", "Emulation.setDeviceMetricsOverride", "Emulation.clearDeviceMetricsOverride", "Emulation.setUserAgentOverride", "Emulation.setTouchEmulationEnabled", "Emulation.setLocaleOverride", "Emulation.setTimezoneOverride", "Emulation.setScriptExecutionDisabled":
			return true
		}
	}
	return false
}

func (p *cdpPolicy) response(m protocolMessage) (protocolMessage, bool) {
	if m.ID != 0 {
		request, ok := p.pending[m.ID]
		if !ok {
			return m, false
		}
		delete(p.pending, m.ID)
		if m.Error == nil && request.Method == "Target.setAutoAttach" && request.Session == "" {
			m.Result = map[string]any{}
		}
		if request.Method == "Target.getTargets" && m.Error == nil {
			result, _ := m.Result.(map[string]any)
			targets, _ := result["targetInfos"].([]any)
			filtered := []any{}
			for _, entry := range targets {
				if info, ok := entry.(map[string]any); ok && info["targetId"] == p.target {
					filtered = append(filtered, info)
				}
			}
			m.Result = map[string]any{"targetInfos": filtered}
		}
		return m, true
	}
	if m.Method == "Target.attachedToTarget" {
		info, _ := m.Params["targetInfo"].(map[string]any)
		session, _ := m.Params["sessionId"].(string)
		if session == "" || !(info["targetId"] == p.target || (p.sessions[m.Session] && info["type"] == "iframe")) {
			return m, false
		}
		p.sessions[session] = true
		return m, true
	}
	if m.Method == "Target.detachedFromTarget" {
		session, _ := m.Params["sessionId"].(string)
		if !p.sessions[session] {
			return m, false
		}
		delete(p.sessions, session)
		return m, true
	}
	if m.Method == "Target.targetInfoChanged" {
		info, _ := m.Params["targetInfo"].(map[string]any)
		return m, info["targetId"] == p.target
	}
	return m, m.Session != "" && p.sessions[m.Session] && !strings.HasPrefix(m.Method, "Target.") && !strings.HasPrefix(m.Method, "Browser.")
}

func proxyCDP(ctx context.Context, client, upstream cdpConnection, target string, allowed func() bool, dispatch func(string, func() error) error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type input struct {
		upstream bool
		message  protocolMessage
		err      error
	}
	queue := make(chan input, 32)
	for _, side := range []struct {
		conn     cdpConnection
		upstream bool
	}{{client, false}, {upstream, true}} {
		side.conn.SetReadLimit(16 << 20)
		go func() {
			for {
				var m protocolMessage
				_, raw, err := side.conn.ReadMessage()
				if err == nil {
					err = json.Unmarshal(raw, &m)
				}
				select {
				case queue <- input{side.upstream, m, err}:
				case <-ctx.Done():
					return
				}
				if err != nil {
					return
				}
			}
		}()
	}
	p := cdpPolicy{target: target, sessions: map[string]bool{}, pending: map[int64]protocolMessage{}}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	write := func(conn cdpConnection, m protocolMessage) error {
		_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
		return conn.WriteMessage(websocket.TextMessage, mustJSON(m))
	}
	for {
		if !allowed() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			continue
		case in := <-queue:
			if in.err != nil || !allowed() {
				return
			}
			if in.upstream {
				if m, ok := p.response(in.message); ok {
					if write(client, m) != nil {
						return
					}
				}
				continue
			}
			m, err := p.command(in.message)
			if err == nil {
				err = dispatch(m.Method, func() error { return write(upstream, m) })
				if err != nil {
					delete(p.pending, in.message.ID)
				}
			}
			if err != nil {
				if write(client, protocolMessage{ID: in.message.ID, Session: in.message.Session, Error: map[string]any{"code": -32000, "message": err.Error()}}) != nil {
					return
				}
			}
		}
	}
}

// Each consumer gets a real, independent target session. WebView2 carries
// commands and all events natively using Target's message envelope; the broker
// presents the same flattened protocol Playwright expects. No Chromium port,
// hard-coded event list, or shared debugger state between consumers is needed.
type nativeCDP struct {
	page    *NativePage
	allowed func() bool
	ctx     context.Context
	cancel  context.CancelFunc
	stop    func()
	once    sync.Once
	mu      sync.Mutex
	root    string
	parents map[string]string
	in      chan []byte
	out     chan protocolMessage
}

func newNativeCDP(ctx context.Context, page *NativePage, allowed func() bool) (*nativeCDP, error) {
	n := &nativeCDP{page: page, allowed: allowed, parents: map[string]string{}, in: make(chan []byte, 256), out: make(chan protocolMessage, 256)}
	n.ctx, n.cancel = context.WithCancel(context.Background())
	stop, err := page.cdpEvent(ctx, "Target.receivedMessageFromTarget", func(raw json.RawMessage) {
		var envelope struct {
			Session string `json:"sessionId"`
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &envelope) != nil {
			return
		}
		n.mu.Lock()
		defer n.mu.Unlock()
		if n.root != "" && envelope.Session == n.root {
			n.receive([]byte(envelope.Message), "")
		}
	})
	if err != nil {
		n.cancel()
		return nil, err
	}
	n.stop = stop
	target, err := page.CDPTarget(ctx)
	if err == nil {
		var raw json.RawMessage
		raw, err = page.Call(ctx, "Target.attachToTarget", mustJSON(map[string]any{"targetId": target, "flatten": false}), allowed)
		var result struct {
			Session string `json:"sessionId"`
		}
		if err == nil {
			err = json.Unmarshal(raw, &result)
		}
		n.mu.Lock()
		n.root = result.Session
		n.mu.Unlock()
		if err == nil && result.Session == "" {
			err = errors.New("native CDP session missing")
		}
	}
	if err != nil {
		n.Close()
		return nil, err
	}
	go n.send()
	return n, nil
}

// Called only under mu. Unwrap nested iframe sessions without exposing events
// from any other Inspector or Playwright connection on this WebView.
func (n *nativeCDP) receive(raw []byte, session string) {
	var m protocolMessage
	if json.Unmarshal(raw, &m) != nil || m.ID < 0 {
		return
	}
	if m.Method == "Target.receivedMessageFromTarget" {
		child, _ := m.Params["sessionId"].(string)
		message, _ := m.Params["message"].(string)
		if parent, ok := n.parents[child]; ok && parent == session {
			n.receive([]byte(message), child)
		}
		return
	}
	if m.Method == "Target.attachedToTarget" {
		if child, _ := m.Params["sessionId"].(string); child != "" {
			n.parents[child] = session
		}
	}
	if m.Method == "Target.detachedFromTarget" {
		child, _ := m.Params["sessionId"].(string)
		delete(n.parents, child)
	}
	m.Session = session
	n.deliver(mustJSON(m))
}

func (n *nativeCDP) deliver(raw []byte) {
	select {
	case n.in <- raw:
	case <-n.ctx.Done():
	default:
		n.cancel() // Fail closed rather than silently dropping debugger events.
	}
}

func (n *nativeCDP) send() {
	defer n.Close()
	for {
		select {
		case <-n.ctx.Done():
			return
		case m := <-n.out:
			original := m
			if m.Method == "Target.attachToTarget" || m.Method == "Target.setAutoAttach" {
				if m.Params == nil {
					m.Params = map[string]any{}
				}
				m.Params["flatten"] = false
			}
			session := m.Session
			m.Session = ""
			raw := mustJSON(m)
			n.mu.Lock()
			valid := true
			for session != "" {
				parent, ok := n.parents[session]
				if !ok {
					valid = false
					break
				}
				raw = mustJSON(protocolMessage{ID: -1, Method: "Target.sendMessageToTarget", Params: map[string]any{"sessionId": session, "message": string(raw)}})
				session = parent
			}
			root := n.root
			n.mu.Unlock()
			err := errors.New("native CDP session detached")
			if valid {
				ctx, cancel := context.WithTimeout(n.ctx, callTimeout)
				_, err = n.page.Call(ctx, "Target.sendMessageToTarget", mustJSON(map[string]any{"sessionId": root, "message": string(raw)}), n.allowed)
				cancel()
			}
			if err != nil {
				n.deliver(mustJSON(protocolMessage{ID: original.ID, Session: original.Session, Error: map[string]any{"code": -32000, "message": err.Error()}}))
			}
		}
	}
}

func (n *nativeCDP) ReadMessage() (int, []byte, error) {
	select {
	case <-n.ctx.Done():
		return 0, nil, n.ctx.Err()
	case raw := <-n.in:
		return websocket.TextMessage, raw, nil
	}
}
func (n *nativeCDP) WriteMessage(_ int, raw []byte) error {
	var m protocolMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	select {
	case <-n.ctx.Done():
		return n.ctx.Err()
	case n.out <- m:
		return nil
	default:
		n.cancel()
		return errors.New("native CDP command queue is full")
	}
}
func (*nativeCDP) SetReadLimit(int64)               {}
func (*nativeCDP) SetWriteDeadline(time.Time) error { return nil }
func (n *nativeCDP) Close() error {
	n.once.Do(func() {
		n.cancel()
		n.stop()
		n.mu.Lock()
		root := n.root
		n.mu.Unlock()
		if root != "" {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_, _ = n.page.Call(ctx, "Target.detachFromTarget", mustJSON(map[string]any{"sessionId": root}), nil)
			}()
		}
	})
	return nil
}
