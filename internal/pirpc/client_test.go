package pirpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeProcess struct {
	stdinReader  *io.PipeReader
	stdinWriter  *io.PipeWriter
	stdoutReader *io.PipeReader
	stdoutWriter *io.PipeWriter
	stderrReader *io.PipeReader
	stderrWriter *io.PipeWriter
	wait         chan error
	exitOnce     sync.Once
}

func newFakeProcess() *fakeProcess {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	return &fakeProcess{
		stdinReader:  stdinReader,
		stdinWriter:  stdinWriter,
		stdoutReader: stdoutReader,
		stdoutWriter: stdoutWriter,
		stderrReader: stderrReader,
		stderrWriter: stderrWriter,
		wait:         make(chan error, 1),
	}
}

func (process *fakeProcess) Stdin() io.WriteCloser { return process.stdinWriter }
func (process *fakeProcess) Stdout() io.Reader     { return process.stdoutReader }
func (process *fakeProcess) Stderr() io.Reader     { return process.stderrReader }
func (process *fakeProcess) Wait() error           { return <-process.wait }

func (process *fakeProcess) Kill() error {
	process.exit(ErrClientClosed)
	return nil
}

func (process *fakeProcess) exit(err error) {
	process.exitOnce.Do(func() {
		_ = process.stdoutWriter.Close()
		_ = process.stderrWriter.Close()
		_ = process.stdinReader.Close()
		process.wait <- err
	})
}

func readRequest(t *testing.T, reader *bufio.Reader) map[string]any {
	t.Helper()
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read request: %v", err)
	}
	var request map[string]any
	if err := json.Unmarshal([]byte(line), &request); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return request
}

func writeJSONLine(t *testing.T, writer io.Writer, value any) {
	t.Helper()
	record, err := EncodeRecord(value, 4096)
	if err != nil {
		t.Fatalf("encode response: %v", err)
	}
	if _, err := writer.Write(record); err != nil {
		t.Fatalf("write response: %v", err)
	}
}

func TestClientCorrelatesConcurrentResponsesAndForwardsEvents(t *testing.T) {
	process := newFakeProcess()
	events := make(chan Event, 4)
	client := NewClientWithLimits(process, 7, func(event Event) { events <- event }, 4096, 1024)
	t.Cleanup(func() { _ = client.Close() })

	go func() {
		reader := bufio.NewReader(process.stdinReader)
		first := readRequest(t, reader)
		second := readRequest(t, reader)
		writeJSONLine(t, process.stdoutWriter, map[string]any{"type": "agent_start"})
		writeJSONLine(t, process.stdoutWriter, map[string]any{
			"id": second["id"], "type": "response", "command": second["type"], "success": true,
			"data": map[string]any{"label": second["label"]},
		})
		writeJSONLine(t, process.stdoutWriter, map[string]any{
			"id": first["id"], "type": "response", "command": first["type"], "success": true,
			"data": map[string]any{"label": first["label"]},
		})
	}()

	type result struct {
		label string
		err   error
	}
	results := make(chan result, 2)
	for _, label := range []string{"first", "second"} {
		label := label
		go func() {
			response, err := client.Call(context.Background(), map[string]any{"type": "get_state", "label": label})
			if err != nil {
				results <- result{err: err}
				return
			}
			var data struct {
				Label string `json:"label"`
			}
			if err := json.Unmarshal(response.Data, &data); err != nil {
				results <- result{err: err}
				return
			}
			results <- result{label: data.Label}
		}()
	}

	seen := map[string]bool{}
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		seen[result.label] = true
	}
	if !seen["first"] || !seen["second"] {
		t.Fatalf("responses were not correlated: %#v", seen)
	}

	select {
	case event := <-events:
		if event.Type != "agent_start" || event.Generation != 7 {
			t.Fatalf("unexpected event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestClientCancellationIgnoresLateResponse(t *testing.T) {
	process := newFakeProcess()
	events := make(chan Event, 4)
	client := NewClientWithLimits(process, 1, func(event Event) { events <- event }, 4096, 1024)
	t.Cleanup(func() { _ = client.Close() })

	requestSeen := make(chan map[string]any, 1)
	go func() {
		requestSeen <- readRequest(t, bufio.NewReader(process.stdinReader))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := client.Call(ctx, map[string]any{"type": "get_state"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}

	request := <-requestSeen
	writeJSONLine(t, process.stdoutWriter, map[string]any{
		"id": request["id"], "type": "response", "command": "get_state", "success": true,
	})

	select {
	case event := <-events:
		t.Fatalf("late response for an abandoned request emitted an event: %#v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestClientReportsTrulyUnknownResponseID(t *testing.T) {
	process := newFakeProcess()
	events := make(chan Event, 1)
	client := NewClientWithLimits(process, 1, func(event Event) { events <- event }, 4096, 1024)
	t.Cleanup(func() { _ = client.Close() })

	writeJSONLine(t, process.stdoutWriter, map[string]any{
		"id": "req-never-sent", "type": "response", "command": "get_state", "success": true,
	})

	select {
	case event := <-events:
		if event.Type != "protocol_error" || !strings.Contains(event.Error, "unknown id") {
			t.Fatalf("unexpected protocol event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for unknown-response diagnostic")
	}
}

func TestClientDoesNotReportAClosedStdoutHandleAsAProtocolError(t *testing.T) {
	process := newFakeProcess()
	events := make(chan Event, 1)
	client := NewClientWithLimits(process, 2, func(event Event) { events <- event }, 4096, 1024)
	t.Cleanup(func() { _ = client.Close() })

	if err := process.stdoutReader.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case <-client.Done():
	case <-time.After(time.Second):
		t.Fatal("client did not close after stdout was closed")
	}
	select {
	case event := <-events:
		t.Fatalf("closed stdout emitted a protocol event: %#v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestClientReportsMalformedRecordsAndBoundsStderr(t *testing.T) {
	process := newFakeProcess()
	events := make(chan Event, 4)
	client := NewClientWithLimits(process, 2, func(event Event) { events <- event }, 4096, 8)
	t.Cleanup(func() { _ = client.Close() })

	go func() {
		_, _ = io.WriteString(process.stderrWriter, "0123456789abcdef")
		_, _ = io.WriteString(process.stdoutWriter, "{bad json}\n")
	}()

	select {
	case event := <-events:
		if event.Type != "protocol_error" || !strings.Contains(event.Error, "decode") {
			t.Fatalf("unexpected protocol event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for malformed-record event")
	}

	deadline := time.Now().Add(time.Second)
	for client.Diagnostics() != "89abcdef" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if diagnostics := client.Diagnostics(); diagnostics != "89abcdef" {
		t.Fatalf("unexpected bounded diagnostics: %q", diagnostics)
	}
}

func TestClientReturnsRemoteAndProcessErrors(t *testing.T) {
	process := newFakeProcess()
	client := NewClientWithLimits(process, 1, nil, 4096, 1024)
	t.Cleanup(func() { _ = client.Close() })

	go func() {
		request := readRequest(t, bufio.NewReader(process.stdinReader))
		writeJSONLine(t, process.stdoutWriter, map[string]any{
			"id": request["id"], "type": "response", "command": "set_model", "success": false, "error": "unknown model",
		})
	}()

	_, err := client.Call(context.Background(), map[string]any{"type": "set_model"})
	var remoteError *RemoteError
	if !errors.As(err, &remoteError) || remoteError.Message != "unknown model" {
		t.Fatalf("expected RemoteError, got %v", err)
	}

	process.exit(fmt.Errorf("exit status 3"))
	select {
	case <-client.Done():
	case <-time.After(time.Second):
		t.Fatal("client did not close after process exit")
	}

	_, err = client.Call(context.Background(), map[string]any{"type": "get_state"})
	if err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("expected process exit error, got %v", err)
	}
}

func TestClientSendsUncorrelatedProtocolMessage(t *testing.T) {
	process := newFakeProcess()
	client := NewClientWithLimits(process, 1, nil, 4096, 1024)
	t.Cleanup(func() { _ = client.Close() })

	received := make(chan map[string]any, 1)
	go func() {
		received <- readRequest(t, bufio.NewReader(process.stdinReader))
	}()
	if err := client.Send(map[string]any{
		"type": "extension_ui_response", "id": "ui-1", "cancelled": true,
	}); err != nil {
		t.Fatalf("Send returned an error: %v", err)
	}

	request := <-received
	if request["type"] != "extension_ui_response" || request["id"] != "ui-1" || request["cancelled"] != true {
		t.Fatalf("unexpected protocol message: %#v", request)
	}
}
