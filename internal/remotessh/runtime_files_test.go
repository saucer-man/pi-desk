package remotessh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"pi-desk/internal/remotehelper"
	"pi-desk/internal/remoteprotocol"
)

func testRuntimeImage(logicalPath string) RuntimeFileImage {
	content := []byte("\x89PNG\r\n\x1a\nfixture")
	digest := sha256.Sum256(content)
	return RuntimeFileImage{
		Path: logicalPath, MIME: "image/png", Size: int64(len(content)),
		SHA256: hex.EncodeToString(digest[:]), Content: content,
	}
}

func testRuntimeContent(logicalPath string, content []byte) RuntimeFileContent {
	digest := sha256.Sum256(content)
	return RuntimeFileContent{Path: logicalPath, Size: int64(len(content)), SHA256: hex.EncodeToString(digest[:]), Content: content}
}

func testRuntimeWriteResult(logicalPath string, content []byte, created bool) RuntimeFileWriteResult {
	digest := sha256.Sum256(content)
	return RuntimeFileWriteResult{
		Path: logicalPath, Size: int64(len(content)), SHA256: hex.EncodeToString(digest[:]),
		Created: created, ExtendedMetadataNotPreserved: !created,
	}
}

func TestRuntimeLeaseSupervisorRootBoundReadOnlyFiles(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	lease, err := supervisor.AcquireRead(context.Background(), runtimeRequest(root, "repository-refresh"))
	if err != nil {
		t.Fatal(err)
	}
	stat, err := supervisor.StatFile(context.Background(), lease, "file.txt")
	if err != nil || stat.Path != "file.txt" || stat.Kind != "file" || stat.Size != 5 {
		t.Fatalf("stat=%#v err=%v", stat, err)
	}
	listing, err := supervisor.ListFiles(context.Background(), lease, ".")
	if err != nil || listing.Path != "." || len(listing.Entries) != 1 || listing.Entries[0].Path != "file.txt" {
		t.Fatalf("list=%#v err=%v", listing, err)
	}
	read, err := supervisor.ReadFile(context.Background(), lease, "file.txt", 1, 10)
	if err != nil || read.Content != "hello" || read.StartLine != 1 || read.EndLine != 1 {
		t.Fatalf("read=%#v err=%v", read, err)
	}
	image, err := supervisor.ReadImage(context.Background(), lease, "image.png")
	if err != nil || image.Path != "image.png" || image.MIME != "image/png" || len(image.Content) == 0 {
		t.Fatalf("image=%#v err=%v", image, err)
	}
	hash, err := supervisor.HashFile(context.Background(), lease, "file.txt")
	if err != nil || hash.Path != "file.txt" || hash.Size != 5 || hash.SHA256 != strings.Repeat("a", 64) {
		t.Fatalf("hash=%#v err=%v", hash, err)
	}
	if connection.Snapshot().State != ConnectionReady {
		t.Fatalf("read-only operations revoked connection: %#v", connection.Snapshot())
	}
	lease.Release()
	if _, err := supervisor.StatFile(context.Background(), lease, "file.txt"); !errors.Is(err, ErrConnectionGenerationRevoked) {
		t.Fatalf("released lease file error=%v", err)
	}
	_ = supervisor.Close(context.Background())
}

func TestRuntimeLeaseSupervisorEnsureDirectoryRequiresTaskLease(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	task, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "mkdir-task"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := supervisor.EnsureDirectory(context.Background(), task, "nested/deep")
	if err != nil || !slices.Equal(result.Created, []string{"nested", "nested/deep"}) {
		t.Fatalf("mkdir=%#v err=%v", result, err)
	}
	task.Release()
	readLease, err := supervisor.AcquireRead(context.Background(), runtimeRequest(root, "mkdir-read"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.EnsureDirectory(context.Background(), readLease, "nested/deep"); !errors.Is(err, ErrHelperRuntimeInvalidLease) {
		t.Fatalf("read lease mkdir error=%v", err)
	}
	if connection.Snapshot().State != ConnectionReady {
		t.Fatalf("rejected mkdir revoked connection: %#v", connection.Snapshot())
	}
	readLease.Release()
	_ = supervisor.Close(context.Background())
}

func TestRuntimeLeaseSupervisorBrokerEditUsesConditionalWrite(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	task, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "edit-task"))
	if err != nil {
		t.Fatal(err)
	}
	_, runtimes := factory.snapshot()
	runtimes[0].mu.Lock()
	runtimes[0].fileContent = testRuntimeContent("file.txt", []byte("hello old"))
	runtimes[0].fileWrite = testRuntimeWriteResult("file.txt", []byte("hello new"), false)
	runtimes[0].mu.Unlock()
	result, err := supervisor.EditFile(context.Background(), task, RuntimeFileEditRequest{Path: "file.txt", OldText: "old", NewText: "new"})
	if err != nil || result.SHA256 != testRuntimeWriteResult("file.txt", []byte("hello new"), false).SHA256 {
		t.Fatalf("edit=%#v err=%v", result, err)
	}
	runtimes[0].mu.Lock()
	lastWrite := runtimes[0].lastWrite
	expectedHash := runtimes[0].fileContent.SHA256
	runtimes[0].fileContent = testRuntimeContent("file.txt", []byte("old old"))
	runtimes[0].mu.Unlock()
	if string(lastWrite.Content) != "hello new" || lastWrite.ExpectedSHA256 != expectedHash {
		t.Fatalf("conditional edit write=%#v", lastWrite)
	}
	runtimes[0].mu.Lock()
	runtimes[0].fileContent = testRuntimeContent("file.txt", []byte("one middle two"))
	runtimes[0].fileWrite = testRuntimeWriteResult("file.txt", []byte("1 middle 2"), false)
	runtimes[0].mu.Unlock()
	if _, err := supervisor.EditFileAll(context.Background(), task, "file.txt", []RuntimeFileReplacement{{OldText: "two", NewText: "2"}, {OldText: "one", NewText: "1"}}); err != nil {
		t.Fatalf("multi edit: %v", err)
	}
	runtimes[0].mu.Lock()
	multiWrite := append([]byte(nil), runtimes[0].lastWrite.Content...)
	runtimes[0].fileContent = testRuntimeContent("file.txt", []byte("old old"))
	runtimes[0].mu.Unlock()
	if string(multiWrite) != "1 middle 2" {
		t.Fatalf("multi edit content=%q", multiWrite)
	}
	if _, err := supervisor.EditFile(context.Background(), task, RuntimeFileEditRequest{Path: "file.txt", OldText: "old", NewText: "new"}); !errors.Is(err, ErrRuntimeFileConflict) {
		t.Fatalf("ambiguous edit error=%v", err)
	}
	if connection.Snapshot().State != ConnectionReady {
		t.Fatalf("edit conflict revoked connection: %#v", connection.Snapshot())
	}
	task.Release()
	_ = supervisor.Close(context.Background())
}

func TestRuntimeLeaseSupervisorConditionalWriteRequiresTaskLease(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	task, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "write-task"))
	if err != nil {
		t.Fatal(err)
	}
	request := RuntimeFileWriteRequest{Path: "new.txt", Content: []byte("new"), ExpectedAbsent: true}
	result, err := supervisor.WriteFile(context.Background(), task, request)
	if err != nil || result != testRuntimeWriteResult("new.txt", []byte("new"), true) {
		t.Fatalf("write=%#v err=%v", result, err)
	}
	task.Release()
	readLease, err := supervisor.AcquireRead(context.Background(), runtimeRequest(root, "write-read"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.WriteFile(context.Background(), readLease, request); !errors.Is(err, ErrHelperRuntimeInvalidLease) {
		t.Fatalf("read lease write error=%v", err)
	}
	if connection.Snapshot().State != ConnectionReady {
		t.Fatalf("rejected write revoked connection: %#v", connection.Snapshot())
	}
	readLease.Release()
	_ = supervisor.Close(context.Background())
}

func TestRuntimeConditionalWriteMalformedResponseRevokesGeneration(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	lease, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "malformed-write"))
	if err != nil {
		t.Fatal(err)
	}
	_, runtimes := factory.snapshot()
	runtimes[0].mu.Lock()
	runtimes[0].fileWrite.SHA256 = strings.Repeat("0", 64)
	runtimes[0].mu.Unlock()
	if _, err := supervisor.WriteFile(context.Background(), lease, RuntimeFileWriteRequest{Path: "new.txt", Content: []byte("new"), ExpectedAbsent: true}); !errors.Is(err, ErrRuntimeOutcomeUnknown) {
		t.Fatalf("malformed write error=%v", err)
	}
	if connection.Snapshot().State != ConnectionDisconnected {
		t.Fatalf("malformed write retained connection: %#v", connection.Snapshot())
	}
}

func TestRuntimeOutcomeUnknownRevokesGeneration(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	lease, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "unknown-write"))
	if err != nil {
		t.Fatal(err)
	}
	_, runtimes := factory.snapshot()
	runtimes[0].mu.Lock()
	runtimes[0].fileErr = runtimeOutcomeUnknownError()
	runtimes[0].mu.Unlock()

	_, err = supervisor.WriteFile(context.Background(), lease, RuntimeFileWriteRequest{Path: "new.txt", Content: []byte("new"), ExpectedAbsent: true})
	if !errors.Is(err, ErrRuntimeOutcomeUnknown) {
		t.Fatalf("write error=%v", err)
	}
	if connection.Snapshot().State != ConnectionDisconnected || supervisor.Snapshot().State != RuntimeStopped {
		t.Fatalf("outcome-unknown states: connection=%#v runtime=%#v", connection.Snapshot(), supervisor.Snapshot())
	}
	select {
	case <-lease.Context().Done():
	default:
		t.Fatal("outcome-unknown retained task lease")
	}
}

func TestRuntimeFileOperationsRejectPathsBeforeDispatch(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	lease, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "task-files"))
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"", ".", "/absolute", "../escape", "dir/../file", `C:\\anchor`} {
		if _, err := supervisor.ReadFile(context.Background(), lease, invalid, 1, 10); !errors.Is(err, ErrRuntimeFileInvalid) {
			t.Fatalf("invalid read path %q error=%v", invalid, err)
		}
	}
	if _, err := supervisor.ReadFile(context.Background(), lease, "file.txt", 0, 10); !errors.Is(err, ErrRuntimeFileInvalid) {
		t.Fatalf("invalid line request error=%v", err)
	}
	if connection.Snapshot().State != ConnectionReady {
		t.Fatalf("local path rejection revoked generation: %#v", connection.Snapshot())
	}
	lease.Release()
	_ = supervisor.Close(context.Background())
}

func TestRuntimeImageMalformedResponseRevokesGeneration(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	lease, err := supervisor.AcquireRead(context.Background(), runtimeRequest(root, "malformed-image"))
	if err != nil {
		t.Fatal(err)
	}
	_, runtimes := factory.snapshot()
	runtimes[0].mu.Lock()
	runtimes[0].fileImage.MIME = "image/jpeg"
	runtimes[0].mu.Unlock()
	if _, err := supervisor.ReadImage(context.Background(), lease, "image.png"); !errors.Is(err, ErrHelperProtocolMismatch) {
		t.Fatalf("malformed image error=%v", err)
	}
	if connection.Snapshot().State != ConnectionDisconnected || supervisor.Snapshot().State != RuntimeStopped {
		t.Fatalf("malformed image states: connection=%#v runtime=%#v", connection.Snapshot(), supervisor.Snapshot())
	}
}

func TestRuntimeFileMalformedResponseRevokesGeneration(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	lease, err := supervisor.AcquireRead(context.Background(), runtimeRequest(root, "malformed"))
	if err != nil {
		t.Fatal(err)
	}
	_, runtimes := factory.snapshot()
	runtimes[0].mu.Lock()
	runtimes[0].fileHash.SHA256 = strings.Repeat("A", 64)
	runtimes[0].mu.Unlock()
	if _, err := supervisor.HashFile(context.Background(), lease, "file.txt"); !errors.Is(err, ErrHelperProtocolMismatch) {
		t.Fatalf("malformed hash error=%v", err)
	}
	if connection.Snapshot().State != ConnectionDisconnected || supervisor.Snapshot().State != RuntimeStopped {
		t.Fatalf("malformed response states: connection=%#v runtime=%#v", connection.Snapshot(), supervisor.Snapshot())
	}
	select {
	case <-lease.Context().Done():
	default:
		t.Fatal("malformed response retained lease")
	}
}

func TestDecodeImageResponseRequiresMatchingTerminalAndBlob(t *testing.T) {
	image := testRuntimeImage("image.png")
	payload, err := remoteprotocol.EncodePayload(remotehelper.FileImageResponse{
		Path: image.Path, MIME: image.MIME, Size: image.Size, SHA256: image.SHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	frame := remoteprotocol.Frame{Envelope: remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindResponse,
		ID: "image-1", Generation: 5, Payload: payload,
	}, Blob: image.Content}
	response, err := decodeImageResponse(frame, "image-1", 5)
	if err != nil || !validRuntimeFileImage(response, "image.png") {
		t.Fatalf("image response=%#v err=%v", response, err)
	}
	frame.Envelope.ID = "other"
	if _, err := decodeImageResponse(frame, "image-1", 5); !errors.Is(err, ErrHelperProtocolMismatch) {
		t.Fatalf("mismatched image id error=%v", err)
	}
}

func TestDecodeFileResponseProjectsStableRedactedErrors(t *testing.T) {
	for code, expected := range map[string]error{
		"REMOTE_INVALID_REQUEST":         ErrRuntimeFileInvalid,
		"REMOTE_FILE_NOT_FOUND":          ErrRuntimeFileNotFound,
		"REMOTE_UNSUPPORTED_FILE_LAYOUT": ErrRuntimeFileUnsupported,
		"REMOTE_OUTPUT_LIMIT":            ErrRuntimeFileOutputLimit,
		"REMOTE_CANCELLED":               context.Canceled,
	} {
		frame := remoteprotocol.Frame{Envelope: remoteprotocol.Envelope{
			Version: remoteprotocol.Version, Kind: remoteprotocol.KindError,
			ID: "file-1", Generation: 3,
			Error: &remoteprotocol.RemoteError{Code: code, Message: "untrusted /secret/path"},
		}}
		if err := decodeFileResponse(frame, "file-1", 3, &struct{}{}); !errors.Is(err, expected) || strings.Contains(err.Error(), "/secret/path") {
			t.Fatalf("file error %s=%v", code, err)
		}
	}
	unknown := remoteprotocol.Frame{Envelope: remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindError,
		ID: "file-1", Generation: 3,
		Error: &remoteprotocol.RemoteError{Code: "REMOTE_FUTURE", Message: "future"},
	}}
	if err := decodeFileResponse(unknown, "file-1", 3, &struct{}{}); !errors.Is(err, ErrHelperProtocolMismatch) {
		t.Fatalf("unknown file error=%v", err)
	}
}

func TestRuntimeRelativePathValidation(t *testing.T) {
	for _, valid := range []string{".", "file.txt", "src/main.go", "path with spaces/é.txt"} {
		if !validRemoteRelativePath(valid, true) {
			t.Fatalf("valid path rejected: %q", valid)
		}
	}
	for _, invalid := range []string{"", "/absolute", "../escape", "src//main.go", `C:\\repo`, "bad\x00name", "bad\u202ename"} {
		if validRemoteRelativePath(invalid, true) {
			t.Fatalf("invalid path accepted: %q", invalid)
		}
	}
	if !directChildPath(".", "file") || !directChildPath("src", "src/main.go") || directChildPath("src", "other/main.go") {
		t.Fatal("direct child validation mismatch")
	}
}
