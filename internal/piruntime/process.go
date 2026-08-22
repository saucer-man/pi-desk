package piruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"pi-desk/internal/pirpc"
)

type TrustMode string

const (
	TrustApprove TrustMode = "approve"
	TrustDeny    TrustMode = "deny"
)

type StartConfig struct {
	ThreadID            string    `json:"threadId"`
	Workspace           string    `json:"workspace"`
	SessionPath         string    `json:"sessionPath,omitempty"`
	SessionName         string    `json:"sessionName,omitempty"`
	Trust               TrustMode `json:"trust"`
	NoSession           bool      `json:"noSession,omitempty"`
	Offline             bool      `json:"offline,omitempty"`
	DisableThemes       bool      `json:"disableThemes,omitempty"`
	DisableSkills       bool      `json:"disableSkills,omitempty"`
	DisablePlugins      bool      `json:"disablePlugins,omitempty"`
	ProxyURL            string    `json:"proxyUrl,omitempty"`
	RemoteAdapter       string    `json:"-"`
	RemoteSocket        string    `json:"-"`
	RemoteToken         string    `json:"-"`
	RemoteRoot          string    `json:"-"`
	RemoteAdapterSHA256 string    `json:"-"`
	RemoteAdapterSize   int64     `json:"-"`
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
	command.Env, err = processEnvironment(config)
	if err != nil {
		return nil, err
	}
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
	if config.RemoteAdapter != "" {
		adapter, err := validateRemoteAdapter(config)
		if err != nil {
			return nil, err
		}
		args = append(args, "--no-builtin-tools", "--no-extensions", "--no-context-files", "--extension", adapter)
	} else if config.DisablePlugins {
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

func processEnvironment(config StartConfig) ([]string, error) {
	environment := make([]string, 0, len(os.Environ())+9)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "PI_DESK_REMOTE_SOCKET") && !strings.EqualFold(key, "PI_DESK_REMOTE_TOKEN") && !strings.EqualFold(key, "PI_DESK_REMOTE_ROOT") {
			environment = append(environment, entry)
		}
	}
	proxyURL := strings.TrimSpace(config.ProxyURL)
	if proxyURL != "" {
		for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
			environment = append(environment, key+"="+proxyURL)
		}
	}
	if config.RemoteAdapter != "" {
		if _, err := validateRemoteAdapter(config); err != nil {
			return nil, err
		}
		environment = append(environment,
			"PI_DESK_REMOTE_SOCKET="+config.RemoteSocket,
			"PI_DESK_REMOTE_TOKEN="+config.RemoteToken,
			"PI_DESK_REMOTE_ROOT="+config.RemoteRoot,
		)
	}
	return environment, nil
}

func validateRemoteAdapter(config StartConfig) (string, error) {
	adapter := filepath.Clean(strings.TrimSpace(config.RemoteAdapter))
	if adapter == "." || !filepath.IsAbs(adapter) {
		return "", errors.New("remote adapter path is invalid")
	}
	info, err := os.Lstat(adapter)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > 1<<20 || info.Size() != config.RemoteAdapterSize || !validLowerHexToken(config.RemoteAdapterSHA256) {
		return "", errors.New("remote adapter file is invalid")
	}
	content, err := os.ReadFile(adapter)
	if err != nil || int64(len(content)) != config.RemoteAdapterSize {
		return "", errors.New("remote adapter file is invalid")
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != config.RemoteAdapterSHA256 {
		return "", errors.New("remote adapter file hash is invalid")
	}
	socket := filepath.Clean(strings.TrimSpace(config.RemoteSocket))
	if socket == "." || !filepath.IsAbs(socket) || !validLowerHexToken(config.RemoteToken) || !validRemotePOSIXRoot(config.RemoteRoot) {
		return "", errors.New("remote adapter configuration is invalid")
	}
	return adapter, nil
}

func validLowerHexToken(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func validRemotePOSIXRoot(value string) bool {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) || !path.IsAbs(value) || path.Clean(value) != value || strings.Contains(value, "\\") {
		return false
	}
	for _, character := range value {
		if character == 0 || unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}

type execProcess struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.Reader
	stderr  io.Reader
}

func (process *execProcess) PID() int {
	if process == nil || process.command == nil || process.command.Process == nil {
		return 0
	}
	return process.command.Process.Pid
}

func (process *execProcess) Stdin() io.WriteCloser { return process.stdin }
func (process *execProcess) Stdout() io.Reader     { return process.stdout }
func (process *execProcess) Stderr() io.Reader     { return process.stderr }
func (process *execProcess) Wait() error           { return process.command.Wait() }
func (process *execProcess) Kill() error           { return killProcessTree(process.command) }
