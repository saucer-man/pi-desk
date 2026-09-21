// Package browser hosts embedded WebView2 pages and their scoped CDP tools.
// The WebSocket client is also shared with the legacy Chromium helpers.
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	portFileName      = "DevToolsActivePort"
	discoverTimeout   = 3 * time.Second
	callTimeout       = 20 * time.Second
	launchWaitTimeout = 15 * time.Second
)

var ErrBrowserNotRunning = errors.New("managed browser is not running; ask Pi to use a browser tool first")

// ProfileDir is the dedicated Chromium user data directory, shared with the
// extension (LOCALAPPDATA on Windows so browser caches stay out of Roaming).
func ProfileDir() (string, error) {
	base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if base == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve browser profile directory: %w", err)
		}
		base = config
	}
	return filepath.Join(base, "pi-desk", "browser"), nil
}

// ReadPortFile parses DevToolsActivePort: line one is the port, line two the
// browser-level WebSocket path.
func ReadPortFile(profileDir string) (port int, wsPath string, err error) {
	content, readErr := os.ReadFile(filepath.Join(profileDir, portFileName))
	if readErr != nil {
		return 0, "", ErrBrowserNotRunning
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) == 0 {
		return 0, "", fmt.Errorf("empty %s in %s", portFileName, profileDir)
	}
	port, parseErr := strconv.Atoi(strings.TrimSpace(lines[0]))
	if parseErr != nil || port <= 0 || port > 65535 {
		return 0, "", fmt.Errorf("invalid port in %s: %q", portFileName, strings.TrimSpace(lines[0]))
	}
	wsPath = "/devtools/browser"
	if len(lines) > 1 {
		wsPath = strings.TrimSpace(lines[1])
	}
	return port, wsPath, nil
}

// Target describes one attachable page (tab) of the managed browser.
type Target struct {
	Port   int
	PageWS string
	URL    string
	Title  string
}

// Locate finds an installed Chromium executable, preferring Chrome over Edge.
func Locate() (string, error) {
	candidates := []struct{ root, rest string }{
		{os.Getenv("ProgramFiles"), `Google\Chrome\Application\chrome.exe`},
		{os.Getenv("ProgramFiles(x86)"), `Google\Chrome\Application\chrome.exe`},
		{os.Getenv("LocalAppData"), `Google\Chrome\Application\chrome.exe`},
		{os.Getenv("ProgramFiles"), `Microsoft\Edge\Application\msedge.exe`},
		{os.Getenv("ProgramFiles(x86)"), `Microsoft\Edge\Application\msedge.exe`},
	}
	for _, candidate := range candidates {
		if candidate.root == "" {
			continue
		}
		path := filepath.Join(candidate.root, candidate.rest)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", errors.New("no Chromium browser found; install Google Chrome or Microsoft Edge")
}

// Launch starts a detached managed Chromium with the dedicated profile and
// waits for its DevTools endpoint. Safe to race with the extension's own
// launcher: a second invocation with the same user data directory forwards
// to the running instance.
func Launch(profileDir string) (Target, error) {
	executable, err := Locate()
	if err != nil {
		return Target{}, err
	}
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return Target{}, fmt.Errorf("create browser profile directory: %w", err)
	}
	command := exec.Command(executable,
		"--remote-debugging-port=0", "--user-data-dir="+profileDir,
		"--no-first-run", "--no-default-browser-check",
		"--disable-session-crashed-bubble", "--hide-crash-restore-bubble",
		"about:blank")
	if err := command.Start(); err != nil {
		return Target{}, fmt.Errorf("start managed browser: %w", err)
	}
	go func() { _ = command.Wait() }()
	deadline := time.Now().Add(launchWaitTimeout)
	for time.Now().Before(deadline) {
		if target, discoverErr := DiscoverPage(profileDir); discoverErr == nil {
			return target, nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return Target{}, errors.New("timed out waiting for the managed browser to start")
}

type cdpTargetInfo struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	Title                string `json:"title"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// NewPage creates an independent CDP page without replacing the agent's page.
func NewPage(profileDir string) (Target, error) {
	target, err := DiscoverPage(profileDir)
	if errors.Is(err, ErrBrowserNotRunning) {
		target, err = Launch(profileDir)
	}
	if err != nil {
		return Target{}, err
	}
	request, err := http.NewRequest(http.MethodPut, fmt.Sprintf("http://127.0.0.1:%d/json/new?about:blank", target.Port), nil)
	if err != nil {
		return Target{}, err
	}
	response, err := (&http.Client{Timeout: discoverTimeout}).Do(request)
	if err != nil {
		return Target{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Target{}, fmt.Errorf("create browser page: HTTP %d", response.StatusCode)
	}
	var page cdpTargetInfo
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		return Target{}, err
	}
	return Target{Port: target.Port, PageWS: page.WebSocketDebuggerURL, URL: page.URL, Title: page.Title}, nil
}

// DiscoverPage finds the first inspectable page target of the managed browser.
func DiscoverPage(profileDir string) (Target, error) {
	port, _, err := ReadPortFile(profileDir)
	if err != nil {
		return Target{}, err
	}
	request, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/list", port), nil)
	if requestErr != nil {
		return Target{}, requestErr
	}
	client := &http.Client{Timeout: discoverTimeout}
	response, err := client.Do(request)
	if err != nil {
		return Target{}, ErrBrowserNotRunning
	}
	defer response.Body.Close()
	var targets []cdpTargetInfo
	if err := json.NewDecoder(response.Body).Decode(&targets); err != nil {
		return Target{}, fmt.Errorf("decode browser target list: %w", err)
	}
	for _, target := range targets {
		if target.Type == "page" && target.WebSocketDebuggerURL != "" && !strings.HasPrefix(target.URL, "devtools://") {
			return Target{Port: port, PageWS: target.WebSocketDebuggerURL, URL: target.URL, Title: target.Title}, nil
		}
	}
	return Target{}, errors.New("managed browser has no open page")
}

type cdpMessage struct {
	ID     int64           `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Client is a minimal CDP WebSocket client. Screencast frames are acked
// directly in the reader goroutine so a slow consumer never stalls the
// frame stream; events are delivered through OnEvent afterwards.
type cdpConnection interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(int, []byte) error
	SetReadLimit(int64)
	SetWriteDeadline(time.Time) error
	Close() error
}

type Client struct {
	conn    cdpConnection
	writeMu sync.Mutex // guards nextID and pending
	connMu  sync.Mutex // gorilla/websocket allows one concurrent writer only
	nextID  int64
	pending map[int64]chan cdpMessage
	OnEvent func(method string, params json.RawMessage)
	done    chan struct{}
}

func Dial(ctx context.Context, wsURL string) (*Client, error) {
	return dialEvents(ctx, wsURL, nil)
}

func dialEvents(ctx context.Context, wsURL string, event func(string, json.RawMessage)) (*Client, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to managed browser: %w", err)
	}
	client := &Client{conn: conn, pending: make(map[int64]chan cdpMessage), done: make(chan struct{}), OnEvent: event}
	conn.SetReadLimit(32 << 20)
	go client.read()
	return client, nil
}

func (client *Client) Done() <-chan struct{} { return client.done }

func (client *Client) Close() {
	_ = client.conn.Close()
}

func (client *Client) read() {
	defer close(client.done)
	for {
		_, raw, err := client.conn.ReadMessage()
		if err != nil {
			client.failPending(errors.New("browser connection closed"))
			return
		}
		var message cdpMessage
		if json.Unmarshal(raw, &message) != nil {
			continue
		}
		if message.ID != 0 {
			if waiter := client.takePending(message.ID); waiter != nil {
				waiter <- message
				close(waiter)
			}
			continue
		}
		if message.Method == "" {
			continue
		}
		if message.Method == "Page.screencastFrame" {
			client.ackScreencastFrame(message.Params)
		}
		if client.OnEvent != nil {
			client.OnEvent(message.Method, message.Params)
		}
	}
}

// notify writes a CDP command without waiting for its reply. The reader
// goroutine uses it for screencast acks — waiting there would deadlock the
// read loop against its own response.
func (client *Client) notify(method string, params any) {
	client.writeMu.Lock()
	client.nextID++
	id := client.nextID
	client.writeMu.Unlock()
	payload, err := json.Marshal(cdpMessage{ID: id, Method: method, Params: mustJSON(params)})
	if err != nil {
		return
	}
	client.connMu.Lock()
	_ = client.conn.WriteMessage(websocket.TextMessage, payload)
	client.connMu.Unlock()
}

func (client *Client) ackScreencastFrame(params json.RawMessage) {
	var frame struct {
		SessionID int64 `json:"sessionId"`
	}
	if json.Unmarshal(params, &frame) != nil || frame.SessionID == 0 {
		return
	}
	client.notify("Page.screencastFrameAck", map[string]any{"sessionId": frame.SessionID})
}

func (client *Client) takePending(id int64) chan cdpMessage {
	client.writeMu.Lock()
	defer client.writeMu.Unlock()
	waiter := client.pending[id]
	delete(client.pending, id)
	return waiter
}

func (client *Client) failPending(reason error) {
	client.writeMu.Lock()
	defer client.writeMu.Unlock()
	for id, waiter := range client.pending {
		waiter <- cdpMessage{Error: &struct {
			Message string `json:"message"`
		}{Message: reason.Error()}}
		close(waiter)
		delete(client.pending, id)
	}
}

// Call sends one CDP command and waits for its reply. A nil result discards it.
func (client *Client) Call(method string, params any, result any) error {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return client.call(ctx, method, params, result, nil)
}

func (client *Client) call(ctx context.Context, method string, params any, result any, dispatch func(func() error) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	client.writeMu.Lock()
	client.nextID++
	id := client.nextID
	waiter := make(chan cdpMessage, 1)
	client.pending[id] = waiter
	payload, err := json.Marshal(cdpMessage{ID: id, Method: method, Params: mustJSON(params)})
	if err != nil {
		delete(client.pending, id)
		client.writeMu.Unlock()
		return fmt.Errorf("encode %s: %w", method, err)
	}
	client.writeMu.Unlock()

	send := func() error {
		client.connMu.Lock()
		defer client.connMu.Unlock()
		_ = client.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return client.conn.WriteMessage(websocket.TextMessage, payload)
	}
	var writeErr error
	if dispatch != nil {
		writeErr = dispatch(send)
	} else {
		writeErr = send()
	}
	if writeErr != nil {
		client.takePending(id)
		return fmt.Errorf("send %s: %w", method, writeErr)
	}
	select {
	case message := <-waiter:
		if message.Error != nil {
			return fmt.Errorf("%s failed: %s", method, message.Error.Message)
		}
		if result != nil && len(message.Result) > 0 {
			if err := json.Unmarshal(message.Result, result); err != nil {
				return fmt.Errorf("decode %s result: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		client.takePending(id)
		return fmt.Errorf("%s: %w", method, ctx.Err())
	case <-client.done:
		return fmt.Errorf("%s failed: browser connection closed", method)
	}
}

func mustJSON(params any) json.RawMessage {
	if params == nil {
		return nil
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil
	}
	return encoded
}
