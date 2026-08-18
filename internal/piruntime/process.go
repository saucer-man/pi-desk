package piruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"pi-desk/internal/pirpc"
)

type TrustMode string

const (
	TrustApprove TrustMode = "approve"
	TrustDeny    TrustMode = "deny"
)

type StartConfig struct {
	ThreadID       string    `json:"threadId"`
	Workspace      string    `json:"workspace"`
	SessionPath    string    `json:"sessionPath,omitempty"`
	SessionName    string    `json:"sessionName,omitempty"`
	Trust          TrustMode `json:"trust"`
	NoSession      bool      `json:"noSession,omitempty"`
	Offline        bool      `json:"offline,omitempty"`
	DisableThemes  bool      `json:"disableThemes,omitempty"`
	DisableSkills  bool      `json:"disableSkills,omitempty"`
	DisablePlugins bool      `json:"disablePlugins,omitempty"`
	ProxyURL       string    `json:"proxyUrl,omitempty"`
}

type ProcessStarter interface {
	Start(context.Context, StartConfig) (pirpc.Process, error)
}

type ExecStarter struct {
	locator *Locator
}

func NewExecStarter(locator *Locator) *ExecStarter {
	return &ExecStarter{locator: locator}
}

func (starter *ExecStarter) Start(ctx context.Context, config StartConfig) (pirpc.Process, error) {
	workspace, err := validateWorkspace(config.Workspace)
	if err != nil {
		return nil, err
	}
	args, err := buildPiArgs(config)
	if err != nil {
		return nil, err
	}
	invocation, err := starter.locator.Invocation(args...)
	if err != nil {
		return nil, fmt.Errorf("locate Pi CLI: %w", err)
	}

	command := exec.CommandContext(ctx, invocation.Executable, invocation.Args...)
	command.Dir = workspace
	command.Env = processEnvironment(config.ProxyURL)
	configureProcess(command)

	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open Pi stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open Pi stdout: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("open Pi stderr: %w", err)
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start Pi RPC process: %w", err)
	}

	return &execProcess{command: command, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}

func validateWorkspace(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("workspace path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve workspace path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve workspace links: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect workspace: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("workspace path is not a directory")
	}
	return filepath.Clean(resolved), nil
}

func buildPiArgs(config StartConfig) ([]string, error) {
	args := []string{"--mode", "rpc"}
	if config.DisableThemes {
		args = append(args, "--no-themes")
	}
	if config.Offline {
		args = append(args, "--offline")
	}
	if config.DisableSkills {
		args = append(args, "--no-skills")
	}
	if config.DisablePlugins {
		args = append(args, "--no-extensions")
	}
	if config.NoSession {
		args = append(args, "--no-session")
	}
	if config.SessionPath != "" {
		absolute, err := filepath.Abs(config.SessionPath)
		if err != nil {
			return nil, fmt.Errorf("resolve session path: %w", err)
		}
		args = append(args, "--session", filepath.Clean(absolute))
	}
	if config.SessionName != "" {
		args = append(args, "--name", config.SessionName)
	}
	switch config.Trust {
	case TrustApprove:
		args = append(args, "--approve")
	case TrustDeny:
		args = append(args, "--no-approve")
	default:
		return nil, errors.New("workspace trust decision is required")
	}
	return args, nil
}

func processEnvironment(proxyURL string) []string {
	environment := append([]string(nil), os.Environ()...)
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return environment
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		environment = append(environment, key+"="+proxyURL)
	}
	return environment
}

type execProcess struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.Reader
	stderr  io.Reader
}

func (process *execProcess) Stdin() io.WriteCloser { return process.stdin }
func (process *execProcess) Stdout() io.Reader     { return process.stdout }
func (process *execProcess) Stderr() io.Reader     { return process.stderr }
func (process *execProcess) Wait() error           { return process.command.Wait() }
func (process *execProcess) Kill() error           { return killProcessTree(process.command) }
