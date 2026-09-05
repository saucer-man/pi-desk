//go:build windows

package piruntime

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

func TestProcessAlreadyExited(t *testing.T) {
	for _, err := range []error{os.ErrProcessDone, syscall.EINVAL} {
		if !processAlreadyExited(err) {
			t.Fatalf("expected %v to be treated as an exited process", err)
		}
	}
	if processAlreadyExited(errors.New("access denied")) {
		t.Fatal("unexpectedly treated an unrelated process error as an exited process")
	}
}

func TestConfigureProcessDeliversRawShimCommandLine(t *testing.T) {
	locator := newLocator(fakeRunner{path: `C:\Program Files\nodejs\npm.cmd`})
	invocation, err := locator.NPMInvocation("install", "-g", "@earendil-works/pi-coding-agent")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(invocation.Executable, invocation.Args...)
	configureProcess(command)
	if command.SysProcAttr == nil || command.SysProcAttr.CmdLine == "" {
		t.Fatal("expected a raw command line for the command processor shim")
	}
	if !strings.Contains(command.SysProcAttr.CmdLine, ` /d /s /c ""`) {
		t.Fatalf("expected the /c payload wrapped in outer quotes: %q", command.SysProcAttr.CmdLine)
	}
	if !strings.Contains(command.SysProcAttr.CmdLine, `"C:\Program Files\nodejs\npm.cmd" install -g`) {
		t.Fatalf("expected the quoted shim path and arguments: %q", command.SysProcAttr.CmdLine)
	}
}
