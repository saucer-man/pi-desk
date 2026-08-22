package piruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"pi-desk/internal/pirpc"
)

type supervisorFakeProcess struct {
	stdinReader  *io.PipeReader
	stdinWriter  *io.PipeWriter
	stdoutReader *io.PipeReader
	stdoutWriter *io.PipeWriter
	stderrReader *io.PipeReader
	stderrWriter *io.PipeWriter
	wait         chan error
	killed       chan struct{}
	exitOnce     sync.Once
	pid          int
}

func newSupervisorFakeProcess() *supervisorFakeProcess {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	return &supervisorFakeProcess{
		stdinReader: stdinReader, stdinWriter: stdinWriter,
		stdoutReader: stdoutReader, stdoutWriter: stdoutWriter,
		stderrReader: stderrReader, stderrWriter: stderrWriter,
		wait: make(chan error, 1), killed: make(chan struct{}), pid: 4242,
	}
}

func (process *supervisorFakeProcess) PID() int              { return process.pid }
func (process *supervisorFakeProcess) Stdin() io.WriteCloser { return process.stdinWriter }
func (process *supervisorFakeProcess) Stdout() io.Reader     { return process.stdoutReader }
func (process *supervisorFakeProcess) Stderr() io.Reader     { return process.stderrReader }
func (process *supervisorFakeProcess) Wait() error           { return <-process.wait }
func (process *supervisorFakeProcess) Kill() error {
	process.exitOnce.Do(func() {
		close(process.killed)
		process.wait <- pirpc.ErrClientClosed
	})
	return nil
}

func (process *supervisorFakeProcess) closeStreams() {
	_ = process.stdoutWriter.Close()
	_ = process.stderrWriter.Close()
	_ = process.stdinReader.Close()
}

type queuedStarter struct {
	mu        sync.Mutex
	processes []*supervisorFakeProcess
	started   chan struct{}
	release   chan struct{}
}

func (starter *queuedStarter) Start(context.Context, StartConfig) (pirpc.Process, error) {
	if starter.started != nil {
		select {
		case starter.started <- struct{}{}:
		default:
		}
	}
	if starter.release != nil {
		<-starter.release
	}
	starter.mu.Lock()
	defer starter.mu.Unlock()
	if len(starter.processes) == 0 {
		return nil, errors.New("no fake process queued")
	}
	process := starter.processes[0]
	starter.processes = starter.processes[1:]
	return process, nil
}

func TestSupervisorRejectsConcurrentStartBeforeLaunchingSecondProcess(t *testing.T) {
	process := newSupervisorFakeProcess()
	serveReady(t, process)
	starter := &queuedStarter{
		processes: []*supervisorFakeProcess{process},
		started:   make(chan struct{}, 1),
		release:   make(chan struct{}),
	}
	supervisor := NewSupervisor(context.Background(), starter, nil)
	t.Cleanup(func() {
		supervisor.Shutdown()
		process.closeStreams()
	})

	firstResult := make(chan error, 1)
	go func() {
		_, err := supervisor.Start(context.Background(), StartConfig{ThreadID: "thread-1"})
		firstResult <- err
	}()
	<-starter.started
	if supervisor.ActiveCount() != 1 {
		t.Fatalf("starting process was not counted as active: %d", supervisor.ActiveCount())
	}

	if _, err := supervisor.Start(context.Background(), StartConfig{ThreadID: "thread-1"}); !errors.Is(err, ErrThreadRunning) {
		t.Fatalf("expected concurrent start to fail with ErrThreadRunning, got %v", err)
	}
	close(starter.release)
	if err := <-firstResult; err != nil {
		t.Fatalf("first start failed: %v", err)
	}
	if supervisor.ActiveCount() != 1 {
		t.Fatalf("ready process was not counted as active: %d", supervisor.ActiveCount())
	}
}

func serveReady(t *testing.T, process *supervisorFakeProcess) {
	t.Helper()
	go func() {
		line, err := bufio.NewReader(process.stdinReader).ReadBytes('\n')
		if err != nil {
			t.Errorf("read readiness request: %v", err)
			return
		}
		var request map[string]any
		if err := json.Unmarshal(line, &request); err != nil {
			t.Errorf("decode readiness request: %v", err)
			return
		}
		response := map[string]any{
			"id": request["id"], "type": "response", "command": "get_state", "success": true,
			"data": map[string]any{"sessionId": "session-1", "isStreaming": false},
		}
		record, _ := pirpc.EncodeRecord(response, 4096)
		_, _ = process.stdoutWriter.Write(record)
	}()
}

func TestSupervisorRejectsStopWhileProcessIsStillStarting(t *testing.T) {
	process := newSupervisorFakeProcess()
	serveReady(t, process)
	starter := &queuedStarter{
		processes: []*supervisorFakeProcess{process},
		started:   make(chan struct{}, 1),
		release:   make(chan struct{}),
	}
	supervisor := NewSupervisor(context.Background(), starter, nil)
	t.Cleanup(func() {
		supervisor.Shutdown()
		process.closeStreams()
	})

	started := make(chan error, 1)
	go func() {
		_, err := supervisor.Start(context.Background(), StartConfig{ThreadID: "thread-1"})
		started <- err
	}()
	<-starter.started
	if err := supervisor.Stop("thread-1"); !errors.Is(err, ErrThreadRunning) {
		t.Fatalf("Stop during startup error = %v, want ErrThreadRunning", err)
	}
	close(starter.release)
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Stop("thread-1"); err != nil {
		t.Fatalf("Stop after startup failed: %v", err)
	}
}

func TestSupervisorRejectsLateStarterCompletionAfterShutdown(t *testing.T) {
	process := newSupervisorFakeProcess()
	starter := &queuedStarter{
		processes: []*supervisorFakeProcess{process},
		started:   make(chan struct{}, 1),
		release:   make(chan struct{}),
	}
	supervisor := NewSupervisor(context.Background(), starter, nil)
	t.Cleanup(process.closeStreams)

	result := make(chan error, 1)
	go func() {
		_, err := supervisor.Start(context.Background(), StartConfig{ThreadID: "thread-1"})
		result <- err
	}()
	<-starter.started
	supervisor.Shutdown()
	close(starter.release)
	if err := <-result; !errors.Is(err, ErrSupervisorClosed) {
		t.Fatalf("late starter completion error = %v, want ErrSupervisorClosed", err)
	}
	if supervisor.ActiveCount() != 0 || len(supervisor.Running()) != 0 {
		t.Fatalf("late process registered after shutdown: active=%d running=%#v", supervisor.ActiveCount(), supervisor.Running())
	}
	select {
	case <-process.killed:
	case <-time.After(time.Second):
		t.Fatal("late process was not closed after shutdown")
	}
	if _, err := supervisor.Start(context.Background(), StartConfig{ThreadID: "thread-2"}); !errors.Is(err, ErrSupervisorClosed) {
		t.Fatalf("start after shutdown error = %v, want ErrSupervisorClosed", err)
	}
}

func TestSupervisorStartsCallsAndStopsSession(t *testing.T) {
	process := newSupervisorFakeProcess()
	serveReady(t, process)
	starter := &queuedStarter{processes: []*supervisorFakeProcess{process}}
	events := make(chan SessionEvent, 4)
	supervisor := NewSupervisor(context.Background(), starter, func(event SessionEvent) { events <- event })
	t.Cleanup(func() {
		supervisor.Shutdown()
		process.closeStreams()
	})

	info, err := supervisor.Start(context.Background(), StartConfig{ThreadID: "thread-1"})
	if err != nil {
		t.Fatalf("Start returned an error: %v", err)
	}
	if info.Generation == 0 || len(info.State) == 0 || info.ProcessID != process.pid {
		t.Fatalf("unexpected session info: %#v", info)
	}
	if len(supervisor.Running()) != 1 {
		t.Fatal("running session was not registered")
	}
	if supervisor.ActiveCount() != 1 {
		t.Fatal("running session was not counted as active")
	}
	if _, err := supervisor.Start(context.Background(), StartConfig{ThreadID: "thread-1"}); !errors.Is(err, ErrThreadRunning) {
		t.Fatalf("expected ErrThreadRunning, got %v", err)
	}
	if err := supervisor.Stop("thread-1"); err != nil {
		t.Fatalf("Stop returned an error: %v", err)
	}
	if len(supervisor.Running()) != 0 {
		t.Fatal("stopped session remains registered")
	}
	if supervisor.ActiveCount() != 0 {
		t.Fatal("stopped session remains active")
	}
}

func TestSupervisorStopsAllSessionsWithoutShuttingDown(t *testing.T) {
	first := newSupervisorFakeProcess()
	second := newSupervisorFakeProcess()
	serveReady(t, first)
	serveReady(t, second)
	supervisor := NewSupervisor(context.Background(), &queuedStarter{processes: []*supervisorFakeProcess{first, second}}, nil)
	t.Cleanup(func() {
		supervisor.Shutdown()
		first.closeStreams()
		second.closeStreams()
	})

	for _, threadID := range []string{"thread-1", "thread-2"} {
		if _, err := supervisor.Start(context.Background(), StartConfig{ThreadID: threadID}); err != nil {
			t.Fatal(err)
		}
	}
	if err := supervisor.StopAll(); err != nil {
		t.Fatal(err)
	}
	if supervisor.ActiveCount() != 0 || len(supervisor.Running()) != 0 {
		t.Fatalf("sessions remain after StopAll: active=%d running=%#v", supervisor.ActiveCount(), supervisor.Running())
	}
}

func TestSupervisorBlocksRestartUntilNaturalExitIsProjected(t *testing.T) {
	oldProcess := newSupervisorFakeProcess()
	newProcess := newSupervisorFakeProcess()
	serveReady(t, oldProcess)
	serveReady(t, newProcess)
	entered := make(chan struct{})
	release := make(chan struct{})
	supervisor := NewSupervisor(context.Background(), &queuedStarter{processes: []*supervisorFakeProcess{oldProcess, newProcess}}, func(event SessionEvent) {
		if event.Event.Type == "runtime_exit" {
			close(entered)
			<-release
		}
	})
	t.Cleanup(func() {
		supervisor.Shutdown()
		oldProcess.closeStreams()
		newProcess.closeStreams()
	})

	if _, err := supervisor.Start(context.Background(), StartConfig{ThreadID: "thread-1"}); err != nil {
		t.Fatal(err)
	}
	oldProcess.wait <- errors.New("process exited")
	_ = oldProcess.stdoutWriter.Close()
	<-entered
	if supervisor.ActiveCount() != 1 {
		t.Fatalf("natural exit projection was not counted as active: %d", supervisor.ActiveCount())
	}
	if _, err := supervisor.Start(context.Background(), StartConfig{ThreadID: "thread-1"}); !errors.Is(err, ErrThreadRunning) {
		t.Fatalf("restart raced natural exit projection: %v", err)
	}
	close(release)
	for supervisor.ActiveCount() != 0 {
		time.Sleep(time.Millisecond)
	}
	if _, err := supervisor.Start(context.Background(), StartConfig{ThreadID: "thread-1"}); err != nil {
		t.Fatalf("restart after exit projection failed: %v", err)
	}
}

func TestSupervisorDropsEventsFromReplacedGeneration(t *testing.T) {
	oldProcess := newSupervisorFakeProcess()
	newProcess := newSupervisorFakeProcess()
	serveReady(t, oldProcess)
	serveReady(t, newProcess)
	starter := &queuedStarter{processes: []*supervisorFakeProcess{oldProcess, newProcess}}
	events := make(chan SessionEvent, 8)
	supervisor := NewSupervisor(context.Background(), starter, func(event SessionEvent) { events <- event })
	t.Cleanup(func() {
		supervisor.Shutdown()
		oldProcess.closeStreams()
		newProcess.closeStreams()
	})

	oldInfo, err := supervisor.Start(context.Background(), StartConfig{ThreadID: "thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Stop("thread-1"); err != nil {
		t.Fatal(err)
	}
	newInfo, err := supervisor.Start(context.Background(), StartConfig{ThreadID: "thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	if newInfo.Generation <= oldInfo.Generation {
		t.Fatalf("generation did not advance: old=%d new=%d", oldInfo.Generation, newInfo.Generation)
	}

	oldEvent, _ := pirpc.EncodeRecord(map[string]any{"type": "agent_start"}, 4096)
	newEvent, _ := pirpc.EncodeRecord(map[string]any{"type": "message_start"}, 4096)
	_, _ = oldProcess.stdoutWriter.Write(oldEvent)
	_, _ = newProcess.stdoutWriter.Write(newEvent)

	select {
	case event := <-events:
		if event.Event.Type != "message_start" || event.Event.Generation != newInfo.Generation {
			t.Fatalf("stale event escaped generation gate: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for current generation event")
	}

	select {
	case event := <-events:
		t.Fatalf("unexpected additional event: %#v", event)
	case <-time.After(20 * time.Millisecond):
	}
}
