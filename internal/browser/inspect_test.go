package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

func TestInspectorBuffersPaginationAndTakeover(t *testing.T) {
	var taken atomic.Bool
	i := &Inspector{client: &Client{done: make(chan struct{})}, allowed: func() bool { return !taken.Load() }, events: map[string][]inspectEvent{}, dropped: map[string]int{}, breakpoints: map[string]json.RawMessage{}, xhr: map[string]bool{}}
	for n := 0; n < 1005; n++ {
		i.event("Network.requestWillBeSent", json.RawMessage(fmt.Sprintf(`{"requestId":"%d","request":{"url":"https://example.test/%d"}}`, n, n)))
	}
	i.event("Network.webSocketFrameReceived", json.RawMessage(`{"requestId":"ws","response":{"payloadData":"hello"}}`))
	i.event("Debugger.paused", json.RawMessage(`{"callFrames":[{"callFrameId":"frame"}]}`))
	result, err := i.Request(context.Background(), "events", json.RawMessage(`{"kind":"network","limit":2}`))
	if err != nil {
		t.Fatal(err)
	}
	page := result.(map[string]any)
	if page["dropped"] != 5 || len(page["events"].([]inspectEvent)) != 2 || page["hasMore"] != true || page["cursor"] != uint64(7) {
		t.Fatalf("bad pagination: %+v", page)
	}
	result, err = i.Request(context.Background(), "events", json.RawMessage(`{"kind":"network","requestId":"1004"}`))
	if err != nil || len(result.(map[string]any)["events"].([]inspectEvent)) != 1 {
		t.Fatalf("request filter: %v %v", result, err)
	}
	state, err := i.Request(context.Background(), "state", json.RawMessage(`{}`))
	if err != nil || !strings.Contains(string(state.(json.RawMessage)), "frame") {
		t.Fatalf("paused state: %s %v", state, err)
	}
	i.event("Debugger.resumed", json.RawMessage(`{}`))
	if i.paused != nil {
		t.Fatal("stale paused state")
	}
	for _, method := range []string{"Browser.close", "Target.attachToTarget", "Storage.getCookies", "Network.clearBrowserCache", "Fetch.enable"} {
		if _, err = i.Request(context.Background(), method, json.RawMessage(`{}`)); err == nil {
			t.Fatalf("accepted %s", method)
		}
	}
	if _, err = i.Request(context.Background(), "events", json.RawMessage(`{"kind":"network|websocket"}`)); err == nil {
		t.Fatal("accepted invalid capture kind")
	}
	_, err = i.Request(context.Background(), "clear", json.RawMessage(`{"kind":"network"}`))
	if err != nil || len(i.events["network"]) != 0 || len(i.events["websocket"]) != 1 {
		t.Fatal("clear crossed collection boundaries")
	}
	i.event("Debugger.scriptParsed", json.RawMessage(`{"scriptId":"old"}`))
	i.event("Runtime.executionContextsCleared", json.RawMessage(`{}`))
	if len(i.events["scripts"]) != 0 {
		t.Fatal("navigation kept stale script ids")
	}
	taken.Store(true)
	i.event("Runtime.consoleAPICalled", json.RawMessage(`{"type":"log"}`))
	if len(i.events["console"]) != 0 {
		t.Fatal("events survived takeover")
	}
	if _, err = i.Request(context.Background(), "events", json.RawMessage(`{"kind":"websocket"}`)); err == nil {
		t.Fatal("read survived takeover")
	}
}
