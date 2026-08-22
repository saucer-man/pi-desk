//go:build linux || darwin

package remotehelper

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"pi-desk/internal/remoteprotocol"
)

func openBashRoot(t *testing.T, harness *serverHarness, generation uint64) string {
	t.Helper()
	frame := harness.requestPayload(t, "root-bash", MethodRootOpen, generation, RootOpenRequest{Path: filepath.ToSlash(t.TempDir())})
	var response RootOpenResponse
	if frame.Envelope.Kind != remoteprotocol.KindResponse || remoteprotocol.DecodePayload(frame.Envelope.Payload, &response) != nil {
		t.Fatalf("open Bash root: %#v", frame.Envelope)
	}
	return response.Handle
}

func sendBashRequest(t *testing.T, harness *serverHarness, generation uint64, id, root, command string) {
	t.Helper()
	payload, err := remoteprotocol.EncodePayload(BashRunRequest{RootHandle: root, Command: command})
	if err != nil {
		t.Fatal(err)
	}
	harness.send(t, remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest, ID: id, Generation: generation, Method: MethodBashRun, Payload: payload})
}

func sendBashCredit(t *testing.T, harness *serverHarness, generation uint64, id string, bytes uint32) {
	t.Helper()
	payload, err := remoteprotocol.EncodePayload(remoteprotocol.StreamCredit{Bytes: bytes})
	if err != nil {
		t.Fatal(err)
	}
	harness.send(t, remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindEvent, ID: id, Generation: generation, Method: remoteprotocol.MethodStreamCredit, Payload: payload})
}

func TestServerBashStreamsOnlyWithCreditAndHasOneTerminal(t *testing.T) {
	const generation = 31
	harness := newServerHarness(t)
	harness.hello(t, generation)
	root := openBashRoot(t, harness, generation)
	sendBashRequest(t, harness, generation, "bash-1", root, `printf out; printf err >&2`)
	accepted := harness.read(t)
	if accepted.Envelope.Kind != remoteprotocol.KindEvent || accepted.Envelope.Method != remoteprotocol.MethodProcessAccepted {
		t.Fatalf("accepted=%#v", accepted.Envelope)
	}
	pending := make(chan remoteprotocol.Frame, 1)
	go func() {
		frame, _ := harness.reader.Read()
		pending <- frame
	}()
	select {
	case frame := <-pending:
		t.Fatalf("Bash emitted without credit: %#v", frame.Envelope)
	case <-time.After(100 * time.Millisecond):
	}
	sendBashCredit(t, harness, generation, "bash-1", 64<<10)
	var data remoteprotocol.Frame
	select {
	case data = <-pending:
	case <-time.After(5 * time.Second):
		t.Fatal("Bash stream did not resume after credit")
	}
	if data.Envelope.Kind != remoteprotocol.KindEvent || data.Envelope.Method != remoteprotocol.MethodStreamData || string(data.Blob) != "outerr" {
		t.Fatalf("stream=%#v blob=%q", data.Envelope, data.Blob)
	}
	sendBashCredit(t, harness, generation, "bash-1", uint32(len(data.Blob)))
	terminal := harness.read(t)
	var response BashRunResponse
	if terminal.Envelope.Kind != remoteprotocol.KindResponse || remoteprotocol.DecodePayload(terminal.Envelope.Payload, &response) != nil || response.ExitCode != 0 || response.OutputBytes != 6 {
		t.Fatalf("terminal=%#v response=%#v", terminal.Envelope, response)
	}
	harness.request(t, "shutdown-bash", MethodShutdown, generation)
	if err := harness.await(t); err != nil {
		t.Fatal(err)
	}
}

func TestServerBashCancelTerminatesProcessGroup(t *testing.T) {
	const generation = 32
	harness := newServerHarness(t)
	harness.hello(t, generation)
	root := openBashRoot(t, harness, generation)
	sendBashRequest(t, harness, generation, "bash-cancel", root, `trap '' INT TERM; while :; do sleep 1; done`)
	accepted := harness.read(t)
	if accepted.Envelope.Method != remoteprotocol.MethodProcessAccepted {
		t.Fatalf("accepted=%#v", accepted.Envelope)
	}
	harness.send(t, remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindCancel, ID: "bash-cancel", Generation: generation})
	terminal := harness.read(t)
	if terminal.Envelope.Kind != remoteprotocol.KindError || terminal.Envelope.Error == nil || terminal.Envelope.Error.Code != "REMOTE_CANCELLED" {
		t.Fatalf("cancel terminal=%#v", terminal.Envelope)
	}
	harness.request(t, "shutdown-cancel", MethodShutdown, generation)
	if err := harness.await(t); err != nil && err != context.Canceled {
		t.Fatal(err)
	}
}
