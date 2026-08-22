package remotehelper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"pi-desk/internal/remoteprotocol"
)

const testNonce = "0123456789abcdef0123456789abcdef"

type serverHarness struct {
	writer *remoteprotocol.Writer
	reader *remoteprotocol.Reader
	input  *io.PipeWriter
	output *io.PipeReader
	done   chan error
}

func newServerHarness(t *testing.T) *serverHarness {
	return newServerHarnessWithRoots(t, nil)
}

func newServerHarnessWithRoots(t *testing.T, roots rootCapabilityManager) *serverHarness {
	t.Helper()
	serverInput, clientInput := io.Pipe()
	clientOutput, serverOutput := io.Pipe()
	server, err := NewServer(serverInput, serverOutput, Config{BuildHash: "test-build"})
	if err != nil {
		t.Fatal(err)
	}
	if roots != nil {
		server.roots = roots
	}
	harness := &serverHarness{
		writer: remoteprotocol.NewWriter(clientInput, remoteprotocol.Limits{}),
		reader: remoteprotocol.NewReader(clientOutput, remoteprotocol.Limits{}),
		input:  clientInput,
		output: clientOutput,
		done:   make(chan error, 1),
	}
	go func() {
		err := server.Serve(context.Background())
		_ = serverInput.Close()
		_ = serverOutput.Close()
		harness.done <- err
	}()
	t.Cleanup(func() {
		_ = clientInput.Close()
		_ = clientOutput.Close()
		select {
		case <-harness.done:
		default:
		}
	})
	return harness
}

func (harness *serverHarness) hello(t *testing.T, generation uint64) HelloResponse {
	t.Helper()
	payload, err := remoteprotocol.EncodePayload(HelloRequest{
		Nonce:         testNonce,
		ClientVersion: "pi-desk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.writer.Write(remoteprotocol.Envelope{
		Version:    remoteprotocol.Version,
		Kind:       remoteprotocol.KindRequest,
		ID:         "hello-1",
		Generation: generation,
		Method:     MethodHello,
		Payload:    payload,
	}, nil); err != nil {
		t.Fatal(err)
	}
	frame, err := harness.reader.Read()
	if err != nil {
		t.Fatalf("read hello response: %v", err)
	}
	if frame.Envelope.Kind != remoteprotocol.KindResponse || frame.Envelope.ID != "hello-1" {
		t.Fatalf("unexpected hello response: %#v", frame.Envelope)
	}
	var response HelloResponse
	if err := remoteprotocol.DecodePayload(frame.Envelope.Payload, &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func (harness *serverHarness) request(t *testing.T, id, method string, generation uint64) remoteprotocol.Frame {
	return harness.requestPayload(t, id, method, generation, nil)
}

func (harness *serverHarness) send(t *testing.T, envelope remoteprotocol.Envelope) {
	t.Helper()
	if err := harness.writer.Write(envelope, nil); err != nil {
		t.Fatal(err)
	}
}

func (harness *serverHarness) read(t *testing.T) remoteprotocol.Frame {
	t.Helper()
	frame, err := harness.reader.Read()
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func (harness *serverHarness) requestPayload(t *testing.T, id, method string, generation uint64, payload any) remoteprotocol.Frame {
	t.Helper()
	var encoded []byte
	var err error
	if payload != nil {
		encoded, err = remoteprotocol.EncodePayload(payload)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := harness.writer.Write(remoteprotocol.Envelope{
		Version:    remoteprotocol.Version,
		Kind:       remoteprotocol.KindRequest,
		ID:         id,
		Generation: generation,
		Method:     method,
		Payload:    encoded,
	}, nil); err != nil {
		t.Fatal(err)
	}
	frame, err := harness.reader.Read()
	if err != nil {
		t.Fatalf("read %s response: %v", method, err)
	}
	return frame
}

func (harness *serverHarness) await(t *testing.T) error {
	t.Helper()
	select {
	case err := <-harness.done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("remote helper did not stop")
		return nil
	}
}

func TestServerHelloPingAndShutdown(t *testing.T) {
	harness := newServerHarness(t)
	response := harness.hello(t, 41)
	if response.Nonce != testNonce || response.ProtocolVersion != remoteprotocol.Version || response.BuildHash != "test-build" {
		t.Fatalf("unexpected hello response: %#v", response)
	}
	if response.OS == "" || response.Architecture == "" || !slices.Equal(response.Capabilities, []string{MethodFileContent, MethodFileHash, MethodFileImage, MethodFileList, MethodFileMkdir, MethodFileRead, MethodFileStat, MethodFileWrite, MethodBashRun, MethodGitRead, MethodTerminalRun, MethodPing, MethodRootOpen, MethodSearchFind, MethodSearchGrep, MethodShutdown}) {
		t.Fatalf("incomplete helper capabilities: %#v", response)
	}

	ping := harness.request(t, "ping-1", MethodPing, 41)
	if ping.Envelope.Kind != remoteprotocol.KindResponse || ping.Envelope.ID != "ping-1" {
		t.Fatalf("unexpected ping response: %#v", ping.Envelope)
	}
	var pingResponse PingResponse
	if err := remoteprotocol.DecodePayload(ping.Envelope.Payload, &pingResponse); err != nil || !pingResponse.OK {
		t.Fatalf("unexpected ping payload: %#v, %v", pingResponse, err)
	}

	shutdown := harness.request(t, "shutdown-1", MethodShutdown, 41)
	if shutdown.Envelope.Kind != remoteprotocol.KindResponse || shutdown.Envelope.ID != "shutdown-1" {
		t.Fatalf("unexpected shutdown response: %#v", shutdown.Envelope)
	}
	if err := harness.await(t); err != nil {
		t.Fatalf("Serve returned an error: %v", err)
	}
}

type fakeRootManager struct {
	response        RootOpenResponse
	err             error
	paths           []string
	closed          bool
	statResponse    FileInfoResponse
	listResponse    FileListResponse
	readResponse    FileReadResponse
	hashResponse    FileHashResponse
	imageResponse   FileImageResponse
	contentResponse FileContentResponse
	contentBlob     []byte
	imageBlob       []byte
	imageStarted    chan struct{}
	writeResponse   FileWriteResponse
	mkdirResponse   FileMkdirResponse
	findResponse    SearchFindResponse
	grepResponse    SearchGrepResponse
	gitResponse     GitReadResponse
	gitBlob         []byte
	writtenBlob     []byte
	fileErr         error
	statStarted     chan struct{}
	statBlock       chan struct{}
	statOnce        sync.Once
}

func (manager *fakeRootManager) Open(_ context.Context, path string) (RootOpenResponse, error) {
	manager.paths = append(manager.paths, path)
	return manager.response, manager.err
}

func (manager *fakeRootManager) Stat(ctx context.Context, _, _ string) (FileInfoResponse, error) {
	if manager.statStarted != nil {
		manager.statOnce.Do(func() { close(manager.statStarted) })
	}
	if manager.statBlock != nil {
		select {
		case <-ctx.Done():
			return FileInfoResponse{}, ctx.Err()
		case <-manager.statBlock:
		}
	}
	return manager.statResponse, manager.fileErr
}

func (manager *fakeRootManager) List(context.Context, string, string) (FileListResponse, error) {
	return manager.listResponse, manager.fileErr
}

func (manager *fakeRootManager) Read(context.Context, FileReadRequest) (FileReadResponse, error) {
	return manager.readResponse, manager.fileErr
}

func (manager *fakeRootManager) Image(context.Context, string, string) (FileImageResponse, []byte, error) {
	if manager.imageStarted != nil {
		manager.imageStarted <- struct{}{}
	}
	return manager.imageResponse, manager.imageBlob, manager.fileErr
}

func (manager *fakeRootManager) Content(context.Context, string, string) (FileContentResponse, []byte, error) {
	return manager.contentResponse, manager.contentBlob, manager.fileErr
}

func (manager *fakeRootManager) Hash(context.Context, string, string) (FileHashResponse, error) {
	return manager.hashResponse, manager.fileErr
}

func (manager *fakeRootManager) Write(_ context.Context, _ FileWriteRequest, content []byte) (FileWriteResponse, error) {
	manager.writtenBlob = append([]byte(nil), content...)
	return manager.writeResponse, manager.fileErr
}

func (manager *fakeRootManager) Mkdir(context.Context, FileMkdirRequest) (FileMkdirResponse, error) {
	return manager.mkdirResponse, manager.fileErr
}

func (manager *fakeRootManager) Find(context.Context, SearchFindRequest) (SearchFindResponse, error) {
	return manager.findResponse, manager.fileErr
}

func (manager *fakeRootManager) Grep(context.Context, SearchGrepRequest) (SearchGrepResponse, error) {
	return manager.grepResponse, manager.fileErr
}

func (manager *fakeRootManager) Git(context.Context, GitReadRequest) (GitReadResponse, []byte, error) {
	return manager.gitResponse, manager.gitBlob, manager.fileErr
}

func (manager *fakeRootManager) Close() error {
	manager.closed = true
	return nil
}

func TestServerRootOpenUsesStrictPayloadAndStableErrors(t *testing.T) {
	roots := &fakeRootManager{response: RootOpenResponse{
		Handle: "root-0123456789abcdef0123456789abcdef", CanonicalPath: "/srv/repo", Device: 7, Inode: 11,
	}}
	harness := newServerHarnessWithRoots(t, roots)
	harness.hello(t, 8)
	frame := harness.requestPayload(t, "root-1", MethodRootOpen, 8, RootOpenRequest{Path: "/srv/repo"})
	if frame.Envelope.Kind != remoteprotocol.KindResponse || len(roots.paths) != 1 || roots.paths[0] != "/srv/repo" {
		t.Fatalf("root open frame=%#v paths=%#v", frame.Envelope, roots.paths)
	}
	var response RootOpenResponse
	if err := remoteprotocol.DecodePayload(frame.Envelope.Payload, &response); err != nil || response != roots.response {
		t.Fatalf("root response=%#v err=%v", response, err)
	}

	if err := harness.writer.Write(remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest,
		ID: "root-invalid", Generation: 8, Method: MethodRootOpen,
		Payload: []byte(`{"path":"/srv/repo","unknown":true}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	invalid, err := harness.reader.Read()
	if err != nil || invalid.Envelope.Kind != remoteprotocol.KindError || invalid.Envelope.Error == nil || invalid.Envelope.Error.Code != "REMOTE_INVALID_REQUEST" {
		t.Fatalf("invalid root response=%#v err=%v", invalid.Envelope, err)
	}
	harness.request(t, "shutdown-1", MethodShutdown, 8)
	if err := harness.await(t); err != nil || !roots.closed {
		t.Fatalf("root manager cleanup: err=%v closed=%v", err, roots.closed)
	}
}

func TestServerRootOpenProjectsResourceFailureWithoutPath(t *testing.T) {
	roots := &fakeRootManager{err: ErrRootResourceLimit}
	harness := newServerHarnessWithRoots(t, roots)
	harness.hello(t, 9)
	frame := harness.requestPayload(t, "root-limit", MethodRootOpen, 9, RootOpenRequest{Path: "/secret/repo"})
	if frame.Envelope.Kind != remoteprotocol.KindError || frame.Envelope.Error == nil || frame.Envelope.Error.Code != "REMOTE_RESOURCE_LIMIT" || strings.Contains(frame.Envelope.Error.Message, "/secret/repo") {
		t.Fatalf("resource root response=%#v", frame.Envelope)
	}
	harness.request(t, "shutdown-1", MethodShutdown, 9)
	if err := harness.await(t); err != nil {
		t.Fatal(err)
	}
}

func TestServerRootBoundReadOnlyMethods(t *testing.T) {
	roots := &fakeRootManager{
		statResponse:    FileInfoResponse{Path: "file.txt", Kind: "file", Size: 5},
		listResponse:    FileListResponse{Path: ".", Entries: []FileInfoResponse{{Path: "file.txt", Kind: "file", Size: 5}}},
		readResponse:    FileReadResponse{Path: "file.txt", Content: "hello", StartLine: 1, EndLine: 1},
		hashResponse:    FileHashResponse{Path: "file.txt", Size: 5, SHA256: strings.Repeat("a", 64)},
		imageResponse:   FileImageResponse{Path: "image.png", MIME: "image/png", Size: 8, SHA256: strings.Repeat("b", 64)},
		imageBlob:       []byte("\x89PNG\r\n\x1a\n"),
		contentResponse: FileContentResponse{Path: "file.txt", Size: 5, SHA256: strings.Repeat("d", 64)},
		contentBlob:     []byte("hello"),
		writeResponse:   FileWriteResponse{Path: "new.txt", Size: 3, SHA256: strings.Repeat("c", 64), Created: true},
		mkdirResponse:   FileMkdirResponse{Path: "nested/deep", Created: []string{"nested", "nested/deep"}},
		findResponse:    SearchFindResponse{Paths: []string{"file.txt"}},
		grepResponse:    SearchGrepResponse{Matches: []SearchGrepMatch{{Path: "file.txt", Line: 1, Text: "hello"}}},
		gitResponse:     GitReadResponse{Operation: "status", Parts: []GitOutputPart{{Name: "status", Size: 6, SHA256: strings.Repeat("e", 64)}}},
		gitBlob:         []byte("status"),
	}
	harness := newServerHarnessWithRoots(t, roots)
	harness.hello(t, 10)
	requests := []struct {
		id      string
		method  string
		payload any
		output  any
	}{
		{id: "stat", method: MethodFileStat, payload: FileRequest{RootHandle: "root-0123456789abcdef0123456789abcdef", Path: "file.txt"}, output: &FileInfoResponse{}},
		{id: "list", method: MethodFileList, payload: FileRequest{RootHandle: "root-0123456789abcdef0123456789abcdef", Path: "."}, output: &FileListResponse{}},
		{id: "read", method: MethodFileRead, payload: FileReadRequest{RootHandle: "root-0123456789abcdef0123456789abcdef", Path: "file.txt", StartLine: 1, MaxLines: 10}, output: &FileReadResponse{}},
		{id: "hash", method: MethodFileHash, payload: FileRequest{RootHandle: "root-0123456789abcdef0123456789abcdef", Path: "file.txt"}, output: &FileHashResponse{}},
		{id: "image", method: MethodFileImage, payload: FileRequest{RootHandle: "root-0123456789abcdef0123456789abcdef", Path: "image.png"}, output: &FileImageResponse{}},
		{id: "content", method: MethodFileContent, payload: FileRequest{RootHandle: "root-0123456789abcdef0123456789abcdef", Path: "file.txt"}, output: &FileContentResponse{}},
		{id: "mkdir", method: MethodFileMkdir, payload: FileMkdirRequest{RootHandle: "root-0123456789abcdef0123456789abcdef", Path: "nested/deep"}, output: &FileMkdirResponse{}},
		{id: "find", method: MethodSearchFind, payload: SearchFindRequest{RootHandle: "root-0123456789abcdef0123456789abcdef", Path: ".", Pattern: "*.txt", Limit: 10}, output: &SearchFindResponse{}},
		{id: "grep", method: MethodSearchGrep, payload: SearchGrepRequest{RootHandle: "root-0123456789abcdef0123456789abcdef", Path: ".", Pattern: "hello", Limit: 10}, output: &SearchGrepResponse{}},
		{id: "git", method: MethodGitRead, payload: GitReadRequest{RootHandle: "root-0123456789abcdef0123456789abcdef", Operation: "status"}, output: &GitReadResponse{}},
	}
	for _, request := range requests {
		frame := harness.requestPayload(t, request.id, request.method, 10, request.payload)
		if frame.Envelope.Kind != remoteprotocol.KindResponse {
			t.Fatalf("%s response=%#v", request.method, frame.Envelope)
		}
		if err := remoteprotocol.DecodePayload(frame.Envelope.Payload, request.output); err != nil {
			t.Fatalf("decode %s: %v", request.method, err)
		}
		if request.method == MethodFileImage && !bytes.Equal(frame.Blob, roots.imageBlob) {
			t.Fatalf("image blob=%q", frame.Blob)
		}
		if request.method == MethodFileContent && !bytes.Equal(frame.Blob, roots.contentBlob) {
			t.Fatalf("content blob=%q", frame.Blob)
		}
		if request.method == MethodGitRead && !bytes.Equal(frame.Blob, roots.gitBlob) {
			t.Fatalf("git blob=%q", frame.Blob)
		}
	}
	writePayload, err := remoteprotocol.EncodePayload(FileWriteRequest{
		RootHandle: "root-0123456789abcdef0123456789abcdef", Path: "new.txt", ExpectedAbsent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.writer.Write(remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest,
		ID: "write", Generation: 10, Method: MethodFileWrite, Payload: writePayload,
	}, []byte("new")); err != nil {
		t.Fatal(err)
	}
	writeFrame := harness.read(t)
	if writeFrame.Envelope.Kind != remoteprotocol.KindResponse || string(roots.writtenBlob) != "new" {
		t.Fatalf("write response=%#v blob=%q", writeFrame.Envelope, roots.writtenBlob)
	}
	harness.request(t, "shutdown", MethodShutdown, 10)
	if err := harness.await(t); err != nil {
		t.Fatal(err)
	}
}

func TestServerMkdirProjectsPartialCancellationAsOutcomeUnknown(t *testing.T) {
	roots := &fakeRootManager{fileErr: ErrMutationOutcomeUnknown}
	harness := newServerHarnessWithRoots(t, roots)
	harness.hello(t, 11)
	frame := harness.requestPayload(t, "mkdir", MethodFileMkdir, 11, FileMkdirRequest{
		RootHandle: "root-0123456789abcdef0123456789abcdef", Path: "nested/deep",
	})
	if frame.Envelope.Kind != remoteprotocol.KindError || frame.Envelope.Error == nil || frame.Envelope.Error.Code != "REMOTE_OUTCOME_UNKNOWN" || !frame.Envelope.Error.OutcomeUnknown {
		t.Fatalf("mkdir outcome=%#v", frame.Envelope)
	}
	harness.request(t, "shutdown", MethodShutdown, 11)
	if err := harness.await(t); err != nil {
		t.Fatal(err)
	}
}

func TestServerFileErrorsAreStableAndRedacted(t *testing.T) {
	for expected, operationErr := range map[string]error{
		"REMOTE_INVALID_REQUEST":         ErrFileInvalidPath,
		"REMOTE_FILE_NOT_FOUND":          ErrFileNotFound,
		"REMOTE_UNSUPPORTED_FILE_LAYOUT": ErrFileUnsupported,
		"REMOTE_OUTPUT_LIMIT":            ErrFileOutputLimit,
		"REMOTE_FILE_CONFLICT":           ErrFileConflict,
		"REMOTE_FILE_WRITE_FAILED":       ErrFileWrite,
	} {
		t.Run(expected, func(t *testing.T) {
			roots := &fakeRootManager{fileErr: operationErr}
			harness := newServerHarnessWithRoots(t, roots)
			harness.hello(t, 11)
			frame := harness.requestPayload(t, "stat", MethodFileStat, 11, FileRequest{RootHandle: "root-0123456789abcdef0123456789abcdef", Path: "secret.txt"})
			if frame.Envelope.Kind != remoteprotocol.KindError || frame.Envelope.Error == nil || frame.Envelope.Error.Code != expected || strings.Contains(frame.Envelope.Error.Message, "secret.txt") {
				t.Fatalf("file error=%#v", frame.Envelope)
			}
			harness.request(t, "shutdown", MethodShutdown, 11)
			if err := harness.await(t); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHelperRequestCoalescesReturnedStreamCredit(t *testing.T) {
	request := &helperRequest{creditReady: make(chan struct{}, 1)}
	if err := request.pushCredit(32 << 10); err != nil {
		t.Fatal(err)
	}
	if err := request.takeCredit(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if err := request.pushCredit(1); err != nil {
		t.Fatal(err)
	}
	if err := request.pushCredit(1); err != nil {
		t.Fatalf("second returned credit was rejected: %v", err)
	}
	if err := request.pushCredit(int64(remoteprotocol.MaxStreamChunkBytes)); !errors.Is(err, ErrUnexpectedMessage) {
		t.Fatalf("oversized aggregate credit error=%v", err)
	}
}

func TestServerRequiresHelloAsFirstRequest(t *testing.T) {
	harness := newServerHarness(t)
	frame := harness.request(t, "ping-1", MethodPing, 1)
	if frame.Envelope.Kind != remoteprotocol.KindError || frame.Envelope.Error == nil || frame.Envelope.Error.Code != "REMOTE_HANDSHAKE_REQUIRED" {
		t.Fatalf("unexpected handshake error: %#v", frame.Envelope)
	}
	if err := harness.await(t); !errors.Is(err, ErrHandshakeRequired) {
		t.Fatalf("Serve error = %v, want ErrHandshakeRequired", err)
	}
}

func TestServerRejectsInvalidHelloPayload(t *testing.T) {
	harness := newServerHarness(t)
	if err := harness.writer.Write(remoteprotocol.Envelope{
		Version:    remoteprotocol.Version,
		Kind:       remoteprotocol.KindRequest,
		ID:         "hello-1",
		Generation: 1,
		Method:     MethodHello,
		Payload:    []byte(`{"nonce":"0123456789abcdef","clientVersion":"test","unknown":true}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	frame, err := harness.reader.Read()
	if err != nil {
		t.Fatal(err)
	}
	if frame.Envelope.Kind != remoteprotocol.KindError || frame.Envelope.Error == nil || frame.Envelope.Error.Code != "REMOTE_HANDSHAKE_REJECTED" {
		t.Fatalf("unexpected hello rejection: %#v", frame.Envelope)
	}
	if err := harness.await(t); !errors.Is(err, ErrHandshakeRejected) {
		t.Fatalf("Serve error = %v, want ErrHandshakeRejected", err)
	}
}

func TestServerRejectsGenerationChange(t *testing.T) {
	harness := newServerHarness(t)
	harness.hello(t, 1)
	if err := harness.writer.Write(remoteprotocol.Envelope{
		Version:    remoteprotocol.Version,
		Kind:       remoteprotocol.KindRequest,
		ID:         "ping-1",
		Generation: 2,
		Method:     MethodPing,
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := harness.await(t); !errors.Is(err, ErrGenerationMismatch) {
		t.Fatalf("Serve error = %v, want ErrGenerationMismatch", err)
	}
}

func TestServerDispatchesRequestsConcurrently(t *testing.T) {
	started := make(chan struct{})
	blocked := make(chan struct{})
	roots := &fakeRootManager{
		statResponse: FileInfoResponse{Path: "file.txt", Kind: "file", Size: 5},
		statStarted:  started, statBlock: blocked,
	}
	harness := newServerHarnessWithRoots(t, roots)
	harness.hello(t, 12)
	payload, _ := remoteprotocol.EncodePayload(FileRequest{RootHandle: "root-0123456789abcdef0123456789abcdef", Path: "file.txt"})
	harness.send(t, remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest, ID: "slow-stat", Generation: 12, Method: MethodFileStat, Payload: payload})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stat request was not dispatched")
	}
	harness.send(t, remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest, ID: "fast-ping", Generation: 12, Method: MethodPing})
	if frame := harness.read(t); frame.Envelope.ID != "fast-ping" || frame.Envelope.Kind != remoteprotocol.KindResponse {
		t.Fatalf("blocked request serialized dispatcher: %#v", frame.Envelope)
	}
	close(blocked)
	if frame := harness.read(t); frame.Envelope.ID != "slow-stat" || frame.Envelope.Kind != remoteprotocol.KindResponse {
		t.Fatalf("unexpected stat terminal: %#v", frame.Envelope)
	}
	harness.request(t, "shutdown", MethodShutdown, 12)
	if err := harness.await(t); err != nil {
		t.Fatal(err)
	}
}

func TestServerCancelHasOneTerminalAndLateCancelIsIgnored(t *testing.T) {
	started := make(chan struct{})
	roots := &fakeRootManager{statStarted: started, statBlock: make(chan struct{})}
	harness := newServerHarnessWithRoots(t, roots)
	harness.hello(t, 13)
	payload, _ := remoteprotocol.EncodePayload(FileRequest{RootHandle: "root-0123456789abcdef0123456789abcdef", Path: "file.txt"})
	harness.send(t, remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest, ID: "cancel-me", Generation: 13, Method: MethodFileStat, Payload: payload})
	<-started
	cancelEnvelope := remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindCancel, ID: "cancel-me", Generation: 13}
	harness.send(t, cancelEnvelope)
	terminal := harness.read(t)
	if terminal.Envelope.ID != "cancel-me" || terminal.Envelope.Kind != remoteprotocol.KindError || terminal.Envelope.Error == nil || terminal.Envelope.Error.Code != "REMOTE_CANCELLED" {
		t.Fatalf("cancel terminal=%#v", terminal.Envelope)
	}
	harness.send(t, cancelEnvelope)
	ping := harness.request(t, "ping-after-late-cancel", MethodPing, 13)
	if ping.Envelope.Kind != remoteprotocol.KindResponse {
		t.Fatalf("late cancel stopped helper: %#v", ping.Envelope)
	}
	harness.request(t, "shutdown", MethodShutdown, 13)
	if err := harness.await(t); err != nil {
		t.Fatal(err)
	}
}

func TestServerRequestTimeoutProducesCancelledTerminal(t *testing.T) {
	roots := &fakeRootManager{statBlock: make(chan struct{})}
	harness := newServerHarnessWithRoots(t, roots)
	harness.hello(t, 14)
	payload, _ := remoteprotocol.EncodePayload(FileRequest{RootHandle: "root-0123456789abcdef0123456789abcdef", Path: "file.txt"})
	harness.send(t, remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest, ID: "timeout", Generation: 14,
		Method: MethodFileStat, Payload: payload, TimeoutMillis: 10,
	})
	terminal := harness.read(t)
	if terminal.Envelope.Kind != remoteprotocol.KindError || terminal.Envelope.Error == nil || terminal.Envelope.Error.Code != "REMOTE_CANCELLED" {
		t.Fatalf("timeout terminal=%#v", terminal.Envelope)
	}
	harness.request(t, "shutdown", MethodShutdown, 14)
	if err := harness.await(t); err != nil {
		t.Fatal(err)
	}
}

func TestServerBackpressuresImageBuffersUntilTerminalIsAccepted(t *testing.T) {
	started := make(chan struct{}, 2)
	roots := &fakeRootManager{
		imageResponse: FileImageResponse{Path: "image.png", MIME: "image/png", Size: 8, SHA256: strings.Repeat("a", 64)},
		imageBlob:     []byte("\x89PNG\r\n\x1a\n"), imageStarted: started,
	}
	harness := newServerHarnessWithRoots(t, roots)
	harness.hello(t, 19)
	payload, _ := remoteprotocol.EncodePayload(FileRequest{RootHandle: "root-0123456789abcdef0123456789abcdef", Path: "image.png"})
	harness.send(t, remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest,
		ID: "image-1", Generation: 19, Method: MethodFileImage, Payload: payload,
	})
	<-started
	harness.send(t, remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest,
		ID: "image-2", Generation: 19, Method: MethodFileImage, Payload: payload,
	})
	select {
	case <-started:
		t.Fatal("second image allocated before the first terminal was accepted")
	case <-time.After(20 * time.Millisecond):
	}
	if frame := harness.read(t); frame.Envelope.ID != "image-1" || len(frame.Blob) == 0 {
		t.Fatalf("first image terminal=%#v", frame.Envelope)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("second image did not resume after terminal acceptance")
	}
	if frame := harness.read(t); frame.Envelope.ID != "image-2" || len(frame.Blob) == 0 {
		t.Fatalf("second image terminal=%#v", frame.Envelope)
	}
	harness.request(t, "shutdown", MethodShutdown, 19)
	if err := harness.await(t); err != nil {
		t.Fatal(err)
	}
}

func TestServerBoundsConcurrentProcesses(t *testing.T) {
	server := &Server{active: make(map[string]*helperRequest), tombstones: make(map[string]time.Time)}
	for index := range maxConcurrentProcesses {
		request, limited, err := server.registerRequest(context.Background(), remoteprotocol.Envelope{ID: fmt.Sprintf("process-%d", index), Method: MethodBashRun})
		if err != nil || limited {
			t.Fatalf("register process %d limited=%v err=%v", index, limited, err)
		}
		defer request.cancel()
	}
	request, limited, err := server.registerRequest(context.Background(), remoteprotocol.Envelope{ID: "process-limit", Method: MethodTerminalRun})
	if err != nil || !limited {
		t.Fatalf("process limit=%v err=%v", limited, err)
	}
	request.cancel()
}

func TestServerBoundsConcurrentRequests(t *testing.T) {
	roots := &fakeRootManager{statBlock: make(chan struct{})}
	harness := newServerHarnessWithRoots(t, roots)
	harness.hello(t, 17)
	payload, _ := remoteprotocol.EncodePayload(FileRequest{RootHandle: "root-0123456789abcdef0123456789abcdef", Path: "file.txt"})
	for index := 0; index <= maxConcurrentRequests; index++ {
		harness.send(t, remoteprotocol.Envelope{
			Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest,
			ID: fmt.Sprintf("bounded-%d", index), Generation: 17, Method: MethodFileStat, Payload: payload,
		})
	}
	limited := harness.read(t)
	if limited.Envelope.Kind != remoteprotocol.KindError || limited.Envelope.Error == nil || limited.Envelope.Error.Code != "REMOTE_RESOURCE_LIMIT" {
		t.Fatalf("request limit terminal=%#v", limited.Envelope)
	}
	harness.send(t, remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest, ID: "shutdown", Generation: 17, Method: MethodShutdown})
	cancelled := 0
	for {
		frame := harness.read(t)
		if frame.Envelope.ID == "shutdown" {
			break
		}
		if frame.Envelope.Kind != remoteprotocol.KindError || frame.Envelope.Error == nil || frame.Envelope.Error.Code != "REMOTE_CANCELLED" {
			t.Fatalf("bounded request terminal=%#v", frame.Envelope)
		}
		cancelled++
	}
	if cancelled != maxConcurrentRequests {
		t.Fatalf("cancelled terminals=%d, want %d", cancelled, maxConcurrentRequests)
	}
	if err := harness.await(t); err != nil {
		t.Fatal(err)
	}
}

func TestServerRejectsDuplicateRequestAndUnknownCancel(t *testing.T) {
	t.Run("duplicate active request", func(t *testing.T) {
		started := make(chan struct{})
		roots := &fakeRootManager{statStarted: started, statBlock: make(chan struct{})}
		harness := newServerHarnessWithRoots(t, roots)
		harness.hello(t, 15)
		payload, _ := remoteprotocol.EncodePayload(FileRequest{RootHandle: "root-0123456789abcdef0123456789abcdef", Path: "file.txt"})
		harness.send(t, remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest, ID: "duplicate", Generation: 15, Method: MethodFileStat, Payload: payload})
		<-started
		harness.send(t, remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest, ID: "duplicate", Generation: 15, Method: MethodPing})
		if err := harness.await(t); !errors.Is(err, ErrDuplicateRequest) {
			t.Fatalf("duplicate error=%v", err)
		}
	})
	t.Run("duplicate completed request", func(t *testing.T) {
		harness := newServerHarness(t)
		harness.hello(t, 18)
		if frame := harness.request(t, "completed", MethodPing, 18); frame.Envelope.Kind != remoteprotocol.KindResponse {
			t.Fatalf("initial terminal=%#v", frame.Envelope)
		}
		harness.send(t, remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest, ID: "completed", Generation: 18, Method: MethodPing})
		if err := harness.await(t); !errors.Is(err, ErrDuplicateRequest) {
			t.Fatalf("completed duplicate error=%v", err)
		}
	})
	t.Run("unknown cancel", func(t *testing.T) {
		harness := newServerHarness(t)
		harness.hello(t, 16)
		harness.send(t, remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindCancel, ID: "missing", Generation: 16})
		if err := harness.await(t); !errors.Is(err, ErrUnknownCancel) {
			t.Fatalf("unknown cancel error=%v", err)
		}
	})
}

func TestServerReportsUnknownMethodAndRemainsUsable(t *testing.T) {
	harness := newServerHarness(t)
	harness.hello(t, 1)
	unknown := harness.request(t, "unknown-1", "unknown.method", 1)
	if unknown.Envelope.Kind != remoteprotocol.KindError || unknown.Envelope.Error == nil || unknown.Envelope.Error.Code != "REMOTE_METHOD_NOT_FOUND" {
		t.Fatalf("unexpected method error: %#v", unknown.Envelope)
	}
	ping := harness.request(t, "ping-1", MethodPing, 1)
	if ping.Envelope.Kind != remoteprotocol.KindResponse {
		t.Fatalf("server was not usable after an unknown method: %#v", ping.Envelope)
	}
	harness.request(t, "shutdown-1", MethodShutdown, 1)
	if err := harness.await(t); err != nil {
		t.Fatal(err)
	}
}

func TestServerReturnsOnInputEOF(t *testing.T) {
	harness := newServerHarness(t)
	if err := harness.input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := harness.await(t); err != nil {
		t.Fatalf("Serve returned an error on EOF: %v", err)
	}
}

func TestServerStopsWhenContextIsCancelled(t *testing.T) {
	input, output := io.Pipe()
	server, err := NewServer(input, io.Discard, Config{BuildHash: "test-build"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Serve error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("remote helper did not stop after context cancellation")
	}
	_ = output.Close()
}

func TestNewServerValidatesConfiguration(t *testing.T) {
	if _, err := NewServer(nil, io.Discard, Config{BuildHash: "build"}); err == nil {
		t.Fatal("NewServer should reject a nil input")
	}
	if _, err := NewServer(bytes.NewReader(nil), io.Discard, Config{BuildHash: "invalid hash"}); err == nil {
		t.Fatal("NewServer should reject an invalid build hash")
	}
}
