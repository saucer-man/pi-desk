package remotessh

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"pi-desk/internal/processutil"
)

const (
	maxProbeOutputBytes           = 64 << 10
	maxConnectionProbeStdoutBytes = 4 << 10
	maxConnectionProbeStderrBytes = 64 << 10
)

var (
	ErrOpenSSHNotFound     = errors.New("OpenSSH client was not found")
	ErrProbeOutputTooLarge = errors.New("OpenSSH process output exceeds the safety limit")
)

var versionPattern = regexp.MustCompile(`OpenSSH(?:_for_Windows)?_([0-9][0-9A-Za-z.+-]*)`)

// OpenSSH child processes inherit the desktop process environment. This
// preserves the user's PATH for configured ProxyCommand/ProxyJump helpers and
// uses the same SSH agent/environment as a direct system OpenSSH invocation.

type commandRunner interface {
	LookPath(file string) (string, error)
	IsFile(path string) bool
	CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error)
	Run(ctx context.Context, name string, args ...string) (commandOutput, error)
}

type platformCommandRunner interface {
	RunPlatform(ctx context.Context, name string, args ...string) (commandOutput, error)
}

type commandOutput struct {
	Stdout []byte
	Stderr []byte
}

type osCommandRunner struct{}

func (osCommandRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (osCommandRunner) IsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (osCommandRunner) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := newSSHCommand(ctx, name, args...)
	output := &boundedOutput{limit: maxProbeOutputBytes}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	if output.overflow {
		return nil, ErrProbeOutputTooLarge
	}
	return append([]byte(nil), output.buffer.Bytes()...), err
}

func (osCommandRunner) Run(ctx context.Context, name string, args ...string) (commandOutput, error) {
	command := newSSHCommand(ctx, name, args...)
	stdout := &boundedOutput{limit: maxConnectionProbeStdoutBytes}
	stderr := &boundedOutput{limit: maxConnectionProbeStderrBytes}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if stdout.overflow || stderr.overflow {
		return commandOutput{}, ErrProbeOutputTooLarge
	}
	return commandOutput{
		Stdout: append([]byte(nil), stdout.buffer.Bytes()...),
		Stderr: append([]byte(nil), stderr.buffer.Bytes()...),
	}, err
}

// RunPlatform waits for OpenSSH to report the remote command's exit status,
// then terminates the local SSH process tree. This is required for Windows
// ProxyCommand helpers which can keep their stdio open after the SSH channel
// has already received EOF/CLOSE.
func (osCommandRunner) RunPlatform(ctx context.Context, name string, args ...string) (commandOutput, error) {
	command := newSSHCommand(ctx, name, args...)
	stdoutPipe, err := command.StdoutPipe()
	if err != nil {
		return commandOutput{}, err
	}
	stderrPipe, err := command.StderrPipe()
	if err != nil {
		return commandOutput{}, err
	}
	stdout := &boundedOutput{limit: maxConnectionProbeStdoutBytes}
	stderr := &boundedOutput{limit: maxConnectionProbeStderrBytes}
	if err := command.Start(); err != nil {
		return commandOutput{}, err
	}

	var outputWait sync.WaitGroup
	outputWait.Add(2)
	go func() {
		defer outputWait.Done()
		_, _ = io.Copy(stdout, stdoutPipe)
	}()
	remoteExitStatus := false
	go func() {
		defer outputWait.Done()
		scanner := bufio.NewScanner(stderrPipe)
		scanner.Buffer(make([]byte, 1024), maxConnectionProbeStderrBytes)
		for scanner.Scan() {
			line := scanner.Text()
			_, _ = stderr.Write(append([]byte(line), '\n'))
			if strings.Contains(line, "debug1: Exit status 0") || strings.Contains(line, "rtype exit-status reply 0") {
				remoteExitStatus = true
				_ = processutil.TerminateTree(command)
			}
		}
	}()

	cleanupDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = processutil.TerminateTree(command)
		case <-cleanupDone:
		}
	}()
	waitErr := command.Wait()
	close(cleanupDone)
	outputWait.Wait()
	if stdout.overflow || stderr.overflow {
		return commandOutput{}, ErrProbeOutputTooLarge
	}
	if remoteExitStatus {
		waitErr = nil
	}
	return commandOutput{
		Stdout: append([]byte(nil), stdout.buffer.Bytes()...),
		Stderr: append([]byte(nil), stderr.buffer.Bytes()...),
	}, waitErr
}

func newSSHCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, name, args...)
	processutil.ConfigureBackground(command)
	return command
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
	runner         commandRunner
	goos           string
	windir         string
	testConfigPath string
}

type ClientInfo struct {
	Executable string
	Version    string
}

func NewLocator() *Locator {
	return &Locator{
		runner: osCommandRunner{},
		goos:   runtime.GOOS,
		windir: os.Getenv("WINDIR"),
	}
}

func newLocator(runner commandRunner, goos, windir string) *Locator {
	return &Locator{runner: runner, goos: goos, windir: windir}
}

func (locator *Locator) Resolve() (string, error) {
	if locator.goos == "windows" && strings.TrimSpace(locator.windir) != "" {
		systemOpenSSH := filepath.Join(locator.windir, "System32", "OpenSSH", "ssh.exe")
		if locator.runner.IsFile(systemOpenSSH) {
			return filepath.Clean(systemOpenSSH), nil
		}
	}

	names := []string{"ssh"}
	if locator.goos == "windows" {
		names = []string{"ssh.exe", "ssh"}
	}
	var lastErr error
	for _, name := range names {
		path, err := locator.runner.LookPath(name)
		if err == nil {
			return path, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		return "", ErrOpenSSHNotFound
	}
	return "", fmt.Errorf("%w: %v", ErrOpenSSHNotFound, lastErr)
}

func (locator *Locator) Probe(ctx context.Context) (ClientInfo, error) {
	path, err := locator.Resolve()
	if err != nil {
		return ClientInfo{}, err
	}
	output, err := locator.runner.CombinedOutput(ctx, path, "-V")
	if err != nil {
		return ClientInfo{}, fmt.Errorf("probe OpenSSH client: %w", err)
	}
	version := parseVersion(output)
	if version == "" {
		return ClientInfo{}, errors.New("OpenSSH client returned an unrecognized version")
	}
	return ClientInfo{Executable: path, Version: version}, nil
}

func parseVersion(output []byte) string {
	match := versionPattern.FindSubmatch(output)
	if len(match) < 2 {
		return ""
	}
	return string(match[1])
}
