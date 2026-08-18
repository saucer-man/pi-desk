package piruntime

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"pi-desk/internal/domain"
)

type fakeRunner struct {
	path       string
	lookErr    error
	output     string
	commandErr error
}

type timeoutRunner struct{}

func (timeoutRunner) LookPath(string) (string, error) { return `C:\\tools\\pi.cmd`, nil }
func (timeoutRunner) CombinedOutput(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (runner fakeRunner) LookPath(string) (string, error) {
	return runner.path, runner.lookErr
}

func (runner fakeRunner) CombinedOutput(context.Context, string, ...string) ([]byte, error) {
	return []byte(runner.output), runner.commandErr
}

func TestProbeReady(t *testing.T) {
	locator := newLocator(fakeRunner{path: `C:\tools\pi.exe`, output: "0.84.1\n"})

	status := locator.Probe(context.Background())

	if status.State != domain.RuntimeReady || status.Version != "0.84.1" {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func TestProbeMissing(t *testing.T) {
	locator := newLocator(fakeRunner{lookErr: errors.New("not found")})

	status := locator.Probe(context.Background())

	if status.State != domain.RuntimeMissing {
		t.Fatalf("expected missing status, got %#v", status)
	}
}

func TestProbeCommandError(t *testing.T) {
	locator := newLocator(fakeRunner{path: "pi", commandErr: errors.New("failed")})

	status := locator.Probe(context.Background())

	if status.State != domain.RuntimeError {
		t.Fatalf("expected error status, got %#v", status)
	}
}

func TestProbeKeepsFoundPiReadyWhenVersionCheckTimesOut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	status := newLocator(timeoutRunner{}).Probe(ctx)

	if status.State != domain.RuntimeReady || status.Command == "" || status.Message == "" {
		t.Fatalf("expected a usable runtime with a diagnostic, got %#v", status)
	}
}

func TestInvocationUsesCommandProcessorForWindowsShim(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows command shim behavior")
	}

	locator := newLocator(fakeRunner{path: `C:\Program Files\nodejs\pi.cmd`})
	invocation, err := locator.Invocation("--mode", "rpc", "--name", "name with spaces")
	if err != nil {
		t.Fatalf("Invocation returned an error: %v", err)
	}
	if !strings.EqualFold(invocation.Executable, "cmd.exe") && !strings.EqualFold(invocation.Executable, `C:\Windows\system32\cmd.exe`) {
		t.Fatalf("expected command processor, got %q", invocation.Executable)
	}
	if len(invocation.Args) != 4 || invocation.Args[0] != "/d" || invocation.Args[2] != "/c" {
		t.Fatalf("unexpected command processor arguments: %#v", invocation.Args)
	}
	if !strings.Contains(invocation.Args[3], `"C:\Program Files\nodejs\pi.cmd"`) || !strings.Contains(invocation.Args[3], `"name with spaces"`) {
		t.Fatalf("command line was not quoted: %q", invocation.Args[3])
	}
}

func TestBoundedOutputDrainsWhileLimitingMemory(t *testing.T) {
	output := &boundedOutput{limit: 4}
	n, err := output.Write([]byte("123456"))
	if err != nil || n != 6 || output.buffer.String() != "1234" || !output.overflow {
		t.Fatalf("unexpected bounded output: n=%d err=%v value=%q overflow=%v", n, err, output.buffer.String(), output.overflow)
	}
}
