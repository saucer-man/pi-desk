//go:build linux || darwin

package remotehelper

import (
	"bytes"
	"path/filepath"
	"testing"

	"pi-desk/internal/remoteprotocol"
)

func TestServerTerminalStreamsInputResizeAndExit(t *testing.T) {
	const generation = 35
	harness := newServerHarness(t)
	harness.hello(t, generation)
	rootFrame := harness.requestPayload(t, "root-terminal", MethodRootOpen, generation, RootOpenRequest{Path: filepath.ToSlash(t.TempDir())})
	var root RootOpenResponse
	if remoteprotocol.DecodePayload(rootFrame.Envelope.Payload, &root) != nil {
		t.Fatalf("root=%#v", rootFrame.Envelope)
	}
	payload, err := remoteprotocol.EncodePayload(TerminalRunRequest{RootHandle: root.Handle, Columns: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	harness.send(t, remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest, ID: "terminal-1", Generation: generation, Method: MethodTerminalRun, Payload: payload})
	sendBashCredit(t, harness, generation, "terminal-1", 32<<10)
	accepted := harness.read(t)
	if accepted.Envelope.Method != remoteprotocol.MethodProcessAccepted {
		t.Fatalf("accepted=%#v", accepted.Envelope)
	}
	resize, _ := remoteprotocol.EncodePayload(remoteprotocol.TerminalResize{Columns: 100, Rows: 30})
	harness.send(t, remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindEvent, ID: "terminal-1", Generation: generation, Method: remoteprotocol.MethodTerminalResize, Payload: resize})
	if err := harness.writer.Write(remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindEvent, ID: "terminal-1", Generation: generation, Method: remoteprotocol.MethodTerminalInput}, []byte("printf 'terminal-output'; exit 3\n")); err != nil {
		t.Fatal(err)
	}
	var output []byte
	for {
		frame := harness.read(t)
		if frame.Envelope.Kind == remoteprotocol.KindEvent {
			output = append(output, frame.Blob...)
			sendBashCredit(t, harness, generation, "terminal-1", uint32(len(frame.Blob)))
			continue
		}
		var response TerminalRunResponse
		if frame.Envelope.Kind != remoteprotocol.KindResponse || remoteprotocol.DecodePayload(frame.Envelope.Payload, &response) != nil || response.ExitCode != 3 || response.OutputBytes != int64(len(output)) {
			t.Fatalf("terminal=%#v response=%#v output=%q", frame.Envelope, response, output)
		}
		break
	}
	if !bytes.Contains(output, []byte("terminal-output")) {
		t.Fatalf("terminal output=%q", output)
	}
	harness.request(t, "shutdown-terminal", MethodShutdown, generation)
	if err := harness.await(t); err != nil {
		t.Fatal(err)
	}
}
