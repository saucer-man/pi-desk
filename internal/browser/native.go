package browser

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type AgentCommand struct {
	ThreadID string          `json:"threadId"`
	TabID    string          `json:"tabId"`
	Action   string          `json:"action"`
	Method   string          `json:"method"`
	Params   json.RawMessage `json:"params"`
}

var broker struct {
	sync.RWMutex
	address string
	secret  []byte
}

// Per-process capabilities bind commands to the owning Pi conversation. A page
// cannot call this endpoint: no CORS, no cookies, and a mandatory secret header.
func AgentEnvironment(threadID string) []string {
	broker.RLock()
	defer broker.RUnlock()
	if broker.address == "" || threadID == "" {
		return nil
	}
	mac := hmac.New(sha256.New, broker.secret)
	mac.Write([]byte(threadID))
	return []string{"PI_DESK_BROWSER_URL=" + broker.address, "PI_DESK_BROWSER_TOKEN=" + hex.EncodeToString(mac.Sum(nil)), "PI_DESK_BROWSER_THREAD=" + threadID}
}
func ServeAgent(handler func(context.Context, AgentCommand) (any, error)) (func() error, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		listener.Close()
		return nil, err
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 45 * time.Second}
	connections, cancelConnections := context.WithCancel(context.Background())
	slots := make(chan struct{}, 64)
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/browser/cdp" {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			default:
				http.Error(w, "too many browser connections", 429)
				return
			}
			serveCDP(connections, w, r, secret, handler)
			return
		}
		if r.Method != "POST" || r.URL.Path != "/browser" || r.Header.Get("Origin") != "" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var command AgentCommand
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&command) != nil || command.Action == "connect" {
			http.Error(w, "invalid request", 400)
			return
		}
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte(command.ThreadID))
		provided, err := hex.DecodeString(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil || command.ThreadID == "" || !hmac.Equal(provided, mac.Sum(nil)) {
			http.Error(w, "forbidden", 403)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		result, err := handler(ctx, command)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(409)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
	broker.Lock()
	broker.address = "http://" + listener.Addr().String() + "/browser"
	broker.secret = secret
	broker.Unlock()
	go server.Serve(listener)
	return func() error {
		cancelConnections()
		broker.Lock()
		broker.address = ""
		broker.secret = nil
		broker.Unlock()
		return server.Close()
	}, nil
}
func ValidateAgentCommand(command AgentCommand) error {
	switch command.Method {
	case "Page.enable", "Page.captureScreenshot", "Page.getLayoutMetrics", "Page.getFrameTree", "Page.getNavigationHistory", "Page.reload", "Input.dispatchMouseEvent", "Input.dispatchKeyEvent", "Input.insertText", "Runtime.evaluate":
	case "Page.createIsolatedWorld":
		var payload struct {
			FrameID              string `json:"frameId"`
			GrantUniversalAccess bool   `json:"grantUniveralAccess"`
		}
		if json.Unmarshal(command.Params, &payload) != nil || payload.FrameID == "" || payload.GrantUniversalAccess {
			return errors.New("invalid isolated world parameters")
		}
	case "Page.navigateToHistoryEntry":
		var payload struct {
			EntryID *int `json:"entryId"`
		}
		if json.Unmarshal(command.Params, &payload) != nil || payload.EntryID == nil || *payload.EntryID < 0 {
			return errors.New("invalid browser history entry")
		}
	case "Page.navigate":
		var payload struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(command.Params, &payload) != nil || !ValidPageURL(payload.URL) {
			return errors.New("invalid browser URL")
		}
	default:
		return errors.New("unsupported browser protocol method")
	}
	if !json.Valid(command.Params) {
		return errors.New("invalid browser parameters")
	}
	return nil
}

func ValidPageURL(raw string) bool {
	if raw == "about:blank" {
		return true
	}
	parsed, err := url.Parse(raw)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && !strings.ContainsAny(raw, "\x00\r\n")
}
