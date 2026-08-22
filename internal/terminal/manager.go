package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	ptylib "github.com/aymanbagabas/go-pty"
)

const (
	maxReplayBytes    = 1 << 20
	maxActiveSessions = 32
)

var (
	ErrNotRunning    = errors.New("terminal is not running")
	ErrAlreadyClosed = errors.New("terminal manager is closed")
	ErrStopping      = errors.New("terminal is stopping")
	ErrLimitReached  = errors.New("terminal session limit reached")
)

type StartConfig struct {
	ThreadID string
	CWD      string
	Columns  int
	Rows     int
}

type Snapshot struct {
	ThreadID   string
	CWD        string
	Shell      string
	Running    bool
	Generation uint64
	Sequence   uint64
	Output     []byte
}

type Event struct {
	ThreadID   string
	Type       string
	Generation uint64
	Sequence   uint64
	Data       []byte
	ExitCode   int
	Error      string
}

type process interface {
	io.ReadWriteCloser
	Resize(int, int) error
	Wait() error
	Stop() error
	ExitCode() int
}

type starter interface {
	Start(context.Context, StartConfig) (process, string, error)
}

type osStarter struct{}

type osProcess struct {
	pty       ptylib.Pty
	command   *ptylib.Cmd
	closeOnce sync.Once
	closeErr  error
}

func (osStarter) Start(ctx context.Context, config StartConfig) (process, string, error) {
	pseudoterminal, err := ptylib.New()
	if err != nil {
		return nil, "", fmt.Errorf("create pseudo-terminal: %w", err)
	}
	shell, args, err := defaultShell()
	if err != nil {
		_ = pseudoterminal.Close()
		return nil, "", err
	}
	if err := pseudoterminal.Resize(config.Columns, config.Rows); err != nil {
		_ = pseudoterminal.Close()
		return nil, "", fmt.Errorf("resize pseudo-terminal: %w", err)
	}
	command := pseudoterminal.CommandContext(ctx, shell, args...)
	command.Dir = config.CWD
	command.Env = terminalEnvironment(os.Environ())
	if err := command.Start(); err != nil {
		_ = pseudoterminal.Close()
		return nil, "", fmt.Errorf("start shell: %w", err)
	}
	return &osProcess{pty: pseudoterminal, command: command}, shell, nil
}

func (process *osProcess) Read(data []byte) (int, error)  { return process.pty.Read(data) }
func (process *osProcess) Write(data []byte) (int, error) { return process.pty.Write(data) }
func (process *osProcess) Resize(columns, rows int) error { return process.pty.Resize(columns, rows) }
func (process *osProcess) Wait() error                    { return process.command.Wait() }

func (process *osProcess) Close() error {
	process.closeOnce.Do(func() { process.closeErr = process.pty.Close() })
	return process.closeErr
}

func (process *osProcess) Stop() error {
	var killErr error
	if process.command.Process != nil {
		killErr = process.command.Process.Kill()
		if errors.Is(killErr, os.ErrProcessDone) {
			killErr = nil
		}
	}
	return errors.Join(killErr, process.Close())
}

func (process *osProcess) ExitCode() int {
	if process.command.ProcessState == nil {
		return -1
	}
	return process.command.ProcessState.ExitCode()
}

type session struct {
	info      Snapshot
	process   process
	stopping  bool
	finished  bool
	bufferMu  sync.Mutex
	processMu sync.Mutex
}

type Manager struct {
	ctx            context.Context
	starter        starter
	onEvent        func(Event)
	mu             sync.Mutex
	sessions       map[string]*session
	nextGeneration uint64
	closed         bool
}

func NewManager(ctx context.Context, onEvent func(Event)) *Manager {
	return newManager(ctx, osStarter{}, onEvent)
}

func newManager(ctx context.Context, starter starter, onEvent func(Event)) *Manager {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Manager{ctx: ctx, starter: starter, onEvent: onEvent, sessions: make(map[string]*session)}
}

func (manager *Manager) Start(config StartConfig) (Snapshot, error) {
	if err := validateStartConfig(config); err != nil {
		return Snapshot{}, err
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed {
		return Snapshot{}, ErrAlreadyClosed
	}
	if existing := manager.sessions[config.ThreadID]; existing != nil {
		if existing.isStopping() {
			return Snapshot{}, ErrStopping
		}
		if err := existing.resize(config.Columns, config.Rows); err != nil {
			return Snapshot{}, err
		}
		return existing.snapshot(), nil
	}
	if len(manager.sessions) >= maxActiveSessions {
		return Snapshot{}, ErrLimitReached
	}
	process, shell, err := manager.starter.Start(manager.ctx, config)
	if err != nil {
		return Snapshot{}, err
	}
	manager.nextGeneration++
	generation := manager.nextGeneration
	running := &session{
		info:    Snapshot{ThreadID: config.ThreadID, CWD: config.CWD, Shell: shell, Running: true, Generation: generation},
		process: process,
	}
	manager.sessions[config.ThreadID] = running
	go manager.read(running)
	go manager.wait(running)
	return running.snapshot(), nil
}

func (manager *Manager) Snapshot(threadID string) Snapshot {
	manager.mu.Lock()
	running := manager.sessions[strings.TrimSpace(threadID)]
	manager.mu.Unlock()
	if running == nil {
		return Snapshot{ThreadID: strings.TrimSpace(threadID)}
	}
	return running.snapshot()
}

func (manager *Manager) Write(threadID string, data []byte) error {
	running := manager.running(threadID)
	if running == nil {
		return ErrNotRunning
	}
	running.processMu.Lock()
	defer running.processMu.Unlock()
	_, err := running.process.Write(data)
	return err
}

func (manager *Manager) Resize(threadID string, columns, rows int) error {
	if err := validateDimensions(columns, rows); err != nil {
		return err
	}
	running := manager.running(threadID)
	if running == nil {
		return ErrNotRunning
	}
	return running.resize(columns, rows)
}

func (manager *Manager) Stop(threadID string) error {
	running := manager.running(threadID)
	if running == nil {
		return ErrNotRunning
	}
	running.processMu.Lock()
	running.stopping = true
	err := running.process.Stop()
	running.processMu.Unlock()
	return err
}

func (manager *Manager) Shutdown() {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return
	}
	manager.closed = true
	sessions := make([]*session, 0, len(manager.sessions))
	for _, running := range manager.sessions {
		sessions = append(sessions, running)
	}
	manager.mu.Unlock()
	for _, running := range sessions {
		running.processMu.Lock()
		running.stopping = true
		_ = running.process.Stop()
		running.processMu.Unlock()
	}
}

func (manager *Manager) running(threadID string) *session {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.sessions[strings.TrimSpace(threadID)]
}

func (manager *Manager) read(running *session) {
	buffer := make([]byte, 32<<10)
	for {
		count, err := running.process.Read(buffer)
		if count > 0 {
			data := append([]byte(nil), buffer[:count]...)
			sequence := running.appendOutput(data)
			manager.emit(Event{ThreadID: running.info.ThreadID, Type: "output", Generation: running.info.Generation, Sequence: sequence, Data: data})
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !running.suppressReadError() {
				sequence := running.nextSequence()
				manager.emit(Event{ThreadID: running.info.ThreadID, Type: "error", Generation: running.info.Generation, Sequence: sequence, Error: err.Error()})
			}
			return
		}
	}
}

func (manager *Manager) wait(running *session) {
	err := running.process.Wait()
	running.processMu.Lock()
	running.finished = true
	stopping := running.stopping
	running.processMu.Unlock()
	_ = running.process.Close()
	manager.mu.Lock()
	if manager.sessions[running.info.ThreadID] == running {
		delete(manager.sessions, running.info.ThreadID)
	}
	manager.mu.Unlock()
	event := Event{ThreadID: running.info.ThreadID, Type: "exit", Generation: running.info.Generation, Sequence: running.nextSequence(), ExitCode: running.process.ExitCode()}
	if err != nil && !stopping {
		event.Error = err.Error()
	}
	manager.emit(event)
}

func (manager *Manager) emit(event Event) {
	if manager.onEvent != nil {
		manager.onEvent(event)
	}
}

func (running *session) snapshot() Snapshot {
	running.bufferMu.Lock()
	defer running.bufferMu.Unlock()
	result := running.info
	result.Output = append([]byte(nil), running.info.Output...)
	return result
}

func (running *session) appendOutput(data []byte) uint64 {
	running.bufferMu.Lock()
	defer running.bufferMu.Unlock()
	running.info.Sequence++
	if len(data) >= maxReplayBytes {
		running.info.Output = append(running.info.Output[:0], data[len(data)-maxReplayBytes:]...)
		return running.info.Sequence
	}
	overflow := len(running.info.Output) + len(data) - maxReplayBytes
	if overflow > 0 {
		copy(running.info.Output, running.info.Output[overflow:])
		running.info.Output = running.info.Output[:len(running.info.Output)-overflow]
	}
	running.info.Output = append(running.info.Output, data...)
	return running.info.Sequence
}

func (running *session) nextSequence() uint64 {
	running.bufferMu.Lock()
	defer running.bufferMu.Unlock()
	running.info.Sequence++
	return running.info.Sequence
}

func (running *session) resize(columns, rows int) error {
	if err := validateDimensions(columns, rows); err != nil {
		return err
	}
	running.processMu.Lock()
	defer running.processMu.Unlock()
	return running.process.Resize(columns, rows)
}

func (running *session) isStopping() bool {
	running.processMu.Lock()
	defer running.processMu.Unlock()
	return running.stopping
}

func (running *session) suppressReadError() bool {
	running.processMu.Lock()
	defer running.processMu.Unlock()
	return running.stopping || running.finished
}

func validateStartConfig(config StartConfig) error {
	if strings.TrimSpace(config.ThreadID) == "" {
		return errors.New("terminal thread id is required")
	}
	if strings.TrimSpace(config.CWD) == "" {
		return errors.New("terminal working directory is required")
	}
	return validateDimensions(config.Columns, config.Rows)
}

func validateDimensions(columns, rows int) error {
	if columns < 20 || columns > 500 || rows < 5 || rows > 300 {
		return errors.New("terminal dimensions are outside the supported range")
	}
	return nil
}

func terminalEnvironment(environment []string) []string {
	result := append([]string(nil), environment...)
	for key, value := range map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"} {
		prefix := key + "="
		replaced := false
		for index := range result {
			if strings.EqualFold(strings.SplitN(result[index], "=", 2)[0]+"=", prefix) {
				result[index] = prefix + value
				replaced = true
			}
		}
		if !replaced {
			result = append(result, prefix+value)
		}
	}
	return result
}
