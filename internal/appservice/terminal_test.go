package appservice

import (
	"bytes"
	"errors"
	"testing"

	"pi-desk/internal/domain"
	terminalruntime "pi-desk/internal/terminal"
	"pi-desk/internal/workspace"
)

type fakeTerminalRuntime struct {
	startConfig terminalruntime.StartConfig
	state       terminalruntime.Snapshot
	writeID     string
	writeData   []byte
	resizeID    string
	columns     int
	rows        int
	stopID      string
	stopErr     error
}

func (runtime *fakeTerminalRuntime) Start(config terminalruntime.StartConfig) (terminalruntime.Snapshot, error) {
	runtime.startConfig = config
	return runtime.state, nil
}

func (runtime *fakeTerminalRuntime) Snapshot(string) terminalruntime.Snapshot { return runtime.state }

func (runtime *fakeTerminalRuntime) Write(threadID string, data []byte) error {
	runtime.writeID = threadID
	runtime.writeData = append([]byte(nil), data...)
	return nil
}

func (runtime *fakeTerminalRuntime) Resize(threadID string, columns, rows int) error {
	runtime.resizeID, runtime.columns, runtime.rows = threadID, columns, rows
	return nil
}

func (runtime *fakeTerminalRuntime) Stop(threadID string) error {
	runtime.stopID = threadID
	return runtime.stopErr
}

func (*fakeTerminalRuntime) Shutdown() {}

type terminalWorkspaceResolver struct {
	record workspace.Record
	err    error
}

func (resolver terminalWorkspaceResolver) ResolvePath(string) (workspace.Record, error) {
	return resolver.record, resolver.err
}

func TestTerminalServiceRequiresTrustAndMapsRuntimeState(t *testing.T) {
	runtime := &fakeTerminalRuntime{state: terminalruntime.Snapshot{
		ThreadID: "thread-1", CWD: "D:\\repo", Shell: "pwsh.exe", Running: true, Sequence: 4, Output: []byte("ready"),
	}}
	service := newTerminalService(terminalWorkspaceResolver{record: workspace.Record{Path: "D:\\repo", Trust: "approve"}}, runtime)
	state, err := service.Start(domain.StartTerminalRequest{ThreadID: " thread-1 ", WorkspacePath: "D:\\repo", Columns: 90, Rows: 28})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.startConfig.ThreadID != "thread-1" || runtime.startConfig.CWD != "D:\\repo" || !state.Running || state.OutputB64 != "cmVhZHk=" {
		t.Fatalf("unexpected mapped terminal state: config=%#v state=%#v", runtime.startConfig, state)
	}

	denied := newTerminalService(terminalWorkspaceResolver{record: workspace.Record{Path: "D:\\repo", Trust: "deny"}}, runtime)
	if _, err := denied.Start(domain.StartTerminalRequest{ThreadID: "thread-1", WorkspacePath: "D:\\repo", Columns: 80, Rows: 24}); err == nil {
		t.Fatal("expected untrusted workspace to be rejected")
	}
}

func TestTerminalServiceForwardsBoundedInputResizeAndCleanup(t *testing.T) {
	runtime := &fakeTerminalRuntime{}
	service := newTerminalService(terminalWorkspaceResolver{}, runtime)
	if err := service.Write(domain.TerminalWriteRequest{ThreadID: " thread-1 ", Data: "dir\r"}); err != nil {
		t.Fatal(err)
	}
	if runtime.writeID != "thread-1" || string(runtime.writeData) != "dir\r" {
		t.Fatalf("terminal input was not forwarded: %#v", runtime)
	}
	if err := service.Write(domain.TerminalWriteRequest{ThreadID: "thread-1", Data: string(bytes.Repeat([]byte("x"), maxTerminalInput+1))}); err == nil {
		t.Fatal("expected oversized terminal input to fail")
	}
	if err := service.Resize(domain.TerminalResizeRequest{ThreadID: "thread-1", Columns: 120, Rows: 40}); err != nil {
		t.Fatal(err)
	}
	if runtime.resizeID != "thread-1" || runtime.columns != 120 || runtime.rows != 40 {
		t.Fatalf("resize was not forwarded: %#v", runtime)
	}
	runtime.stopErr = terminalruntime.ErrNotRunning
	if err := service.stopThreadIfRunning("thread-1"); err != nil {
		t.Fatalf("already stopped terminal should be safe during cleanup: %v", err)
	}
	runtime.stopErr = errors.New("stop failed")
	if err := service.stopThreadIfRunning("thread-1"); err == nil {
		t.Fatal("expected runtime stop failure to propagate")
	}
}
