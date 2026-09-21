package browser

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestEmbeddedBrokerRejectsForeignCapabilitiesAndPageRequests(t *testing.T) {
	stop, err := ServeAgent(func(_ context.Context, c AgentCommand) (any, error) { return c.ThreadID, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	env := map[string]string{}
	for _, entry := range AgentEnvironment("thread-a") {
		key, value, _ := strings.Cut(entry, "=")
		env[key] = value
	}
	for _, tc := range []struct {
		thread, token, origin string
		code                  int
	}{
		{"thread-a", env["PI_DESK_BROWSER_TOKEN"], "", 200},
		{"thread-b", env["PI_DESK_BROWSER_TOKEN"], "", 403},
		{"thread-a", "", "", 403},
		{"thread-a", env["PI_DESK_BROWSER_TOKEN"], "https://untrusted.example", 403},
	} {
		body, _ := json.Marshal(AgentCommand{ThreadID: tc.thread, Action: "list"})
		req, _ := http.NewRequest("POST", env["PI_DESK_BROWSER_URL"], strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set("Origin", tc.origin)
		response, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		response.Body.Close()
		if response.StatusCode != tc.code {
			t.Fatalf("unexpected broker status: %d, want %d", response.StatusCode, tc.code)
		}
	}
	for _, url := range []string{"file:///C:/secret", "javascript:alert(1)", "wails://main", "https://", "http://example.com\n"} {
		if ValidPageURL(url) {
			t.Fatalf("unsafe URL accepted: %q", url)
		}
	}
	if ValidateAgentCommand(AgentCommand{Method: "Browser.close", Params: json.RawMessage(`{}`)}) == nil {
		t.Fatal("browser-wide protocol method accepted")
	}
}

func TestEmbeddedBrowserNavigationValidation(t *testing.T) {
	for _, tc := range []struct {
		method, params string
		valid          bool
	}{
		{"Page.getNavigationHistory", `{}`, true},
		{"Page.reload", `{}`, true},
		{"Page.navigateToHistoryEntry", `{"entryId":12}`, true},
		{"Page.navigateToHistoryEntry", `{}`, false},
		{"Page.navigateToHistoryEntry", `{"entryId":-1}`, false},
		{"Page.createIsolatedWorld", `{"frameId":"main","worldName":"pi-desk-tools"}`, true},
		{"Page.createIsolatedWorld", `{"frameId":"main","grantUniveralAccess":true}`, false},
		{"Page.createIsolatedWorld", `{}`, false},
	} {
		if err := ValidateAgentCommand(AgentCommand{Method: tc.method, Params: json.RawMessage(tc.params)}); (err == nil) != tc.valid {
			t.Fatalf("%s %s: %v", tc.method, tc.params, err)
		}
	}
}

func TestScreencastAcknowledgementsHaveDistinctCommandIDs(t *testing.T) {
	messages := make(chan cdpMessage, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for session := 1; session <= 2; session++ {
			_ = conn.WriteJSON(cdpMessage{Method: "Page.screencastFrame", Params: mustJSON(map[string]int{"sessionId": session})})
			var ack cdpMessage
			if conn.ReadJSON(&ack) != nil {
				return
			}
			messages <- ack
		}
	}))
	defer server.Close()
	client, err := Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var previous int64
	for range 2 {
		select {
		case ack := <-messages:
			if ack.ID <= previous || ack.Method != "Page.screencastFrameAck" {
				t.Fatalf("invalid acknowledgement: %+v", ack)
			}
			previous = ack.ID
		case <-time.After(2 * time.Second):
			t.Fatal("screencast acknowledgement did not arrive")
		}
	}
}

func TestReadPortFile(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if _, _, err := ReadPortFile(directory); !errors.Is(err, ErrBrowserNotRunning) {
		t.Fatalf("expected ErrBrowserNotRunning for a missing port file, got %v", err)
	}

	if err := os.WriteFile(filepath.Join(directory, portFileName), []byte("0\n/devtools/browser/x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadPortFile(directory); err == nil {
		t.Fatal("expected port 0 to be rejected")
	}

	if err := os.WriteFile(filepath.Join(directory, portFileName), []byte("9223\n/devtools/browser/guid"), 0o600); err != nil {
		t.Fatal(err)
	}
	port, wsPath, err := ReadPortFile(directory)
	if err != nil || port != 9223 || wsPath != "/devtools/browser/guid" {
		t.Fatalf("unexpected port file parse: port=%d path=%q err=%v", port, wsPath, err)
	}
}

func TestParseKeySpec(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  KeySpec
	}{
		{"Enter", KeySpec{Key: "Enter", Code: "Enter", VirtualKey: 13, Text: "\r"}},
		{"esc", KeySpec{Key: "Escape", Code: "Escape", VirtualKey: 27}},
		{"space", KeySpec{Key: " ", Code: "Space", VirtualKey: 32, Text: " "}},
		{"ArrowLeft", KeySpec{Key: "ArrowLeft", Code: "ArrowLeft", VirtualKey: 37}},
		{"F12", KeySpec{Key: "F12", Code: "F12", VirtualKey: 123}},
		{"a", KeySpec{Key: "a", Code: "KeyA", VirtualKey: 65, Text: "a"}},
		{"5", KeySpec{Key: "5", Code: "Digit5", VirtualKey: 53, Text: "5"}},
	}
	for _, testCase := range cases {
		got, err := ParseKeySpec(testCase.input)
		if err != nil || got != testCase.want {
			t.Fatalf("ParseKeySpec(%q) = %#v, %v; want %#v", testCase.input, got, err, testCase.want)
		}
	}
	for _, invalid := range []string{"", "  ", "ctrl+a", "abc", "F13"} {
		if _, err := ParseKeySpec(invalid); err == nil {
			t.Fatalf("ParseKeySpec(%q) unexpectedly succeeded", invalid)
		}
	}
}

func TestProfileDirUnderLocalAppData(t *testing.T) {
	t.Setenv("LOCALAPPDATA", filepath.Join(t.TempDir(), "local"))
	directory, err := ProfileDir()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(directory) != "browser" || filepath.Base(filepath.Dir(directory)) != "pi-desk" {
		t.Fatalf("unexpected profile directory %q", directory)
	}
}
