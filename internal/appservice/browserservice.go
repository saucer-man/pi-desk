package appservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"pi-desk/internal/browser"
	"pi-desk/internal/domain"
)

const browserEventName = "browser:event"

type browserTab struct {
	status    domain.BrowserStatus
	page      *browser.NativePage
	ready     chan struct{}
	err       error
	closed    bool
	epoch     atomic.Uint64
	inspectMu sync.Mutex
	inspector atomic.Pointer[browser.Inspector]
}

// The host owns page identity. Unmounting a BrowserPane only hides its viewport.
type BrowserService struct {
	mu         sync.Mutex
	host       *browser.NativeHost
	tabs       map[string]*browserTab
	selected   map[string]string
	emit       func(domain.BrowserEvent)
	stopBroker func() error
}

func NewBrowserService() *BrowserService {
	return &BrowserService{tabs: map[string]*browserTab{}, selected: map[string]string{}}
}
func (s *BrowserService) ServiceStartup(context.Context, application.ServiceOptions) error {
	profile, err := browser.ProfileDir()
	if err != nil {
		return err
	}
	profile = filepath.Join(filepath.Dir(profile), "browser-webview2")
	app := application.Get()
	s.host = browser.NewNativeHost(func() uintptr {
		window, ok := app.Window.GetByName("main")
		if !ok {
			return 0
		}
		native, ok := window.(*application.WebviewWindow)
		if !ok {
			return 0
		}
		return uintptr(native.NativeWindow())
	}, profile)
	s.emit = func(event domain.BrowserEvent) { app.Event.Emit(browserEventName, event) }
	s.stopBroker, err = browser.ServeAgent(s.agentCommand)
	return err
}
func (s *BrowserService) publish(tab *browserTab, kind string) {
	s.mu.Lock()
	status := tab.status
	closed := tab.closed
	s.mu.Unlock()
	if s.emit != nil && (!closed || kind == "closed") {
		s.emit(domain.BrowserEvent{Type: kind, TabID: status.TabID, ThreadID: status.ThreadID, Status: &status})
	}
}
func (s *BrowserService) StartTab(request domain.BrowserStartRequest) (domain.BrowserStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tab, err := s.start(ctx, request, false)
	if err != nil {
		return domain.BrowserStatus{}, err
	}
	s.mu.Lock()
	s.selected[request.ThreadID] = request.TabID
	status := tab.status
	s.mu.Unlock()
	return status, nil
}
func (s *BrowserService) start(ctx context.Context, request domain.BrowserStartRequest, temporary bool) (*browserTab, error) {
	if strings.TrimSpace(request.TabID) == "" || strings.TrimSpace(request.ThreadID) == "" || len(request.TabID) > 256 || len(request.ThreadID) > 256 {
		return nil, errors.New("invalid browser identity")
	}
	if request.URL == "" {
		request.URL = "about:blank"
	}
	if !browser.ValidPageURL(request.URL) {
		return nil, errors.New("invalid browser URL")
	}
	s.mu.Lock()
	tab := s.tabs[request.TabID]
	if tab != nil {
		if tab.status.ThreadID != request.ThreadID {
			s.mu.Unlock()
			return nil, errors.New("browser tab belongs to another conversation")
		}
		s.mu.Unlock()
		select {
		case <-tab.ready:
			return tab, tab.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.host == nil {
		s.mu.Unlock()
		return nil, errors.New("browser host is not ready")
	}
	count := 0
	for _, existing := range s.tabs {
		if existing.status.ThreadID == request.ThreadID {
			count++
		}
	}
	if count >= 16 || len(s.tabs) >= 64 {
		s.mu.Unlock()
		return nil, errors.New("too many open browser pages; close unused tabs first")
	}
	tab = &browserTab{status: domain.BrowserStatus{TabID: request.TabID, ThreadID: request.ThreadID, URL: request.URL, Temporary: temporary}, ready: make(chan struct{})}
	s.tabs[request.TabID] = tab
	s.mu.Unlock()
	page, err := s.host.NewPage(ctx, func(state browser.NativeState) {
		s.mu.Lock()
		tab.status.URL = state.URL
		tab.status.Title = state.Title
		tab.status.Loading = state.Loading
		tab.status.CanGoBack = state.CanGoBack
		tab.status.CanGoForward = state.CanGoForward
		tab.status.Error = state.Error
		s.mu.Unlock()
		s.publish(tab, "state")
	})
	if err == nil {
		err = page.Navigate(ctx, request.URL)
	}
	s.mu.Lock()
	tab.page = page
	tab.err = err
	tab.status.Attached = err == nil
	closed := tab.closed
	close(tab.ready)
	if err != nil && s.tabs[request.TabID] == tab {
		delete(s.tabs, request.TabID)
	}
	s.mu.Unlock()
	if closed || err != nil {
		if page != nil {
			page.Close()
		}
		if err == nil {
			err = errors.New("browser tab was closed")
		}
	} else {
		s.publish(tab, "opened")
	}
	return tab, err
}

// Also called by the host when Pi exits without delivering agent_end.
// An empty thread ID is used only by host-wide maintenance/shutdown.
func (s *BrowserService) finishThread(threadID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	ids := []string{}
	for id, tab := range s.tabs {
		if threadID == "" || tab.status.ThreadID == threadID {
			tab.epoch.Add(1)
			tab.inspector.Load().Close()
			if tab.status.Temporary {
				ids = append(ids, id)
			}
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		_ = s.CloseTab(id)
	}
}
func (s *BrowserService) tab(id string) (*browserTab, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tab := s.tabs[id]
	if tab == nil || tab.closed || tab.page == nil {
		return nil, errors.New("browser tab is not ready")
	}
	return tab, nil
}
func (s *BrowserService) CloseTab(id string) error {
	s.mu.Lock()
	tab := s.tabs[id]
	if tab != nil {
		tab.closed = true
		tab.epoch.Add(1)
		tab.inspector.Load().Close()
		delete(s.tabs, id)
		if s.selected[tab.status.ThreadID] == id {
			delete(s.selected, tab.status.ThreadID)
		}
	}
	page := (*browser.NativePage)(nil)
	if tab != nil {
		page = tab.page
	}
	s.mu.Unlock()
	if tab != nil {
		if page != nil {
			page.Close()
		}
		s.publish(tab, "closed")
	}
	return nil
}
func (s *BrowserService) SetBounds(request domain.BrowserBoundsRequest) error {
	tab, err := s.tab(request.TabID)
	if err != nil {
		return err
	}
	if request.Width < 0 || request.Height < 0 || request.Width > 32768 || request.Height > 32768 || request.X < 0 || request.Y < 0 {
		return errors.New("invalid browser bounds")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return tab.page.Bounds(ctx, request.X, request.Y, request.Width, request.Height, request.Visible)
}
func (s *BrowserService) OpenURL(request domain.BrowserOpenURLRequest) (domain.BrowserStatus, error) {
	tab, err := s.tab(request.TabID)
	if err != nil {
		return domain.BrowserStatus{}, err
	}
	if !browser.ValidPageURL(request.URL) {
		return domain.BrowserStatus{}, errors.New("only http, https and about:blank URLs are supported")
	}
	_ = s.KeepTab(request.TabID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = tab.page.Navigate(ctx, request.URL); err != nil {
		return domain.BrowserStatus{}, err
	}
	s.mu.Lock()
	status := tab.status
	s.mu.Unlock()
	return status, nil
}
func (s *BrowserService) Command(id, command string) error {
	tab, err := s.tab(id)
	if err != nil {
		return err
	}
	_ = s.KeepTab(id)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return tab.page.Command(ctx, command)
}
func (s *BrowserService) KeepTab(id string) error {
	tab, err := s.tab(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	tab.status.Temporary = false
	s.mu.Unlock()
	s.publish(tab, "state")
	return nil
}
func (s *BrowserService) agentCommand(ctx context.Context, command browser.AgentCommand) (any, error) {
	if command.Action == "list" {
		s.mu.Lock()
		defer s.mu.Unlock()
		states := []domain.BrowserStatus{}
		for _, tab := range s.tabs {
			if tab.status.ThreadID == command.ThreadID {
				states = append(states, tab.status)
			}
		}
		return states, nil
	}
	if command.Action == "finish" {
		if command.ThreadID == "" {
			return nil, errors.New("threadId is required")
		}
		s.finishThread(command.ThreadID)
		return true, nil
	}
	if command.Action == "create" {
		var params struct {
			URL string `json:"url"`
		}
		if len(command.Params) != 0 && json.Unmarshal(command.Params, &params) != nil {
			return nil, errors.New("invalid browser parameters")
		}
		command.TabID = fmt.Sprintf("%s:agent:%d", command.ThreadID, time.Now().UnixNano())
		tab, err := s.start(ctx, domain.BrowserStartRequest{ThreadID: command.ThreadID, TabID: command.TabID, URL: params.URL}, true)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.selected[command.ThreadID] = command.TabID
		status := tab.status
		s.mu.Unlock()
		return status, nil
	}
	if strings.TrimSpace(command.TabID) == "" {
		return nil, errors.New("tabId is required; use browser_tabs to list or create a tab")
	}
	tab, err := s.tab(command.TabID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	status := tab.status
	s.mu.Unlock()
	if status.ThreadID != command.ThreadID {
		return nil, errors.New("browser tab belongs to another conversation")
	}
	switch command.Action {
	case "inspect":
		tab.inspectMu.Lock()
		inspector := tab.inspector.Load()
		if command.Method == "stop" {
			inspector.Close()
			tab.inspectMu.Unlock()
			return true, nil
		}
		if inspector == nil || !inspector.Active() {
			inspector.Close()
			epoch := tab.epoch.Load()
			inspector, err = browser.NewInspector(ctx, tab.page, func() bool { return tab.epoch.Load() == epoch })
			if err == nil {
				tab.inspector.Store(inspector)
				if !inspector.Active() {
					inspector.Close()
				}
			}
		}
		tab.inspectMu.Unlock()
		if err != nil {
			return nil, err
		}
		return inspector.Request(ctx, command.Method, command.Params)
	case "cdp", "connect":
		epoch := tab.epoch.Load()
		if command.Action == "cdp" {
			return map[string]string{"endpoint": browser.AgentCDPURL(command.ThreadID, command.TabID, epoch)}, nil
		}
		var params struct {
			Epoch uint64 `json:"epoch"`
		}
		if json.Unmarshal(command.Params, &params) != nil || params.Epoch != epoch {
			return nil, errors.New("stale browser connection; request a new connection")
		}
		return &browser.CDPAccess{Page: tab.page, Allowed: func() bool { return tab.epoch.Load() == epoch }}, nil
	case "ensure", "select":
		s.mu.Lock()
		s.selected[command.ThreadID] = command.TabID
		s.mu.Unlock()
		return status, nil
	case "keep":
		_ = s.KeepTab(command.TabID)
		return true, nil
	case "close":
		if !status.Temporary {
			return nil, errors.New("only temporary Agent tabs may be closed by the Agent")
		}
		return true, s.CloseTab(command.TabID)
	case "call":
		if err = browser.ValidateAgentCommand(command); err != nil {
			return nil, err
		}
		epoch := tab.epoch.Load()
		return tab.page.Call(ctx, command.Method, command.Params, func() bool { return tab.epoch.Load() == epoch })
	default:
		return nil, errors.New("unknown browser action")
	}
}
func (s *BrowserService) ServiceShutdown() error {
	if s.stopBroker != nil {
		_ = s.stopBroker()
	}
	s.mu.Lock()
	ids := make([]string, 0, len(s.tabs))
	for id := range s.tabs {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		_ = s.CloseTab(id)
	}
	return nil
}
