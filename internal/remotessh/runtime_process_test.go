package remotessh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"pi-desk/internal/remotehelper"
	"pi-desk/internal/remoteprotocol"
)

func TestInstalledHelperGenerationHandshakeAndShutdown(t *testing.T) {
	clientInput, serverOutput := io.Pipe()
	serverInput, clientOutput := io.Pipe()
	server, err := remotehelper.NewServer(serverInput, serverOutput, remotehelper.Config{BuildHash: "runtime-test-build"})
	if err != nil {
		t.Fatal(err)
	}
	serverResult := make(chan error, 1)
	go func() {
		serverResult <- server.Serve(context.Background())
	}()
	done := make(chan struct{})
	runtimeProcess := &installedHelperGeneration{
		generation: 9,
		stdin:      clientOutput,
		writer:     remoteprotocol.NewWriter(clientOutput, remoteprotocol.Limits{}),
		reader:     remoteprotocol.NewReader(clientInput, remoteprotocol.Limits{}),
		stderr:     &boundedOutput{limit: maxConnectionProbeStderrBytes},
		cancel: func() {
			_ = clientOutput.Close()
			_ = clientInput.Close()
		},
		done: done,
	}
	go func() {
		err := <-serverResult
		runtimeProcess.waitMu.Lock()
		runtimeProcess.waitErr = err
		runtimeProcess.waitMu.Unlock()
		close(done)
	}()
	artifact := HelperArtifact{
		ProtocolVersion: remoteprotocol.Version,
		BuildIdentity:   "runtime-test-build",
		OS:              runtime.GOOS,
		Architecture:    runtime.GOARCH,
	}
	identity, err := runtimeProcess.handshake(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if identity.BuildIdentity != artifact.BuildIdentity || identity.OS != runtime.GOOS || identity.Architecture != runtime.GOARCH || !sameCapabilities(identity.Capabilities, requiredHelperCapabilities()) {
		t.Fatalf("runtime identity = %#v", identity)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtimeProcess.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtimeProcess.Done():
	default:
		t.Fatal("shutdown returned before helper exit")
	}
	if err := runtimeProcess.Shutdown(ctx); err != nil {
		t.Fatalf("idempotent process shutdown = %v", err)
	}
}

func TestDecodeRootOpenResponseIsStrictAndProjectsStableErrors(t *testing.T) {
	payload, err := remoteprotocol.EncodePayload(remotehelper.RootOpenResponse{
		Handle:        "root-0123456789abcdef0123456789abcdef",
		CanonicalPath: "/srv/repository", Device: 7, Inode: 11,
	})
	if err != nil {
		t.Fatal(err)
	}
	frame := remoteprotocol.Frame{Envelope: remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindResponse,
		ID: "root-open-1", Generation: 4, Payload: payload,
	}}
	response, err := decodeRootOpenResponse(frame, "root-open-1", 4)
	if err != nil || response.Handle == "" || response.CanonicalPath != "/srv/repository" || response.Device != 7 || response.Inode != 11 {
		t.Fatalf("root response=%#v err=%v", response, err)
	}
	for code, expected := range map[string]error{
		"REMOTE_INVALID_REQUEST":         ErrHelperRootInvalid,
		"REMOTE_ROOT_OPEN_FAILED":        ErrHelperRootOpen,
		"REMOTE_UNSUPPORTED_FILE_LAYOUT": ErrHelperRootUnsupported,
		"REMOTE_RESOURCE_LIMIT":          ErrHelperRuntimeLimit,
		"REMOTE_CANCELLED":               context.Canceled,
	} {
		errorFrame := remoteprotocol.Frame{Envelope: remoteprotocol.Envelope{
			Version: remoteprotocol.Version, Kind: remoteprotocol.KindError,
			ID: "root-open-1", Generation: 4,
			Error: &remoteprotocol.RemoteError{Code: code, Message: "untrusted /secret/path"},
		}}
		_, err := decodeRootOpenResponse(errorFrame, "root-open-1", 4)
		if !errors.Is(err, expected) || strings.Contains(err.Error(), "/secret/path") {
			t.Fatalf("root error %s = %v", code, err)
		}
	}
	unknown := frame
	unknown.Envelope.Kind = remoteprotocol.KindError
	unknown.Envelope.Payload = nil
	unknown.Envelope.Error = &remoteprotocol.RemoteError{Code: "REMOTE_FUTURE", Message: "future"}
	if _, err := decodeRootOpenResponse(unknown, "root-open-1", 4); !errors.Is(err, ErrHelperProtocolMismatch) {
		t.Fatalf("unknown root error = %v", err)
	}
}

func TestInstalledHelperGenerationRejectsIdentityMismatch(t *testing.T) {
	clientInput, serverOutput := io.Pipe()
	serverInput, clientOutput := io.Pipe()
	server, err := remotehelper.NewServer(serverInput, serverOutput, remotehelper.Config{BuildHash: "actual-build"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Serve(ctx) }()
	runtimeProcess := &installedHelperGeneration{
		generation: 7,
		stdin:      clientOutput,
		writer:     remoteprotocol.NewWriter(clientOutput, remoteprotocol.Limits{}),
		reader:     remoteprotocol.NewReader(clientInput, remoteprotocol.Limits{}),
		stderr:     &boundedOutput{limit: maxConnectionProbeStderrBytes},
		cancel:     cancel,
		done:       make(chan struct{}),
	}
	artifact := HelperArtifact{
		ProtocolVersion: remoteprotocol.Version,
		BuildIdentity:   "different-build",
		OS:              runtime.GOOS,
		Architecture:    runtime.GOARCH,
	}
	if _, err := runtimeProcess.handshake(artifact); !errors.Is(err, ErrHelperRuntimeIdentity) {
		t.Fatalf("identity mismatch error = %v", err)
	}
	_ = clientOutput.Close()
	_ = clientInput.Close()
}

func TestInstalledHelperGenerationRoutesConcurrentResponsesByID(t *testing.T) {
	runtimeInput, serverOutput := io.Pipe()
	serverInput, runtimeOutput := io.Pipe()
	var closeOnce sync.Once
	runtimeProcess := &installedHelperGeneration{
		generation: 21,
		stdin:      runtimeOutput,
		writer:     remoteprotocol.NewWriter(runtimeOutput, remoteprotocol.Limits{}),
		reader:     remoteprotocol.NewReader(runtimeInput, remoteprotocol.Limits{}),
		cancel:     func() { closeOnce.Do(func() { _ = runtimeOutput.Close(); _ = runtimeInput.Close() }) },
		readDone:   make(chan struct{}), pending: make(map[string]chan helperCallResult),
	}
	runtimeProcess.startReader()
	firstReceived := make(chan struct{})
	serverDone := make(chan error, 1)
	go func() {
		reader := remoteprotocol.NewReader(serverInput, remoteprotocol.Limits{})
		writer := remoteprotocol.NewWriter(serverOutput, remoteprotocol.Limits{})
		first, err := reader.Read()
		if err != nil {
			serverDone <- err
			return
		}
		close(firstReceived)
		second, err := reader.Read()
		if err != nil {
			serverDone <- err
			return
		}
		payload, _ := remoteprotocol.EncodePayload(remotehelper.PingResponse{OK: true})
		for _, request := range []remoteprotocol.Frame{second, first} {
			if err := writer.Write(remoteprotocol.Envelope{
				Version: remoteprotocol.Version, Kind: remoteprotocol.KindResponse,
				ID: request.Envelope.ID, Generation: 21, Payload: payload,
			}, nil); err != nil {
				serverDone <- err
				return
			}
		}
		serverDone <- nil
	}()
	type callResult struct {
		frame remoteprotocol.Frame
		id    string
		err   error
	}
	results := make(chan callResult, 2)
	go func() {
		frame, id, err := runtimeProcess.call(context.Background(), "concurrent", remotehelper.MethodPing, nil, false)
		results <- callResult{frame: frame, id: id, err: err}
	}()
	<-firstReceived
	go func() {
		frame, id, err := runtimeProcess.call(context.Background(), "concurrent", remotehelper.MethodPing, nil, false)
		results <- callResult{frame: frame, id: id, err: err}
	}()
	for range 2 {
		result := <-results
		if result.err != nil || result.frame.Envelope.ID != result.id {
			t.Fatalf("multiplexed call=%#v", result)
		}
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	runtimeProcess.cancel()
	_ = serverInput.Close()
	_ = serverOutput.Close()
}

func TestInstalledHelperGenerationSendsCancelAndWaitsForTerminal(t *testing.T) {
	runtimeInput, serverOutput := io.Pipe()
	serverInput, runtimeOutput := io.Pipe()
	var closeOnce sync.Once
	killed := false
	runtimeProcess := &installedHelperGeneration{
		generation: 22,
		stdin:      runtimeOutput,
		writer:     remoteprotocol.NewWriter(runtimeOutput, remoteprotocol.Limits{}),
		reader:     remoteprotocol.NewReader(runtimeInput, remoteprotocol.Limits{}),
		cancel: func() {
			killed = true
			closeOnce.Do(func() { _ = runtimeOutput.Close(); _ = runtimeInput.Close() })
		},
		readDone: make(chan struct{}), pending: make(map[string]chan helperCallResult),
	}
	runtimeProcess.startReader()
	requestReceived := make(chan struct{})
	serverDone := make(chan error, 1)
	go func() {
		reader := remoteprotocol.NewReader(serverInput, remoteprotocol.Limits{})
		writer := remoteprotocol.NewWriter(serverOutput, remoteprotocol.Limits{})
		request, err := reader.Read()
		if err != nil {
			serverDone <- err
			return
		}
		close(requestReceived)
		cancel, err := reader.Read()
		if err != nil {
			serverDone <- err
			return
		}
		if cancel.Envelope.Kind != remoteprotocol.KindCancel || cancel.Envelope.ID != request.Envelope.ID {
			serverDone <- errors.New("host cancel did not target the active request")
			return
		}
		serverDone <- writer.Write(remoteprotocol.Envelope{
			Version: remoteprotocol.Version, Kind: remoteprotocol.KindError,
			ID: request.Envelope.ID, Generation: 22,
			Error: &remoteprotocol.RemoteError{Code: "REMOTE_CANCELLED", Message: "cancelled"},
		}, nil)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		var response remotehelper.FileInfoResponse
		result <- runtimeProcess.requestFile(ctx, remotehelper.MethodFileStat, remotehelper.FileRequest{
			RootHandle: "root-0123456789abcdef0123456789abcdef", Path: "file.txt",
		}, &response)
	}()
	<-requestReceived
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled file request error=%v", err)
	}
	if killed {
		t.Fatal("confirmed request cancellation killed the helper generation")
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	runtimeProcess.cancel()
	_ = serverInput.Close()
	_ = serverOutput.Close()
}

func TestDecodeMutationResponseRequiresExplicitOutcomeFlag(t *testing.T) {
	payload, err := remoteprotocol.EncodePayload(remotehelper.FileMkdirResponse{Path: "nested", Created: []string{"nested"}})
	if err != nil {
		t.Fatal(err)
	}
	frame := remoteprotocol.Frame{Envelope: remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindResponse,
		ID: "mkdir-1", Generation: 6, Payload: payload,
	}}
	var response remotehelper.FileMkdirResponse
	if err := decodeMutationResponse(frame, "mkdir-1", 6, &response); err != nil || response.Path != "nested" {
		t.Fatalf("mkdir response=%#v err=%v", response, err)
	}
	for _, outcomeFlag := range []bool{false, true} {
		errorFrame := remoteprotocol.Frame{Envelope: remoteprotocol.Envelope{
			Version: remoteprotocol.Version, Kind: remoteprotocol.KindError,
			ID: "mkdir-1", Generation: 6,
			Error: &remoteprotocol.RemoteError{Code: "REMOTE_OUTCOME_UNKNOWN", Message: "unknown", OutcomeUnknown: outcomeFlag},
		}}
		err := decodeMutationResponse(errorFrame, "mkdir-1", 6, &response)
		if outcomeFlag && !errors.Is(err, ErrRuntimeOutcomeUnknown) || !outcomeFlag && !errors.Is(err, ErrHelperProtocolMismatch) {
			t.Fatalf("outcome flag=%v err=%v", outcomeFlag, err)
		}
	}
}

func TestDecodeWriteResponseProjectsConditionalResults(t *testing.T) {
	content := []byte("new")
	digest := sha256.Sum256(content)
	payload, err := remoteprotocol.EncodePayload(remotehelper.FileWriteResponse{
		Path: "new.txt", Size: int64(len(content)), SHA256: hex.EncodeToString(digest[:]), Created: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	frame := remoteprotocol.Frame{Envelope: remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindResponse,
		ID: "write-1", Generation: 6, Payload: payload,
	}}
	response, err := decodeWriteResponse(frame, "write-1", 6)
	if err != nil || response.Path != "new.txt" || !response.Created {
		t.Fatalf("write response=%#v err=%v", response, err)
	}
	for code, expected := range map[string]error{
		"REMOTE_FILE_CONFLICT":           ErrRuntimeFileConflict,
		"REMOTE_FILE_WRITE_FAILED":       ErrRuntimeFileWrite,
		"REMOTE_UNSUPPORTED_FILE_LAYOUT": ErrRuntimeFileUnsupported,
		"REMOTE_OUTPUT_LIMIT":            ErrRuntimeFileOutputLimit,
		"REMOTE_CANCELLED":               context.Canceled,
		"REMOTE_OUTCOME_UNKNOWN":         ErrRuntimeOutcomeUnknown,
	} {
		errorFrame := remoteprotocol.Frame{Envelope: remoteprotocol.Envelope{
			Version: remoteprotocol.Version, Kind: remoteprotocol.KindError,
			ID: "write-1", Generation: 6,
			Error: &remoteprotocol.RemoteError{Code: code, Message: "untrusted /secret/path", OutcomeUnknown: code == "REMOTE_OUTCOME_UNKNOWN"},
		}}
		if _, err := decodeWriteResponse(errorFrame, "write-1", 6); !errors.Is(err, expected) || strings.Contains(err.Error(), "/secret/path") {
			t.Fatalf("write error %s=%v", code, err)
		}
	}
}

func TestInstalledHelperGenerationWriteCancelledBeforeDispatchIsKnown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runtimeProcess := &installedHelperGeneration{generation: 24}
	_, err := runtimeProcess.WriteFile(ctx, "root-0123456789abcdef0123456789abcdef", RuntimeFileWriteRequest{
		Path: "new.txt", Content: []byte("new"), ExpectedAbsent: true,
	})
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrRuntimeOutcomeUnknown) {
		t.Fatalf("pre-dispatch cancellation error=%v", err)
	}
}

func TestInstalledHelperGenerationWriteDisconnectIsOutcomeUnknown(t *testing.T) {
	runtimeInput, serverOutput := io.Pipe()
	serverInput, runtimeOutput := io.Pipe()
	var closeOnce sync.Once
	runtimeProcess := &installedHelperGeneration{
		generation: 24,
		stdin:      runtimeOutput,
		writer:     remoteprotocol.NewWriter(runtimeOutput, remoteprotocol.Limits{}),
		reader:     remoteprotocol.NewReader(runtimeInput, remoteprotocol.Limits{}),
		cancel:     func() { closeOnce.Do(func() { _ = runtimeOutput.Close(); _ = runtimeInput.Close() }) },
		readDone:   make(chan struct{}), pending: make(map[string]chan helperCallResult),
	}
	runtimeProcess.startReader()
	serverDone := make(chan error, 1)
	go func() {
		frame, err := remoteprotocol.NewReader(serverInput, remoteprotocol.Limits{}).Read()
		if err == nil && (frame.Envelope.Method != remotehelper.MethodFileWrite || string(frame.Blob) != "new") {
			err = errors.New("write request did not carry the expected blob")
		}
		_ = serverOutput.Close()
		serverDone <- err
	}()
	_, err := runtimeProcess.WriteFile(context.Background(), "root-0123456789abcdef0123456789abcdef", RuntimeFileWriteRequest{
		Path: "new.txt", Content: []byte("new"), ExpectedAbsent: true,
	})
	if !errors.Is(err, ErrRuntimeOutcomeUnknown) {
		t.Fatalf("disconnected write error=%v", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	_ = serverInput.Close()
}

func TestInstalledHelperGenerationShutdownDeadlineKillsUnresponsiveHelper(t *testing.T) {
	runtimeInput, serverOutput := io.Pipe()
	serverInput, runtimeOutput := io.Pipe()
	var closeOnce sync.Once
	runtimeProcess := &installedHelperGeneration{
		generation: 23,
		stdin:      runtimeOutput,
		writer:     remoteprotocol.NewWriter(runtimeOutput, remoteprotocol.Limits{}),
		reader:     remoteprotocol.NewReader(runtimeInput, remoteprotocol.Limits{}),
		stderr:     &boundedOutput{limit: maxConnectionProbeStderrBytes},
		cancel:     func() { closeOnce.Do(func() { _ = runtimeOutput.Close(); _ = runtimeInput.Close() }) },
		done:       make(chan struct{}), readDone: make(chan struct{}), pending: make(map[string]chan helperCallResult),
	}
	runtimeProcess.startReader()
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		reader := remoteprotocol.NewReader(serverInput, remoteprotocol.Limits{})
		_, _ = reader.Read()
		_, _ = reader.Read()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := runtimeProcess.Shutdown(ctx); !errors.Is(err, ErrHelperRuntimeShutdown) {
		t.Fatalf("unresponsive shutdown error=%v", err)
	}
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown deadline did not kill transport")
	}
	_ = serverInput.Close()
	_ = serverOutput.Close()
}

func TestNewInstalledHelperGenerationFactoryRequiresProductionInstaller(t *testing.T) {
	if _, err := NewInstalledHelperGenerationFactory(nil, HelperArtifact{}); err == nil {
		t.Fatal("nil installer was accepted")
	}
	connection, _ := readyRuntimeConnection(t)
	installer, err := newHelperInstaller(connection, func(context.Context, string) (remoteCacheFS, error) {
		return nil, errors.New("unused")
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewInstalledHelperGenerationFactory(installer, HelperArtifact{}); err == nil {
		t.Fatal("test-only installer without locator was accepted")
	}
}
