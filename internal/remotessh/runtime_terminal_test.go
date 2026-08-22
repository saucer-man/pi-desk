package remotessh

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"pi-desk/internal/remotehelper"
	"pi-desk/internal/remoteprotocol"
)

func TestRuntimeLeaseSupervisorTerminalRequiresTaskLease(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	task, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "terminal-task"))
	if err != nil {
		t.Fatal(err)
	}
	session, err := supervisor.StartTerminal(context.Background(), task, 80, 24)
	if err != nil || session.ProcessID() == "" {
		t.Fatalf("terminal=%#v err=%v", session, err)
	}
	task.Release()
	readLease, err := supervisor.AcquireRead(context.Background(), runtimeRequest(root, "terminal-read"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.StartTerminal(context.Background(), readLease, 80, 24); !errors.Is(err, ErrHelperRuntimeInvalidLease) {
		t.Fatalf("read lease Terminal error=%v", err)
	}
	if connection.Snapshot().State != ConnectionReady {
		t.Fatalf("rejected Terminal revoked connection: %#v", connection.Snapshot())
	}
	readLease.Release()
	_ = supervisor.Close(context.Background())
}

func TestInstalledHelperGenerationTerminalInputResizeReplayAndExit(t *testing.T) {
	runtimeInput, serverOutput := io.Pipe()
	serverInput, runtimeOutput := io.Pipe()
	runtimeProcess := &installedHelperGeneration{
		generation: 45, stdin: runtimeOutput,
		writer:   remoteprotocol.NewWriter(runtimeOutput, remoteprotocol.Limits{}),
		reader:   remoteprotocol.NewReader(runtimeInput, remoteprotocol.Limits{}),
		cancel:   func() { _ = runtimeOutput.Close(); _ = runtimeInput.Close() },
		readDone: make(chan struct{}), pending: make(map[string]chan helperCallResult),
	}
	runtimeProcess.startReader()
	serverDone := make(chan error, 1)
	go func() {
		reader := remoteprotocol.NewReader(serverInput, remoteprotocol.Limits{})
		writer := remoteprotocol.NewWriter(serverOutput, remoteprotocol.Limits{})
		request, err := reader.Read()
		if err != nil {
			serverDone <- err
			return
		}
		if _, err = reader.Read(); err != nil { // initial credit
			serverDone <- err
			return
		}
		accepted, _ := remoteprotocol.EncodePayload(remoteprotocol.ProcessAccepted{ProcessID: "process-0123456789abcdef0123456789abcdef"})
		if err = writer.Write(remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindEvent, ID: request.Envelope.ID, Generation: 45, Method: remoteprotocol.MethodProcessAccepted, Payload: accepted}, nil); err != nil {
			serverDone <- err
			return
		}
		resize, err := reader.Read()
		if err != nil || resize.Envelope.Method != remoteprotocol.MethodTerminalResize {
			serverDone <- errors.New("missing Terminal resize")
			return
		}
		input, err := reader.Read()
		if err != nil || input.Envelope.Method != remoteprotocol.MethodTerminalInput || string(input.Blob) != "echo ok\n" {
			serverDone <- errors.New("missing Terminal input")
			return
		}
		data, _ := remoteprotocol.EncodePayload(remoteprotocol.StreamData{Stream: "terminal", Sequence: 1})
		if err = writer.Write(remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindEvent, ID: request.Envelope.ID, Generation: 45, Method: remoteprotocol.MethodStreamData, Payload: data}, []byte("terminal\r\n")); err != nil {
			serverDone <- err
			return
		}
		if _, err = reader.Read(); err != nil { // returned credit
			serverDone <- err
			return
		}
		terminal, _ := remoteprotocol.EncodePayload(remotehelper.TerminalRunResponse{ExitCode: 0, OutputBytes: 10})
		err = writer.Write(remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindResponse, ID: request.Envelope.ID, Generation: 45, Payload: terminal}, nil)
		serverDone <- err
	}()
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := runtimeProcess.StartTerminal(context.Background(), lifetime, "root-0123456789abcdef0123456789abcdef", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	if err := session.Input([]byte("echo ok\n")); err != nil {
		t.Fatal(err)
	}
	var events []RuntimeTerminalEvent
	for event := range session.Events() {
		events = append(events, event)
	}
	if len(events) != 2 || events[0].Type != "output" || events[1].Type != "exit" || events[1].ExitCode != 0 {
		t.Fatalf("events=%#v", events)
	}
	sequence, replay := session.Replay()
	if sequence != 1 || !bytes.Equal(replay, []byte("terminal\r\n")) {
		t.Fatalf("replay sequence=%d data=%q", sequence, replay)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	_ = serverInput.Close()
	_ = serverOutput.Close()
}
