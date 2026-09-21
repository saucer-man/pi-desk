package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestNativeCDPFlattensOnlyOwnedSessions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := &nativeCDP{ctx: ctx, cancel: cancel, parents: map[string]string{}, in: make(chan []byte, 4)}
	n.receive(mustJSON(protocolMessage{Method: "Target.attachedToTarget", Params: map[string]any{"sessionId": "page"}}), "")
	n.receive(mustJSON(protocolMessage{Method: "Target.attachedToTarget", Params: map[string]any{"sessionId": "frame"}}), "page")
	<-n.in
	<-n.in
	response := string(mustJSON(protocolMessage{ID: 7, Result: map[string]any{"value": "frame result"}}))
	envelope := func(session string) []byte {
		return mustJSON(protocolMessage{Method: "Target.receivedMessageFromTarget", Params: map[string]any{"sessionId": session, "message": response}})
	}
	n.receive(envelope("frame"), "other")
	n.receive(envelope("unowned"), "page")
	n.receive([]byte(`{"id":-1,"result":{}}`), "page")
	if len(n.in) != 0 {
		t.Fatal("foreign session or internal acknowledgement leaked")
	}
	n.receive(envelope("frame"), "page")
	var result protocolMessage
	if json.Unmarshal(<-n.in, &result) != nil || result.Session != "frame" || result.ID != 7 {
		t.Fatalf("nested response not flattened: %+v", result)
	}
	n.receive(mustJSON(protocolMessage{Method: "Target.detachedFromTarget", Params: map[string]any{"sessionId": "frame"}}), "page")
	<-n.in
	n.receive(envelope("frame"), "page")
	if len(n.in) != 0 {
		t.Fatal("detached frame retained events")
	}
	for j := 0; j < 5; j++ {
		n.deliver([]byte(`{}`))
	}
	if ctx.Err() == nil {
		t.Fatal("overflow silently dropped protocol events")
	}
}

func TestCDPPolicyScopesTargetsAndMethods(t *testing.T) {
	p := cdpPolicy{target: "owned", sessions: map[string]bool{}, pending: map[int64]protocolMessage{}}
	m, err := p.command(protocolMessage{ID: 1, Method: "Target.setAutoAttach", Params: map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true}})
	if err != nil || m.Method != "Target.attachToTarget" || m.Params["targetId"] != "owned" {
		t.Fatalf("unscoped auto attach: %+v %v", m, err)
	}
	for _, target := range []string{"other", "owned"} {
		_, ok := p.response(protocolMessage{Method: "Target.attachedToTarget", Params: map[string]any{"sessionId": target, "targetInfo": map[string]any{"targetId": target, "type": "page"}}})
		if ok != (target == "owned") {
			t.Fatalf("attached %s: %v", target, ok)
		}
	}
	m, ok := p.response(protocolMessage{ID: 1, Result: map[string]any{"sessionId": "owned"}})
	if !ok || len(m.Result.(map[string]any)) != 0 {
		t.Fatal("auto-attach response was not normalized")
	}
	for _, request := range []protocolMessage{
		{Method: "Runtime.evaluate"},
		{Method: "Runtime.evaluate", Session: "other"},
		{Method: "Target.attachToTarget", Params: map[string]any{"targetId": "other"}},
		{Method: "Target.getTargetInfo", Params: map[string]any{"targetId": "other"}},
		{Method: "Target.sendMessageToTarget", Params: map[string]any{"sessionId": "other", "message": "{}"}},
		{Method: "Target.createTarget"}, {Method: "Target.closeTarget"}, {Method: "Target.attachToBrowserTarget"},
		{Method: "Browser.close"}, {Method: "Browser.setDownloadBehavior"}, {Method: "Storage.getCookies"},
		{Method: "Network.getAllCookies", Session: "owned"}, {Method: "Network.clearBrowserCookies", Session: "owned"},
		{Method: "Page.navigate", Session: "owned", Params: map[string]any{"url": "file:///C:/private"}},
	} {
		request.ID = 2
		if _, err = p.command(request); err == nil {
			t.Fatalf("accepted unscoped method: %+v", request)
		}
	}
	for _, method := range []string{"Runtime.evaluate", "DOM.getDocument", "Input.dispatchMouseEvent", "Network.enable", "Network.getResponseBody", "Fetch.enable", "Page.captureScreenshot"} {
		if _, err = p.command(protocolMessage{ID: 3, Session: "owned", Method: method}); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		delete(p.pending, 3)
	}
	if _, err = p.command(protocolMessage{ID: 4, Method: "Target.getTargets"}); err != nil {
		t.Fatal(err)
	}
	if _, err = p.command(protocolMessage{ID: 4, Method: "Browser.getVersion"}); err == nil {
		t.Fatal("duplicate id could bypass result filtering")
	}
	m, ok = p.response(protocolMessage{ID: 4, Result: map[string]any{"targetInfos": []any{map[string]any{"targetId": "other", "title": "private"}, map[string]any{"targetId": "owned"}}}})
	if !ok || len(m.Result.(map[string]any)["targetInfos"].([]any)) != 1 {
		t.Fatalf("leaked targets: %+v", m)
	}
	for _, session := range []string{"", "other", "owned"} {
		if _, ok = p.response(protocolMessage{Session: session, Method: "Network.requestWillBeSent"}); ok != (session == "owned") {
			t.Fatal("leaked network event")
		}
	}
	for _, kind := range []string{"page", "worker", "iframe"} {
		_, ok = p.response(protocolMessage{Session: "owned", Method: "Target.attachedToTarget", Params: map[string]any{"sessionId": kind, "targetInfo": map[string]any{"targetId": kind, "type": kind}}})
		if ok != (kind == "iframe") {
			t.Fatalf("unexpected child target %s", kind)
		}
	}
	_, _ = p.response(protocolMessage{Method: "Target.detachedFromTarget", Params: map[string]any{"sessionId": "iframe"}})
	if p.sessions["iframe"] {
		t.Fatal("detached frame retained access")
	}
}

func TestCDPCapabilityCannotChangeTabThreadOrEpoch(t *testing.T) {
	calls := 0
	stop, err := ServeAgent(func(context.Context, AgentCommand) (any, error) { calls++; return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	endpoint, _ := url.Parse(strings.Replace(AgentCDPURL("thread", "tab", 5), "ws:", "http:", 1))
	for _, key := range []string{"threadId", "tabId", "epoch", "key"} {
		modified := *endpoint
		query := modified.Query()
		query.Set(key, "6")
		modified.RawQuery = query.Encode()
		resp, e := http.Get(modified.String())
		if e != nil {
			t.Fatal(e)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatalf("tampered %s: %d", key, resp.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", endpoint.String(), nil)
	req.Header.Set("Origin", "https://page.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 || calls != 0 {
		t.Fatal("page origin or modified capability reached the host")
	}
}
