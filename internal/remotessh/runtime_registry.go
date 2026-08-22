package remotessh

import (
	"context"
	"errors"
	"sync"
	"time"
)

// RuntimeRegistry is the process-wide ownership point for SSH target
// runtimes. Trust deny, identity drift, target removal, and app quit all revoke
// through this registry instead of reaching into helper processes directly.
type RuntimeRegistry struct {
	mu      sync.Mutex
	targets map[string]*RuntimeLeaseSupervisor
}

func NewRuntimeRegistry() *RuntimeRegistry {
	return &RuntimeRegistry{targets: make(map[string]*RuntimeLeaseSupervisor)}
}

func (registry *RuntimeRegistry) Register(targetID string, runtime *RuntimeLeaseSupervisor) error {
	if !validRuntimeIdentity("target-", targetID) || runtime == nil {
		return errors.New("remote target runtime is invalid")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if existing := registry.targets[targetID]; existing != nil && existing != runtime {
		return errors.New("remote target runtime is already registered")
	}
	registry.targets[targetID] = runtime
	return nil
}

// RevokeTarget synchronously revokes leases before returning. The registry
// retains the closed target entry only until the caller explicitly removes it.
func (registry *RuntimeRegistry) RevokeTarget(ctx context.Context, targetID string) error {
	registry.mu.Lock()
	runtime := registry.targets[targetID]
	registry.mu.Unlock()
	if runtime == nil {
		return nil
	}
	return runtime.Disconnect(ctx)
}

// RevokeAndRemoveTarget stops a target runtime without closing the shared SSH
// connection supervisor. The target may therefore establish a new generation
// and register a fresh runtime after an explicit reconnect.
func (registry *RuntimeRegistry) RevokeAndRemoveTarget(ctx context.Context, targetID string) error {
	registry.mu.Lock()
	runtime := registry.targets[targetID]
	delete(registry.targets, targetID)
	registry.mu.Unlock()
	if runtime == nil {
		return nil
	}
	return runtime.Disconnect(ctx)
}

func (registry *RuntimeRegistry) RemoveTarget(ctx context.Context, targetID string) error {
	registry.mu.Lock()
	runtime := registry.targets[targetID]
	delete(registry.targets, targetID)
	registry.mu.Unlock()
	if runtime == nil {
		return nil
	}
	return runtime.Close(ctx)
}

// Close applies one bounded app-quit deadline across all target runtimes.
func (registry *RuntimeRegistry) Close(timeout time.Duration) error {
	if timeout <= 0 {
		return errors.New("remote runtime shutdown timeout is invalid")
	}
	registry.mu.Lock()
	runtimes := make([]*RuntimeLeaseSupervisor, 0, len(registry.targets))
	for _, runtime := range registry.targets {
		runtimes = append(runtimes, runtime)
	}
	registry.targets = make(map[string]*RuntimeLeaseSupervisor)
	registry.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var wait sync.WaitGroup
	errorsByRuntime := make(chan error, len(runtimes))
	for _, runtime := range runtimes {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := runtime.Close(ctx); err != nil {
				errorsByRuntime <- err
			}
		}()
	}
	wait.Wait()
	close(errorsByRuntime)
	var result error
	for err := range errorsByRuntime {
		result = errors.Join(result, err)
	}
	return result
}
