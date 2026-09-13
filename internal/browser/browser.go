// Package browser talks to the managed Chromium that the pi-desk-browser
// extension launches with --remote-debugging-port=0 and a dedicated user
// data directory. Pi Desk only attaches here for the inspector panel's
// screencast and input injection; the browser process itself is owned by
// the extension so agent flows work without the panel ever opening.
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	portFileName    = "DevToolsActivePort"
	discoverTimeout = 3 * time.Second
	callTimeout     = 20 * time.Second
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

type cdpTargetInfo struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	Title                string `json:"title"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
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
type Client struct {
	conn    *websocket.Conn
	writeMu sync.Mutex // guards nextID and pending
	connMu  sync.Mutex // gorilla/websocket allows one concurrent writer only
	nextID  int64
	pending map[int64]chan cdpMessage
	OnEvent func(method string, params json.RawMessage)
	done    chan struct{}
}

func Dial(ctx context.Context, wsURL string) (*Client, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to managed browser: %w", err)
	}
	client := &Client{conn: conn, pending: make(map[int64]chan cdpMessage), done: make(chan struct{})}
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

func (client *Client) ackScreencastFrame(params json.RawMessage) {
	var frame struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(params, &frame) != nil || frame.SessionID == "" {
		return
	}
	_ = client.Call("Page.screencastFrameAck", map[string]string{"sessionId": frame.SessionID}, nil)
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

	client.connMu.Lock()
	writeErr := client.conn.WriteMessage(websocket.TextMessage, payload)
	client.connMu.Unlock()
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
	case <-time.After(callTimeout):
		client.takePending(id)
		return fmt.Errorf("%s timed out", method)
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
