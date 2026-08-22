package remotessh

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pi-desk/internal/remotehelper"
	"pi-desk/internal/remoteprotocol"
)

func TestRuntimeLeaseSupervisorSearchUsesAuthorizedReadLease(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	lease, err := supervisor.AcquireRead(context.Background(), runtimeRequest(root, "search-read"))
	if err != nil {
		t.Fatal(err)
	}
	found, err := supervisor.FindFiles(context.Background(), lease, RuntimeSearchFindRequest{Path: ".", Pattern: "*.txt", Limit: 10})
	if err != nil || len(found.Paths) != 1 || found.Paths[0] != "file.txt" {
		t.Fatalf("find=%#v err=%v", found, err)
	}
	grep, err := supervisor.GrepFiles(context.Background(), lease, RuntimeSearchGrepRequest{Path: ".", Pattern: "hello", Limit: 10})
	if err != nil || len(grep.Matches) != 1 || grep.Matches[0].Text != "hello" {
		t.Fatalf("grep=%#v err=%v", grep, err)
	}
	if connection.Snapshot().State != ConnectionReady {
		t.Fatalf("search revoked connection: %#v", connection.Snapshot())
	}
	lease.Release()
	_ = supervisor.Close(context.Background())
}

func TestRuntimeSearchRejectsInvalidRequestsAndMalformedResponses(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	lease, err := supervisor.AcquireRead(context.Background(), runtimeRequest(root, "search-invalid"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.FindFiles(context.Background(), lease, RuntimeSearchFindRequest{Path: "../escape", Pattern: "*", Limit: 10}); !errors.Is(err, ErrRuntimeSearchInvalid) {
		t.Fatalf("invalid find error=%v", err)
	}
	if _, err := supervisor.GrepFiles(context.Background(), lease, RuntimeSearchGrepRequest{Path: ".", Pattern: "[", Limit: 10}); !errors.Is(err, ErrRuntimeSearchInvalid) {
		t.Fatalf("invalid grep error=%v", err)
	}
	_, runtimes := factory.snapshot()
	runtimes[0].mu.Lock()
	runtimes[0].searchFind.Paths = []string{"../escape"}
	runtimes[0].mu.Unlock()
	if _, err := supervisor.FindFiles(context.Background(), lease, RuntimeSearchFindRequest{Path: ".", Pattern: "*", Limit: 10}); !errors.Is(err, ErrHelperProtocolMismatch) {
		t.Fatalf("malformed find error=%v", err)
	}
	if connection.Snapshot().State != ConnectionDisconnected {
		t.Fatalf("malformed search retained connection: %#v", connection.Snapshot())
	}
}

func TestDecodeSearchResponseProjectsStableErrorsWithoutRemoteText(t *testing.T) {
	for code, expected := range map[string]error{
		"REMOTE_INVALID_REQUEST": ErrRuntimeSearchInvalid,
		"REMOTE_GIT_UNAVAILABLE": ErrRuntimeGitUnavailable,
		"REMOTE_CANCELLED":       context.Canceled,
	} {
		frame := remoteprotocol.Frame{Envelope: remoteprotocol.Envelope{
			Version: remoteprotocol.Version, Kind: remoteprotocol.KindError,
			ID: "search-1", Generation: 4,
			Error: &remoteprotocol.RemoteError{Code: code, Message: "untrusted /secret/path"},
		}}
		if err := decodeSearchResponse(frame, "search-1", 4, &remotehelper.SearchFindResponse{}); !errors.Is(err, expected) || strings.Contains(err.Error(), "/secret/path") {
			t.Fatalf("search error %s=%v", code, err)
		}
	}
	unknown := remoteprotocol.Frame{Envelope: remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindError,
		ID: "search-1", Generation: 4, Error: &remoteprotocol.RemoteError{Code: "REMOTE_NEW_ERROR", Message: "x"},
	}}
	if err := decodeSearchResponse(unknown, "search-1", 4, &remotehelper.SearchFindResponse{}); !errors.Is(err, ErrHelperProtocolMismatch) {
		t.Fatalf("unknown search error=%v", err)
	}
}
