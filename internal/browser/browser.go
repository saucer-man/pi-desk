// Package browser hosts embedded WebView2 pages and their scoped CDP tools.
// The CDP client is shared by the embedded page inspector.
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const callTimeout = 20 * time.Second

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
