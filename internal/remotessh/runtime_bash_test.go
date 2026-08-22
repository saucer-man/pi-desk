package remotessh

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"pi-desk/internal/remotehelper"
	"pi-desk/internal/remoteprotocol"
)

func TestRuntimeLeaseSupervisorBashRequiresTaskLease(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	task, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "bash-task"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := supervisor.RunBash(context.Background(), task, "printf ok")
	if err != nil || result.Output != "ok\n" {
		t.Fatalf("Bash=%#v err=%v", result, err)
	}
	task.Release()
	readLease, err := supervisor.AcquireRead(context.Background(), runtimeRequest(root, "bash-read"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.RunBash(context.Background(), readLease, "printf no"); !errors.Is(err, ErrHelperRuntimeInvalidLease) {
		t.Fatalf("read lease Bash error=%v", err)
	}
	if connection.Snapshot().State != ConnectionReady {
		t.Fatalf("rejected Bash revoked connection: %#v", connection.Snapshot())
	}
	readLease.Release()
	_ = supervisor.Close(context.Background())
}

func TestInstalledHelperGenerationBashDisconnectBeforeAcceptedIsOutcomeUnknown(t *testing.T) {
	runtimeInput, serverOutput := io.Pipe()
	serverInput, runtimeOutput := io.Pipe()
	runtimeProcess := &installedHelperGeneration{
		generation: 44, stdin: runtimeOutput,
		writer:   remoteprotocol.NewWriter(runtimeOutput, remoteprotocol.Limits{}),
		reader:   remoteprotocol.NewReader(runtimeInput, remoteprotocol.Limits{}),
		cancel:   func() { _ = runtimeOutput.Close(); _ = runtimeInput.Close() },
		readDone: make(chan struct{}), pending: make(map[string]chan helperCallResult),
	}
	runtimeProcess.startReader()
	serverDone := make(chan error, 1)
	go func() {
		reader := remoteprotocol.NewReader(serverInput, remoteprotocol.Limits{})
		request, err := reader.Read()
		if err == nil && request.Envelope.Method != remotehelper.MethodBashRun {
			err = errors.New("missing Bash request")
		}
		if err == nil {
			_, err = reader.Read()
		}
		_ = serverOutput.Close()
		serverDone <- err
	}()
	_, err := runtimeProcess.RunBash(context.Background(), "root-0123456789abcdef0123456789abcdef", "touch changed")
	if !errors.Is(err, ErrRuntimeOutcomeUnknown) {
		t.Fatalf("pre-accepted Bash disconnect error=%v", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	_ = serverInput.Close()
}

func TestInstalledHelperGenerationBashRoutesAcceptedStreamCreditAndTerminal(t *testing.T) {
	runtimeInput, serverOutput := io.Pipe()
	serverInput, runtimeOutput := io.Pipe()
	runtimeProcess := &installedHelperGeneration{
		generation: 44, stdin: runtimeOutput,
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
		credit, err := reader.Read()
		if err != nil || credit.Envelope.Method != remoteprotocol.MethodStreamCredit {
			serverDone <- errors.New("missing initial stream credit")
			return
		}
		accepted, _ := remoteprotocol.EncodePayload(remoteprotocol.ProcessAccepted{ProcessID: "process-0123456789abcdef0123456789abcdef"})
		err = writer.Write(remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindEvent, ID: request.Envelope.ID, Generation: 44, Method: remoteprotocol.MethodProcessAccepted, Payload: accepted}, nil)
		data, _ := remoteprotocol.EncodePayload(remoteprotocol.StreamData{Stream: "combined", Sequence: 1})
		if err == nil {
			err = writer.Write(remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindEvent, ID: request.Envelope.ID, Generation: 44, Method: remoteprotocol.MethodStreamData, Payload: data}, []byte("ok\xff"))
		}
		if err == nil {
			_, err = reader.Read()
		}
		terminal, _ := remoteprotocol.EncodePayload(remotehelper.BashRunResponse{ExitCode: 7, OutputBytes: 3})
		if err == nil {
			err = writer.Write(remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindResponse, ID: request.Envelope.ID, Generation: 44, Payload: terminal}, nil)
		}
		serverDone <- err
	}()
	result, err := runtimeProcess.RunBash(context.Background(), "root-0123456789abcdef0123456789abcdef", "printf ok")
	if err != nil || result.ExitCode != 7 || result.OutputBytes != 3 || result.Output != "ok�" {
		t.Fatalf("Bash=%#v err=%v", result, err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	_ = serverInput.Close()
	_ = serverOutput.Close()
}
