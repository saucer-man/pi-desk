package remotessh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

func testRuntimeGitResult(operation string, values map[string][]byte) RuntimeGitReadResult {
	names := map[string][]string{
		"status": {"status"}, "files": {"files"}, "diff": {"staged", "working"}, "branches": {"worktrees", "refs"},
	}[operation]
	result := RuntimeGitReadResult{Operation: operation}
	for _, name := range names {
		value := values[name]
		digest := sha256.Sum256(value)
		result.Parts = append(result.Parts, RuntimeGitOutputPart{Name: name, Offset: int64(len(result.Blob)), Size: int64(len(value)), SHA256: hex.EncodeToString(digest[:])})
		result.Blob = append(result.Blob, value...)
	}
	return result
}

func TestRuntimeLeaseSupervisorReadGitUsesFixedReadLeaseAPI(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	lease, err := supervisor.AcquireRead(context.Background(), runtimeRequest(root, "git-read"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := supervisor.ReadGit(context.Background(), lease, RuntimeGitReadRequest{Operation: "status"})
	if err != nil || result.Operation != "status" || len(result.Parts) != 1 {
		t.Fatalf("git=%#v err=%v", result, err)
	}
	if _, err := supervisor.ReadGit(context.Background(), lease, RuntimeGitReadRequest{Operation: "run", Path: "status"}); !errors.Is(err, ErrRuntimeGitInvalid) {
		t.Fatalf("generic Git request error=%v", err)
	}
	if connection.Snapshot().State != ConnectionReady {
		t.Fatalf("invalid Git request revoked connection: %#v", connection.Snapshot())
	}
	lease.Release()
	_ = supervisor.Close(context.Background())
}

func TestRuntimeGitMalformedBlobRevokesGeneration(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	lease, err := supervisor.AcquireRead(context.Background(), runtimeRequest(root, "git-malformed"))
	if err != nil {
		t.Fatal(err)
	}
	_, runtimes := factory.snapshot()
	runtimes[0].mu.Lock()
	runtimes[0].gitResult.Parts[0].SHA256 = string(make([]byte, 64))
	runtimes[0].mu.Unlock()
	if _, err := supervisor.ReadGit(context.Background(), lease, RuntimeGitReadRequest{Operation: "status"}); !errors.Is(err, ErrHelperProtocolMismatch) {
		t.Fatalf("malformed Git response error=%v", err)
	}
	if connection.Snapshot().State != ConnectionDisconnected {
		t.Fatalf("malformed Git retained connection: %#v", connection.Snapshot())
	}
}
