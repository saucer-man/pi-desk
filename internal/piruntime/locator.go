package piruntime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"pi-desk/internal/domain"
)

const maxCommandOutputBytes = 512 << 10

var versionPattern = regexp.MustCompile(`(?m)(?:^|\s)v?(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)`)

type commandRunner interface {
	LookPath(file string) (string, error)
	CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error)
}

type directoryCommandRunner interface {
	CombinedOutputInDirectory(ctx context.Context, directory, name string, args ...string) ([]byte, error)
}

type osCommandRunner struct{}

func (osCommandRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (osCommandRunner) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return osCommandRunner{}.CombinedOutputInDirectory(ctx, "", name, args...)
}

func (osCommandRunner) CombinedOutputInDirectory(ctx context.Context, directory, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = directory
	configureProcess(command)
	output := &boundedOutput{limit: maxCommandOutputBytes}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	result := append([]byte(nil), output.buffer.Bytes()...)
	if output.overflow {
		result = append(result, []byte("\n... command output truncated by Pi Desk ...\n")...)
	}
	return result, err
}

type boundedOutput struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (output *boundedOutput) Write(value []byte) (int, error) {
	remaining := output.limit - output.buffer.Len()
	if remaining > 0 {
		_, _ = output.buffer.Write(value[:min(len(value), remaining)])
	}
	if len(value) > remaining {
		output.overflow = true
	}
	return len(value), nil
}

type Locator struct {
	runner commandRunner
}

type Invocation struct {
	Executable string
	Args       []string
	PiPath     string
	Directory  string
}

func NewLocator() *Locator {
	return &Locator{runner: osCommandRunner{}}
}

func newLocator(runner commandRunner) *Locator {
	return &Locator{runner: runner}
}

func (locator *Locator) Resolve() (string, error) {
	names := []string{"pi"}
	if runtime.GOOS == "windows" {
		names = []string{"pi.cmd", "pi.exe", "pi"}
	}

	var lastErr error
	for _, name := range names {
		path, err := locator.runner.LookPath(name)
		if err == nil {
			return path, nil
		}
		lastErr = err
	}
	return "", lastErr
}

func (locator *Locator) Invocation(args ...string) (Invocation, error) {
	path, err := locator.Resolve()
	if err != nil {
		return Invocation{}, err
	}
	return invocationForPath(path, args), nil
}

// NPMInvocation resolves npm using the same platform-aware command handling
// as Pi. It is intentionally only used with Pi Desk's fixed install arguments.
func (locator *Locator) NPMInvocation(args ...string) (Invocation, error) {
	names := []string{"npm"}
	if runtime.GOOS == "windows" {
		names = []string{"npm.cmd", "npm.exe", "npm"}
	}
	var lastErr error
	for _, name := range names {
		path, err := locator.runner.LookPath(name)
		if err == nil {
			return invocationForPath(path, args), nil
		}
		lastErr = err
	}
	return Invocation{}, lastErr
}

func (locator *Locator) Run(ctx context.Context, invocation Invocation) ([]byte, error) {
	if invocation.Directory != "" {
		runner, ok := locator.runner.(directoryCommandRunner)
		if !ok {
			return nil, errors.New("command runner does not support a working directory")
		}
		return runner.CombinedOutputInDirectory(ctx, invocation.Directory, invocation.Executable, invocation.Args...)
	}
	return locator.runner.CombinedOutput(ctx, invocation.Executable, invocation.Args...)
}

func (locator *Locator) Probe(ctx context.Context) domain.PiRuntimeStatus {
	path, err := locator.Resolve()
	if err != nil {
		return domain.PiRuntimeStatus{
			State:   domain.RuntimeMissing,
			Message: "Pi CLI was not found in PATH",
		}
	}

	invocation := invocationForPath(path, []string{"--version"})
	output, err := locator.runner.CombinedOutput(ctx, invocation.Executable, invocation.Args...)
	version := parseVersion(output)
	if err != nil {
		// A cold npm/Node shim can exceed the probe deadline even though the
		// executable is present and a real RPC start will succeed. Keep the
		// runtime usable and let session startup remain the authoritative check.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return domain.PiRuntimeStatus{
				State:   domain.RuntimeReady,
				Command: path,
				Version: version,
				Message: "Pi CLI found; version check timed out",
			}
		}
		message := "Pi CLI could not be started"
		return domain.PiRuntimeStatus{
			State:   domain.RuntimeError,
			Command: path,
			Message: message,
		}
	}

	return domain.PiRuntimeStatus{
		State:   domain.RuntimeReady,
		Command: path,
		Version: version,
	}
}

func parseVersion(output []byte) string {
	match := versionPattern.FindSubmatch(output)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(string(match[1]))
}

func invocationForPath(path string, args []string) Invocation {
	invocation := Invocation{
		Executable: path,
		Args:       append([]string(nil), args...),
		PiPath:     path,
	}
	if runtime.GOOS != "windows" {
		return invocation
	}

	extension := strings.ToLower(filepath.Ext(path))
	if extension != ".cmd" && extension != ".bat" {
		return invocation
	}

	commandLine := make([]string, 0, len(args)+1)
	commandLine = append(commandLine, quoteCMDArgument(path))
	for _, arg := range args {
		commandLine = append(commandLine, quoteCMDArgument(arg))
	}
	commandProcessor := os.Getenv("ComSpec")
	if commandProcessor == "" {
		commandProcessor = "cmd.exe"
	}
	invocation.Executable = commandProcessor
	invocation.Args = []string{"/d", "/s", "/c", strings.Join(commandLine, " ")}
	return invocation
}

func quoteCMDArgument(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t&()[]{}^=;!'+,`~|<>\"") {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
