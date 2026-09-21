package appservice

import (
	"context"
	"pi-desk/internal/browser"
	"pi-desk/internal/domain"
	"pi-desk/internal/pirpc"
	"pi-desk/internal/piruntime"
	"testing"
)

func TestBrowserCleanupOnHostSessionEnd(t *testing.T) {
	for _, mode := range []string{"exit", "stop", "maintenance", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			b := NewBrowserService()
			retained := &browserTab{status: domain.BrowserStatus{TabID: "kept", ThreadID: "one"}}
			temporary := &browserTab{status: domain.BrowserStatus{TabID: "temp", ThreadID: "one", Temporary: true}}
			other := &browserTab{status: domain.BrowserStatus{TabID: "other", ThreadID: "two"}}
			b.tabs = map[string]*browserTab{"kept": retained, "temp": temporary, "other": other}
			service := newAgentService(&fakeAgentRuntime{})
			service.browser = b
			// Unrelated events and invalid stop requests must not revoke pages.
			service.handleRuntimeExit(piruntime.SessionEvent{ThreadID: "one", Event: pirpc.Event{Type: "message_update"}})
			if service.StopSession(domain.ThreadRequest{}) == nil || retained.epoch.Load() != 0 {
				t.Fatal("invalid stop or ordinary event changed browser state")
			}
			switch mode {
			case "exit":
				service.handleRuntimeExit(piruntime.SessionEvent{ThreadID: "one", Event: pirpc.Event{Type: "runtime_exit", Generation: 1}})
			case "stop":
				if err := service.StopSession(domain.ThreadRequest{ThreadID: "one"}); err != nil {
					t.Fatal(err)
				}
			case "maintenance":
				release, err := service.preparePiMaintenance()
				if err != nil {
					t.Fatal(err)
				}
				release()
			case "shutdown":
				if err := service.ServiceShutdown(); err != nil {
					t.Fatal(err)
				}
			}
			if b.tabs["temp"] != nil || !temporary.closed || b.tabs["kept"] != retained || retained.epoch.Load() == 0 {
				t.Fatal("host exit must revoke connections, close temporary tabs and retain user pages")
			}
			if (mode == "exit" || mode == "stop") && other.epoch.Load() != 0 {
				t.Fatal("single-session cleanup revoked another conversation")
			}
		})
	}
}

func TestBrowserConversationIsolationAndConnectionLifetime(t *testing.T) {
	s := NewBrowserService()
	a := &browserTab{status: domain.BrowserStatus{TabID: "a", ThreadID: "one", Temporary: true}, page: &browser.NativePage{}}
	b := &browserTab{status: domain.BrowserStatus{TabID: "b", ThreadID: "two"}, page: &browser.NativePage{}}
	s.tabs["a"] = a
	s.tabs["b"] = b
	s.selected["one"] = "a"
	for _, action := range []string{"ensure", "call", "inspect", "select", "keep", "close", "cdp", "connect"} {
		if _, err := s.agentCommand(context.Background(), browser.AgentCommand{ThreadID: "one", Action: action}); err == nil {
			t.Fatalf("%s fell back to the selected page without tabId", action)
		}
	}
	_, err := s.agentCommand(context.Background(), browser.AgentCommand{ThreadID: "one", TabID: "b", Action: "select"})
	if err == nil {
		t.Fatal("another conversation's tab was accepted")
	}
	if err = s.KeepTab("a"); err != nil {
		t.Fatal(err)
	}
	if a.status.Temporary {
		t.Fatal("keep must retain the tab")
	}
	if _, err = s.agentCommand(context.Background(), browser.AgentCommand{ThreadID: "one", TabID: "a", Action: "ensure"}); err != nil {
		t.Fatal(err)
	}
	for _, command := range []browser.AgentCommand{
		{ThreadID: "one", TabID: "b", Action: "inspect", Method: "state", Params: []byte(`{}`)},
		{ThreadID: "one", TabID: "b", Action: "cdp"},
		{ThreadID: "one", TabID: "b", Action: "connect"},
		{ThreadID: "one", TabID: "a", Action: "connect", Params: []byte(`{"epoch":1}`)},
	} {
		if _, err := s.agentCommand(context.Background(), command); err == nil {
			t.Fatalf("accepted invalid CDP access: %+v", command)
		}
	}
	access, err := s.agentCommand(context.Background(), browser.AgentCommand{ThreadID: "one", TabID: "a", Action: "connect", Params: []byte(`{"epoch":0}`)})
	if err != nil || !access.(*browser.CDPAccess).Allowed() {
		t.Fatalf("valid CDP access failed: %v", err)
	}
	s.finishThread("one")
	if access.(*browser.CDPAccess).Allowed() {
		t.Fatal("active connection survived agent end")
	}
	if _, err = s.agentCommand(context.Background(), browser.AgentCommand{ThreadID: "one", TabID: "a", Action: "connect", Params: []byte(`{"epoch":0}`)}); err == nil {
		t.Fatal("stale connection accepted")
	}
	fresh, err := s.agentCommand(context.Background(), browser.AgentCommand{ThreadID: "one", TabID: "a", Action: "connect", Params: []byte(`{"epoch":1}`)})
	if err != nil || !fresh.(*browser.CDPAccess).Allowed() {
		t.Fatalf("new connection failed: %v", err)
	}
	result, err := s.agentCommand(context.Background(), browser.AgentCommand{ThreadID: "one", Action: "list"})
	if err != nil || len(result.([]domain.BrowserStatus)) != 1 {
		t.Fatalf("list leaked another conversation: %v %v", result, err)
	}
	_, err = s.agentCommand(context.Background(), browser.AgentCommand{ThreadID: "one", TabID: "a", Action: "close"})
	if err == nil {
		t.Fatal("agent could close a retained user page")
	}
	before, other := a.epoch.Load(), b.epoch.Load()
	if _, err = s.agentCommand(context.Background(), browser.AgentCommand{ThreadID: "one", Action: "finish"}); err != nil {
		t.Fatal(err)
	}
	if a.epoch.Load() == before || b.epoch.Load() != other || s.tabs["a"] != a {
		t.Fatal("finish must revoke own connections, retaining user tabs and leaving other conversations alone")
	}
	if fresh.(*browser.CDPAccess).Allowed() {
		t.Fatal("renewed connection survived another agent end")
	}
	latest, err := s.agentCommand(context.Background(), browser.AgentCommand{ThreadID: "one", TabID: "a", Action: "connect", Params: []byte(`{"epoch":2}`)})
	if err != nil {
		t.Fatal(err)
	}
	a.page = nil // No Wails window exists in this service-only test.
	if err = s.CloseTab("a"); err != nil {
		t.Fatal(err)
	}
	if latest.(*browser.CDPAccess).Allowed() {
		t.Fatal("connection survived tab closure")
	}
}
