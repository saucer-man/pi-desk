package appservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"pi-desk/internal/browser"
	"pi-desk/internal/domain"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const browserEventName = "browser:event"

// BrowserService streams the managed browser into the inspector panel and
// injects panel input back over CDP. The browser process is launched by the
// pi-desk-browser extension; attaching is best-effort and read-only until
// the panel is open.
type BrowserService struct {
	mu         sync.Mutex
	emit       func(domain.BrowserEvent)
	profileDir string
	client     *browser.Client
	url        string
	title      string
	sequence   atomic.Uint64
}

func NewBrowserService() *BrowserService {
	return &BrowserService{}
}

func (service *BrowserService) ServiceStartup(context.Context, application.ServiceOptions) error {
	profileDir, err := browser.ProfileDir()
	if err != nil {
		return err
	}
	app := application.Get()
	service.profileDir = profileDir
	service.emit = func(event domain.BrowserEvent) {
		event.Sequence = service.sequence.Add(1)
		app.Event.Emit(browserEventName, event)
	}
	return nil
}

func (service *BrowserService) ServiceShutdown() error {
	service.mu.Lock()
	client := service.client
	service.client = nil
	service.mu.Unlock()
	if client != nil {
		client.Close()
	}
	return nil
}

// Start attaches to the managed browser and begins the screencast.
func (service *BrowserService) Start() (domain.BrowserStatus, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.emit == nil {
		return domain.BrowserStatus{}, errors.New("browser service is not ready")
	}
	if service.client != nil {
		return domain.BrowserStatus{Attached: true, URL: service.url, Title: service.title, ProfileDir: service.profileDir}, nil
	}
	target, err := browser.DiscoverPage(service.profileDir)
	if err != nil {
		return domain.BrowserStatus{}, err
	}
	client, err := browser.Dial(context.Background(), target.PageWS)
	if err != nil {
		return domain.BrowserStatus{}, err
	}
	client.OnEvent = func(method string, params json.RawMessage) { service.onCDPEvent(client, method, params) }
	if err := client.Call("Page.enable", nil, nil); err != nil {
		client.Close()
		return domain.BrowserStatus{}, err
	}
	if err := client.Call("Page.startScreencast", map[string]any{"format": "jpeg", "quality": 60, "maxWidth": 1600, "maxHeight": 1600}, nil); err != nil {
		client.Close()
		return domain.BrowserStatus{}, err
	}
	service.client = client
	service.url, service.title = target.URL, target.Title
	go service.watchClosed(client)
	service.emit(domain.BrowserEvent{Type: "attached", URL: target.URL, Title: target.Title})
	return domain.BrowserStatus{Attached: true, URL: target.URL, Title: target.Title, ProfileDir: service.profileDir}, nil
}

// Stop detaches from the managed browser. The browser itself keeps running.
func (service *BrowserService) Stop() error {
	service.mu.Lock()
	client := service.client
	service.client = nil
	service.mu.Unlock()
	if client != nil {
		client.Close()
	}
	return nil
}

func (service *BrowserService) watchClosed(client *browser.Client) {
	<-client.Done()
	service.mu.Lock()
	if service.client == client {
		service.client = nil
	}
	emit := service.emit
	service.mu.Unlock()
	if emit != nil {
		emit(domain.BrowserEvent{Type: "detached"})
	}
}

func (service *BrowserService) onCDPEvent(client *browser.Client, method string, params json.RawMessage) {
	service.mu.Lock()
	emit := service.emit
	attached := service.client == client
	service.mu.Unlock()
	if emit == nil || !attached {
		return
	}
	switch method {
	case "Page.screencastFrame":
		var frame struct {
			Data     string `json:"data"`
			Metadata struct {
				DeviceWidth  int `json:"deviceWidth"`
				DeviceHeight int `json:"deviceHeight"`
			} `json:"metadata"`
		}
		if json.Unmarshal(params, &frame) == nil && frame.Data != "" {
			emit(domain.BrowserEvent{
				Type: "frame", DataB64: frame.Data,
				CssWidth: frame.Metadata.DeviceWidth, CssHeight: frame.Metadata.DeviceHeight,
			})
		}
	case "Page.frameNavigated":
		var navigation struct {
			Frame struct {
				URL      string `json:"url"`
				ParentID string `json:"parentId"`
			} `json:"frame"`
		}
		if json.Unmarshal(params, &navigation) == nil && navigation.Frame.ParentID == "" && strings.HasPrefix(navigation.Frame.URL, "http") {
			emit(domain.BrowserEvent{Type: "navigated", URL: navigation.Frame.URL})
		}
	}
}

func (service *BrowserService) clientForInput() (*browser.Client, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.client == nil {
		return nil, errors.New("browser panel is not connected")
	}
	return service.client, nil
}

// Click injects a mouse press/release at CSS-pixel page coordinates.
func (service *BrowserService) Click(request domain.BrowserClickRequest) error {
	client, err := service.clientForInput()
	if err != nil {
		return err
	}
	button := request.Button
	if button == "" {
		button = "left"
	}
	switch button {
	case "left", "right", "middle":
	default:
		return fmt.Errorf("unsupported mouse button %q", button)
	}
	clickCount := request.ClickCount
	if clickCount <= 0 {
		clickCount = 1
	}
	if clickCount > 3 {
		return errors.New("click count is outside the supported range")
	}
	base := map[string]any{"x": request.X, "y": request.Y, "button": button, "clickCount": clickCount}
	pressed := cloneParams(base)
	pressed["type"] = "mousePressed"
	released := cloneParams(base)
	released["type"] = "mouseReleased"
	if err := client.Call("Input.dispatchMouseEvent", pressed, nil); err != nil {
		return err
	}
	return client.Call("Input.dispatchMouseEvent", released, nil)
}

// Scroll injects a mouse wheel event at CSS-pixel page coordinates.
func (service *BrowserService) Scroll(request domain.BrowserWheelRequest) error {
	client, err := service.clientForInput()
	if err != nil {
		return err
	}
	return client.Call("Input.dispatchMouseEvent", map[string]any{
		"type": "mouseWheel", "x": request.X, "y": request.Y,
		"deltaX": request.DeltaX, "deltaY": request.DeltaY,
	}, nil)
}

// Key injects a named key press, e.g. "Enter", "ctrl+shift+T".
func (service *BrowserService) Key(request domain.BrowserKeyRequest) error {
	client, err := service.clientForInput()
	if err != nil {
		return err
	}
	spec, err := browser.ParseKeySpec(request.Key)
	if err != nil {
		return err
	}
	down := map[string]any{"key": spec.Key, "code": spec.Code, "windowsVirtualKeyCode": spec.VirtualKey, "modifiers": request.Modifiers}
	if spec.Text != "" {
		down["type"] = "keyDown"
		down["text"] = spec.Text
	} else {
		down["type"] = "rawKeyDown"
	}
	if err := client.Call("Input.dispatchKeyEvent", down, nil); err != nil {
		return err
	}
	up := map[string]any{"type": "keyUp", "key": spec.Key, "code": spec.Code, "windowsVirtualKeyCode": spec.VirtualKey, "modifiers": request.Modifiers}
	return client.Call("Input.dispatchKeyEvent", up, nil)
}

// Type inserts text into the focused element via Input.insertText.
func (service *BrowserService) Type(request domain.BrowserTextInputRequest) error {
	client, err := service.clientForInput()
	if err != nil {
		return err
	}
	if request.Text == "" {
		return errors.New("browser text input is empty")
	}
	if len(request.Text) > maxTerminalInput {
		return errors.New("browser text input exceeds the 64 KiB limit")
	}
	return client.Call("Input.insertText", map[string]any{"text": request.Text}, nil)
}

func cloneParams(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source)+1)
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
