package terminal

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

type fakeStarter struct {
	process *fakeProcess
	starts  int
	config  StartConfig
}

func (starter *fakeStarter) Start(_ context.Context, config StartConfig) (process, string, error) {
	starter.starts++
	starter.config = config
	return starter.process, "test-shell", nil
}

type fakeProcess struct {
	reader    *io.PipeReader
	output    *io.PipeWriter
	wait      chan struct{}
	stopOnce  sync.Once
	closeOnce sync.Once
	mu        sync.Mutex
	input     bytes.Buffer
	columns   int
	rows      int
	exitCode  int
}

func newFakeProcess() *fakeProcess {
	reader, output := io.Pipe()
	return &fakeProcess{reader: reader, output: output, wait: make(chan struct{})}
}

func (process *fakeProcess) Read(data []byte) (int, error) {
	return process.reader.Read(data)
}

func (process *fakeProcess) Write(data []byte) (int, error) {
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.input.Write(data)
}

func (process *fakeProcess) Resize(columns, rows int) error {
	process.mu.Lock()
	defer process.mu.Unlock()
	process.columns, process.rows = columns, rows
	return nil
}

func (process *fakeProcess) Wait() error {
	<-process.wait
	return nil
}

func (process *fakeProcess) Stop() error {
	process.stopOnce.Do(func() {
		close(process.wait)
		_ = process.output.Close()
	})
	return nil
}

func (process *fakeProcess) Close() error {
	process.closeOnce.Do(func() {
		_ = process.output.Close()
		_ = process.reader.Close()
	})
	return nil
}

func (process *fakeProcess) ExitCode() int { return process.exitCode }

func (process *fakeProcess) inputString() string {
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.input.String()
}

func TestManagerRunsOneTerminalPerThreadAndReplaysOutput(t *testing.T) {
	process := newFakeProcess()
	starter := &fakeStarter{process: process}
	events := make(chan Event, 4)
	manager := newManager(context.Background(), starter, func(event Event) { events <- event })
	t.Cleanup(manager.Shutdown)

	state, err := manager.Start(StartConfig{ThreadID: "thread-1", CWD: t.TempDir(), Columns: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	if !state.Running || state.Shell != "test-shell" || starter.starts != 1 {
		t.Fatalf("unexpected terminal state: %#v", state)
	}
	if _, err := manager.Start(StartConfig{ThreadID: "thread-1", CWD: starter.config.CWD, Columns: 100, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	if starter.starts != 1 || process.columns != 100 || process.rows != 30 {
		t.Fatalf("existing terminal was not reused and resized: starts=%d size=%dx%d", starter.starts, process.columns, process.rows)
	}

	if _, err := process.output.Write([]byte("hello\r\n")); err != nil {
		t.Fatal(err)
	}
	output := waitForTerminalEvent(t, events, "output")
	if string(output.Data) != "hello\r\n" || output.Sequence != 1 {
		t.Fatalf("unexpected output event: %#v", output)
	}
	state = manager.Snapshot("thread-1")
	if string(state.Output) != "hello\r\n" || state.Sequence != 1 {
		t.Fatalf("unexpected replay state: %#v", state)
	}

	if err := manager.Write("thread-1", []byte("pwd\r")); err != nil {
		t.Fatal(err)
	}
	if process.inputString() != "pwd\r" {
		t.Fatalf("unexpected terminal input %q", process.inputString())
	}
	if err := manager.Stop("thread-1"); err != nil {
		t.Fatal(err)
	}
	exit := waitForTerminalEvent(t, events, "exit")
	if exit.ExitCode != 0 || exit.Error != "" {
		t.Fatalf("unexpected exit event: %#v", exit)
	}
	if state := manager.Snapshot("thread-1"); state.Running {
		t.Fatal("stopped terminal remained registered")
	}
}

func TestManagerBoundsReplayAndValidatesRequests(t *testing.T) {
	running := &session{info: Snapshot{ThreadID: "thread-1", Running: true}}
	large := bytes.Repeat([]byte("a"), maxReplayBytes+512)
	running.appendOutput(large)
	state := running.snapshot()
	if len(state.Output) != maxReplayBytes || state.Sequence != 1 {
		t.Fatalf("replay buffer was not bounded: bytes=%d sequence=%d", len(state.Output), state.Sequence)
	}

	manager := newManager(context.Background(), &fakeStarter{process: newFakeProcess()}, nil)
	if _, err := manager.Start(StartConfig{ThreadID: "", CWD: t.TempDir(), Columns: 80, Rows: 24}); err == nil {
		t.Fatal("expected empty thread id to fail")
	}
	if _, err := manager.Start(StartConfig{ThreadID: "thread-1", CWD: t.TempDir(), Columns: 2, Rows: 2}); err == nil {
		t.Fatal("expected invalid dimensions to fail")
	}
	if err := manager.Write("missing", []byte("x")); err != ErrNotRunning {
		t.Fatalf("expected ErrNotRunning, got %v", err)
	}
	manager.mu.Lock()
	for index := 0; index < maxActiveSessions; index++ {
		threadID := string(rune('a' + index))
		manager.sessions[threadID] = &session{info: Snapshot{ThreadID: threadID, Running: true}}
	}
	manager.mu.Unlock()
	if _, err := manager.Start(StartConfig{ThreadID: "over-limit", CWD: t.TempDir(), Columns: 80, Rows: 24}); err != ErrLimitReached {
		t.Fatalf("expected ErrLimitReached, got %v", err)
	}
	manager.mu.Lock()
	manager.sessions = make(map[string]*session)
	manager.mu.Unlock()
	manager.Shutdown()
	if _, err := manager.Start(StartConfig{ThreadID: "thread-2", CWD: t.TempDir(), Columns: 80, Rows: 24}); err != ErrAlreadyClosed {
		t.Fatalf("expected ErrAlreadyClosed, got %v", err)
	}
}

func waitForTerminalEvent(t *testing.T, events <-chan Event, eventType string) Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Type == eventType {
				return event
			}
		case <-deadline:
			t.Fatalf("timed out waiting for terminal %s event", eventType)
		}
	}
}
