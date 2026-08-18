package piruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"pi-desk/internal/pirpc"
)

const defaultReadyTimeout = 20 * time.Second

var (
	ErrThreadRunning    = errors.New("Pi thread is already running")
	ErrThreadNotRunning = errors.New("Pi thread is not running")
)

type SessionEvent struct {
	ThreadID string      `json:"threadId"`
	Event    pirpc.Event `json:"event"`
}

type SessionInfo struct {
	ThreadID   string          `json:"threadId"`
	Generation uint64          `json:"generation"`
	State      json.RawMessage `json:"state"`
}

type managedSession struct {
	generation uint64
	client     *pirpc.Client
}

type Supervisor struct {
	ctx     context.Context
	cancel  context.CancelFunc
	starter ProcessStarter
	sink    func(SessionEvent)

	nextGeneration atomic.Uint64
	mu             sync.RWMutex
	sessions       map[string]*managedSession
	starting       map[string]struct{}
	shutdownOnce   sync.Once
}

func NewSupervisor(parent context.Context, starter ProcessStarter, sink func(SessionEvent)) *Supervisor {
	ctx, cancel := context.WithCancel(parent)
	if sink == nil {
		sink = func(SessionEvent) {}
	}
	return &Supervisor{
		ctx:      ctx,
		cancel:   cancel,
		starter:  starter,
		sink:     sink,
		sessions: make(map[string]*managedSession),
		starting: make(map[string]struct{}),
	}
}

func (supervisor *Supervisor) Start(ctx context.Context, config StartConfig) (SessionInfo, error) {
	if config.ThreadID == "" {
		return SessionInfo{}, errors.New("thread id is required")
	}

	supervisor.mu.Lock()
	_, running := supervisor.sessions[config.ThreadID]
	_, starting := supervisor.starting[config.ThreadID]
	if running || starting {
		supervisor.mu.Unlock()
		return SessionInfo{}, ErrThreadRunning
	}
	supervisor.starting[config.ThreadID] = struct{}{}
	supervisor.mu.Unlock()
	registered := false
	defer func() {
		if registered {
			return
		}
		supervisor.mu.Lock()
		delete(supervisor.starting, config.ThreadID)
		supervisor.mu.Unlock()
	}()

	process, err := supervisor.starter.Start(supervisor.ctx, config)
	if err != nil {
		return SessionInfo{}, err
	}
	generation := supervisor.nextGeneration.Add(1)

	var client *pirpc.Client
	client = pirpc.NewClient(process, generation, func(event pirpc.Event) {
		if supervisor.isCurrent(config.ThreadID, generation, client) {
			supervisor.sink(SessionEvent{ThreadID: config.ThreadID, Event: event})
		}
	})
	managed := &managedSession{generation: generation, client: client}

	supervisor.mu.Lock()
	delete(supervisor.starting, config.ThreadID)
	if _, exists := supervisor.sessions[config.ThreadID]; exists {
		supervisor.mu.Unlock()
		_ = client.Close()
		return SessionInfo{}, ErrThreadRunning
	}
	supervisor.sessions[config.ThreadID] = managed
	registered = true
	supervisor.mu.Unlock()
	go supervisor.watch(config.ThreadID, managed)

	readyContext := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		readyContext, cancel = context.WithTimeout(ctx, defaultReadyTimeout)
		defer cancel()
	}
	response, err := client.Call(readyContext, map[string]any{"type": "get_state"})
	if err != nil {
		supervisor.remove(config.ThreadID, managed)
		_ = client.Close()
		return SessionInfo{}, fmt.Errorf("wait for Pi RPC readiness: %w", err)
	}

	return SessionInfo{
		ThreadID:   config.ThreadID,
		Generation: generation,
		State:      append(json.RawMessage(nil), response.Data...),
	}, nil
}

func (supervisor *Supervisor) Call(ctx context.Context, threadID string, command map[string]any) (pirpc.Response, error) {
	session, err := supervisor.session(threadID)
	if err != nil {
		return pirpc.Response{}, err
	}
	return session.client.Call(ctx, command)
}

func (supervisor *Supervisor) Send(threadID string, command map[string]any) error {
	session, err := supervisor.session(threadID)
	if err != nil {
		return err
	}
	return session.client.Send(command)
}

func (supervisor *Supervisor) Stop(threadID string) error {
	supervisor.mu.Lock()
	session, exists := supervisor.sessions[threadID]
	if exists {
		delete(supervisor.sessions, threadID)
	}
	supervisor.mu.Unlock()
	if !exists {
		return ErrThreadNotRunning
	}
	return session.client.Close()
}

func (supervisor *Supervisor) Diagnostics(threadID string) (string, error) {
	session, err := supervisor.session(threadID)
	if err != nil {
		return "", err
	}
	return session.client.Diagnostics(), nil
}

func (supervisor *Supervisor) Running() []SessionInfo {
	supervisor.mu.RLock()
	defer supervisor.mu.RUnlock()
	result := make([]SessionInfo, 0, len(supervisor.sessions))
	for threadID, session := range supervisor.sessions {
		result = append(result, SessionInfo{ThreadID: threadID, Generation: session.generation})
	}
	return result
}

// ActiveCount includes processes that are still starting. Shared Pi files must
// not be replaced while either a ready or an initializing process may use them.
func (supervisor *Supervisor) ActiveCount() int {
	supervisor.mu.RLock()
	defer supervisor.mu.RUnlock()
	return len(supervisor.sessions) + len(supervisor.starting)
}

// StopAll closes every ready session without shutting down the supervisor, so
// new sessions can start again after Pi CLI maintenance completes.
func (supervisor *Supervisor) StopAll() error {
	supervisor.mu.Lock()
	if len(supervisor.starting) > 0 {
		supervisor.mu.Unlock()
		return errors.New("cannot stop Pi sessions while a session is starting")
	}
	sessions := supervisor.sessions
	supervisor.sessions = make(map[string]*managedSession)
	supervisor.mu.Unlock()

	errs := make([]error, 0)
	for _, session := range sessions {
		if err := session.client.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (supervisor *Supervisor) Shutdown() {
	supervisor.shutdownOnce.Do(func() {
		supervisor.cancel()
		supervisor.mu.Lock()
		sessions := supervisor.sessions
		supervisor.sessions = make(map[string]*managedSession)
		supervisor.mu.Unlock()
		for _, session := range sessions {
			_ = session.client.Close()
		}
	})
}

func (supervisor *Supervisor) watch(threadID string, session *managedSession) {
	<-session.client.Done()
	if !supervisor.remove(threadID, session) {
		return
	}
	errorMessage := "Pi RPC process exited"
	if err := session.client.Err(); err != nil {
		errorMessage = err.Error()
	}
	supervisor.sink(SessionEvent{
		ThreadID: threadID,
		Event: pirpc.Event{
			Generation: session.generation,
			Type:       "runtime_exit",
			Error:      errorMessage,
		},
	})
}

func (supervisor *Supervisor) session(threadID string) (*managedSession, error) {
	supervisor.mu.RLock()
	session, exists := supervisor.sessions[threadID]
	supervisor.mu.RUnlock()
	if !exists {
		return nil, ErrThreadNotRunning
	}
	return session, nil
}

func (supervisor *Supervisor) isCurrent(threadID string, generation uint64, client *pirpc.Client) bool {
	supervisor.mu.RLock()
	session := supervisor.sessions[threadID]
	supervisor.mu.RUnlock()
	return session != nil && session.generation == generation && session.client == client
}

func (supervisor *Supervisor) remove(threadID string, expected *managedSession) bool {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if supervisor.sessions[threadID] != expected {
		return false
	}
	delete(supervisor.sessions, threadID)
	return true
}
