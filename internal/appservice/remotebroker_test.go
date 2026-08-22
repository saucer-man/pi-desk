package appservice

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"pi-desk/internal/remotessh"
)

func TestRemoteBrokerFramingAndAdapterHaveNoLocalFallback(t *testing.T) {
	request := remoteBrokerRequest{ID: "tool-1", Token: strings.Repeat("a", 64), Operation: "read", Path: "README.md", Offset: 1, Limit: 10}
	content, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var frame bytes.Buffer
	if err := binary.Write(&frame, binary.BigEndian, uint32(len(content))); err != nil {
		t.Fatal(err)
	}
	frame.Write(content)
	decoded, err := readRemoteBrokerRequest(&frame)
	if err != nil || !reflect.DeepEqual(decoded, request) {
		t.Fatalf("decoded=%#v err=%v", decoded, err)
	}

	unknown := []byte(`{"id":"x","token":"x","operation":"read","unknown":true}`)
	frame.Reset()
	_ = binary.Write(&frame, binary.BigEndian, uint32(len(unknown)))
	frame.Write(unknown)
	if _, err := readRemoteBrokerRequest(&frame); err == nil {
		t.Fatal("unknown broker request field was accepted")
	}

	for _, operation := range []string{"write", "edit", "bash"} {
		if !remoteBrokerMutation(operation) {
			t.Fatalf("mutation operation %q was not classified", operation)
		}
	}
	for _, operation := range []string{"read", "find", "grep", "ls", "context", "hello"} {
		if remoteBrokerMutation(operation) {
			t.Fatalf("read operation %q was classified as mutation", operation)
		}
	}

	source := string(remoteAdapterSource)
	for _, required := range []string{`name: "read"`, `name: "write"`, `name: "edit"`, `name: "find"`, `name: "grep"`, `name: "ls"`, `name: "bash"`, `pi.on("user_bash"`} {
		if !strings.Contains(source, required) {
			t.Fatalf("remote adapter is missing %q", required)
		}
	}
	for _, forbidden := range []string{`node:fs`, `child_process`, `createReadTool`, `createBashTool`, `details:`, `value?.trim()`} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("remote adapter contains local fallback %q", forbidden)
		}
	}
	if !strings.Contains(source, `new RemoteBrokerError("REMOTE_OUTCOME_UNKNOWN"`) {
		t.Fatal("remote adapter does not preserve mutation outcome after broker response loss")
	}
	if strings.Contains(source, `return { exitCode: null }`) {
		t.Fatal("remote user_bash swallowed an unknown mutation outcome")
	}
}

func TestRemoteBrokerRejectsConnectionsBeyondItsBound(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	broker := &remoteTaskBroker{
		ctx: ctx, cancel: cancel, listener: listener,
		sem: make(chan struct{}, remoteBrokerMaxConcurrent), conns: make(map[net.Conn]struct{}),
	}
	for range remoteBrokerMaxConcurrent {
		broker.sem <- struct{}{}
	}
	broker.wg.Add(1)
	go broker.serve()
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := connection.Read(make([]byte, 1)); err == nil {
		t.Fatal("connection beyond the broker bound remained open")
	}
	_ = connection.Close()
	cancel()
	_ = listener.Close()
	broker.wg.Wait()
	broker.mu.Lock()
	defer broker.mu.Unlock()
	if len(broker.conns) != 0 {
		t.Fatalf("rejected connections retained=%d", len(broker.conns))
	}
}

func TestRemoteBrokerErrorProjectionIsStableAndRedacted(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{remotessh.ErrRuntimeFileNotFound, "REMOTE_FILE_NOT_FOUND"},
		{remotessh.ErrRuntimeFileConflict, "REMOTE_FILE_CONFLICT"},
		{remotessh.ErrRuntimeOutcomeUnknown, "REMOTE_OUTCOME_UNKNOWN"},
		{remotessh.ErrRuntimeSearchInvalid, "REMOTE_INVALID_REQUEST"},
	}
	for _, test := range cases {
		projected := classifyRemoteBrokerError(errors.Join(test.err, errors.New("/secret/path")))
		if projected.Code != test.code || strings.Contains(projected.Message, "/secret") {
			t.Fatalf("error=%v projected=%#v", test.err, projected)
		}
	}
	for _, value := range []string{"/srv/repo", "/", "/项目"} {
		if !validCanonicalRemoteRoot(value) {
			t.Fatalf("valid root rejected: %q", value)
		}
	}
	for _, value := range []string{"", "relative", "/srv/../etc", "/srv\\repo", "/srv/\u200bhidden"} {
		if validCanonicalRemoteRoot(value) {
			t.Fatalf("invalid root accepted: %q", value)
		}
	}
}
