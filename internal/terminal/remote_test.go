package terminal

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"pi-desk/internal/remotessh"
)

type fakeRemoteTerminal struct {
	events   chan remotessh.RuntimeTerminalEvent
	mu       sync.Mutex
	replay   []byte
	sequence uint64
	input    []byte
	columns  int
	rows     int
	closed   bool
}

func newFakeRemoteTerminal() *fakeRemoteTerminal {
	return &fakeRemoteTerminal{events: make(chan remotessh.RuntimeTerminalEvent, 8)}
}
func (terminal *fakeRemoteTerminal) Events() <-chan remotessh.RuntimeTerminalEvent {
	return terminal.events
}
func (terminal *fakeRemoteTerminal) Replay() (uint64, []byte) {
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	return terminal.sequence, append([]byte(nil), terminal.replay...)
}
func (terminal *fakeRemoteTerminal) Input(data []byte) error {
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	terminal.input = append(terminal.input, data...)
	return nil
}
func (terminal *fakeRemoteTerminal) Resize(columns, rows int) error {
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	terminal.columns, terminal.rows = columns, rows
	return nil
}
func (terminal *fakeRemoteTerminal) Close() error {
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	terminal.closed = true
	return nil
}
func (terminal *fakeRemoteTerminal) setReplay(sequence uint64, value string) {
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	terminal.sequence, terminal.replay = sequence, []byte(value)
}

type fakeRemoteTerminalStarter struct{ terminal *fakeRemoteTerminal }

func (starter *fakeRemoteTerminalStarter) Start(context.Context, *remotessh.RuntimeLease, int, int) (remoteTerminalSession, error) {
	return starter.terminal, nil
}

func TestRemoteManagerProjectsIOReplayGapAndExit(t *testing.T) {
	remote := newFakeRemoteTerminal()
	events := make(chan Event, 8)
	manager := NewRemoteManager(context.Background(), func(event Event) { events <- event })
	if err := manager.bind("thread-1", &fakeRemoteTerminalStarter{terminal: remote}, &remotessh.RuntimeLease{}, "/srv/repository"); err != nil {
		t.Fatal(err)
	}
	started, err := manager.Start(StartConfig{ThreadID: "thread-1", CWD: "/srv/repository", Columns: 80, Rows: 24})
	if err != nil || !started.Running || started.Shell != "remote shell" {
		t.Fatalf("start=%#v err=%v", started, err)
	}
	if err := manager.Write("thread-1", []byte("echo test\n")); err != nil {
		t.Fatal(err)
	}
	if err := manager.Resize("thread-1", 100, 30); err != nil {
		t.Fatal(err)
	}
	remote.events <- remotessh.RuntimeTerminalEvent{Type: "output", Sequence: 1, Data: []byte("one")}
	first := waitRemoteManagerEvent(t, events)
	if first.Type != "output" || first.Generation != started.Generation || first.Sequence != 1 || string(first.Data) != "one" {
		t.Fatalf("first event=%#v", first)
	}
	remote.setReplay(3, "one-two-three")
	remote.events <- remotessh.RuntimeTerminalEvent{Type: "output", Sequence: 3, Data: []byte("three")}
	gap := waitRemoteManagerEvent(t, events)
	if gap.Sequence != 3 || len(gap.Data) != 0 {
		t.Fatalf("gap event=%#v", gap)
	}
	snapshot := manager.Snapshot("thread-1")
	if snapshot.Sequence != 3 || string(snapshot.Output) != "one-two-three" {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	remote.events <- remotessh.RuntimeTerminalEvent{Type: "exit", Sequence: 3, ExitCode: 4}
	close(remote.events)
	exit := waitRemoteManagerEvent(t, events)
	if exit.Type != "exit" || exit.ExitCode != 4 || exit.Sequence != 4 {
		t.Fatalf("exit=%#v", exit)
	}
	remote.mu.Lock()
	input, columns, rows := string(remote.input), remote.columns, remote.rows
	remote.mu.Unlock()
	if input != "echo test\n" || columns != 100 || rows != 30 {
		t.Fatalf("input=%q size=%dx%d", input, columns, rows)
	}
}

func TestRemoteManagerProjectsStableDisconnectWhenEventStreamCloses(t *testing.T) {
	remote := newFakeRemoteTerminal()
	events := make(chan Event, 1)
	manager := NewRemoteManager(context.Background(), func(event Event) { events <- event })
	if err := manager.bind("thread-1", &fakeRemoteTerminalStarter{terminal: remote}, &remotessh.RuntimeLease{}, "/srv/repository"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(StartConfig{ThreadID: "thread-1", CWD: "/srv/repository", Columns: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	close(remote.events)
	event := waitRemoteManagerEvent(t, events)
	if event.Type != "exit" || !strings.HasPrefix(event.Error, "REMOTE_DISCONNECTED:") {
		t.Fatalf("disconnect event=%#v", event)
	}
}

func TestRemoteManagerUnbindStopsAndRejectsSession(t *testing.T) {
	remote := newFakeRemoteTerminal()
	manager := NewRemoteManager(context.Background(), nil)
	if err := manager.bind("thread-1", &fakeRemoteTerminalStarter{terminal: remote}, &remotessh.RuntimeLease{}, "/srv/repository"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(StartConfig{ThreadID: "thread-1", CWD: "/srv/repository", Columns: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Unbind("thread-1"); err != nil {
		t.Fatal(err)
	}
	remote.mu.Lock()
	closed := remote.closed
	remote.mu.Unlock()
	if !closed {
		t.Fatal("unbind did not close remote terminal")
	}
	if _, err := manager.Start(StartConfig{ThreadID: "thread-1", CWD: "/srv/repository", Columns: 80, Rows: 24}); err == nil {
		t.Fatal("unbound terminal restarted")
	}
	if err := manager.Write("missing", []byte("x")); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("missing write error=%v", err)
	}
}

func TestRemoteManagerAllowsImmediateRestartAfterExit(t *testing.T) {
	firstRemote := newFakeRemoteTerminal()
	starter := &fakeRemoteTerminalStarter{terminal: firstRemote}
	events := make(chan Event, 2)
	manager := NewRemoteManager(context.Background(), func(event Event) { events <- event })
	if err := manager.bind("thread-1", starter, &remotessh.RuntimeLease{}, "/srv/repository"); err != nil {
		t.Fatal(err)
	}
	first, err := manager.Start(StartConfig{ThreadID: "thread-1", CWD: "/srv/repository", Columns: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	firstRemote.events <- remotessh.RuntimeTerminalEvent{Type: "exit", ExitCode: 0}
	if event := waitRemoteManagerEvent(t, events); event.Type != "exit" {
		t.Fatalf("exit event=%#v", event)
	}
	starter.terminal = newFakeRemoteTerminal()
	restarted, err := manager.Start(StartConfig{ThreadID: "thread-1", CWD: "/srv/repository", Columns: 80, Rows: 24})
	if err != nil {
		t.Fatalf("immediate restart failed: %v", err)
	}
	if restarted.Generation <= first.Generation {
		t.Fatalf("restart generation=%d first=%d", restarted.Generation, first.Generation)
	}
	close(firstRemote.events)
	close(starter.terminal.events)
}

func TestRemoteManagerUnbindAllowsImmediateRebind(t *testing.T) {
	oldRemote := newFakeRemoteTerminal()
	manager := NewRemoteManager(context.Background(), nil)
	if err := manager.bind("thread-1", &fakeRemoteTerminalStarter{terminal: oldRemote}, &remotessh.RuntimeLease{}, "/srv/repository"); err != nil {
		t.Fatal(err)
	}
	started, err := manager.Start(StartConfig{ThreadID: "thread-1", CWD: "/srv/repository", Columns: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Unbind("thread-1"); err != nil {
		t.Fatal(err)
	}
	newRemote := newFakeRemoteTerminal()
	if err := manager.bind("thread-1", &fakeRemoteTerminalStarter{terminal: newRemote}, &remotessh.RuntimeLease{}, "/srv/repository"); err != nil {
		t.Fatalf("immediate rebind failed: %v", err)
	}
	rebound, err := manager.Start(StartConfig{ThreadID: "thread-1", CWD: "/srv/repository", Columns: 80, Rows: 24})
	if err != nil {
		t.Fatalf("rebound terminal start failed: %v", err)
	}
	if started.Generation == 0 || rebound.Generation <= started.Generation {
		t.Fatalf("terminal generations did not advance: first=%d rebound=%d", started.Generation, rebound.Generation)
	}
	close(oldRemote.events)
	newRemote.events <- remotessh.RuntimeTerminalEvent{Type: "output", Sequence: 1, Data: []byte("new")}
	close(newRemote.events)
}

func waitRemoteManagerEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for remote terminal event")
		return Event{}
	}
}
