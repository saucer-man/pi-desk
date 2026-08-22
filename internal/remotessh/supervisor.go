package remotessh

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrConnectionSupervisorClosed  = errors.New("SSH connection supervisor is closed")
	ErrConnectionInProgress        = errors.New("SSH connection attempt is already in progress")
	ErrConnectionGenerationRevoked = errors.New("SSH connection generation is no longer ready")
	ErrConnectionIdentityChanged   = errors.New("SSH target identity changed since the prior successful connection")
)

// ConnectionState is the fail-closed lifecycle state of a target preflight.
// Ready means only that the fixed strict probe completed successfully. It does
// not imply a reusable OpenSSH master connection or a running helper.
type ConnectionState string

const (
	ConnectionDisconnected ConnectionState = "disconnected"
	ConnectionConnecting   ConnectionState = "connecting"
	ConnectionReady        ConnectionState = "ready"
	ConnectionClosed       ConnectionState = "closed"
)

// ConnectionBinding is the non-secret identity evidence retained by the
// supervisor. Host names, users, aliases, raw ssh -G data, and debug output
// are deliberately excluded.
type ConnectionBinding struct {
	ConfigFingerprint string
	HostKey           HostKeyEvidence
}

// ConnectionSnapshot is an immutable projection of a target supervisor. A
// nonzero Generation is valid only while State is ConnectionReady. Failure is
// nil for connecting and ready states.
type ConnectionSnapshot struct {
	State      ConnectionState
	Generation uint64
	Binding    ConnectionBinding
	Failure    *ConnectionFailure
}

// ConnectionSupervisorError maps lifecycle failures to a stable redacted
// error. It never includes an SSH target, command, remote path, or diagnostic
// output.
type ConnectionSupervisorError struct {
	Failure ConnectionFailure
	cause   error
}

func (err *ConnectionSupervisorError) Error() string {
	return fmt.Sprintf("%s: %s", err.Failure.Code, err.Failure.Reason)
}

func (err *ConnectionSupervisorError) Unwrap() error {
	return err.cause
}

type connectionProber interface {
	ProbeConnection(context.Context, string) (ConnectionPreflight, error)
}

// ConnectionSupervisor owns the pre-helper lifecycle for exactly one
// concrete SSH target. It serializes explicit connection attempts, revokes an
// old generation before starting a new one, and rejects changed effective
// config or host-key evidence instead of silently replacing its binding.
type ConnectionSupervisor struct {
	prober    connectionProber
	locator   *Locator
	hostAlias string

	mu                sync.Mutex
	state             ConnectionState
	nextGeneration    uint64
	liveGeneration    uint64
	binding           ConnectionBinding
	hasBinding        bool
	failure           *ConnectionFailure
	attempt           uint64
	attemptCancel     context.CancelFunc
	nextContextID     uint64
	generationCancels map[uint64]map[uint64]context.CancelFunc
	generationDone    map[uint64]chan struct{}
}

// NewConnectionSupervisor constructs a target-bound supervisor. The caller
// must obtain Target through NewTarget; the alias is revalidated here to avoid
// accepting a manually constructed invalid Target.
func NewConnectionSupervisor(locator *Locator, target Target) (*ConnectionSupervisor, error) {
	if locator == nil {
		return nil, errors.New("SSH locator is required")
	}
	supervisor, err := newConnectionSupervisor(locator, target)
	if err != nil {
		return nil, err
	}
	supervisor.locator = locator
	return supervisor, nil
}

func newConnectionSupervisor(prober connectionProber, target Target) (*ConnectionSupervisor, error) {
	if prober == nil {
		return nil, errors.New("SSH connection prober is required")
	}
	validated, err := NewTarget(target.HostAlias)
	if err != nil {
		return nil, err
	}
	return &ConnectionSupervisor{
		prober:            prober,
		hostAlias:         validated.HostAlias,
		state:             ConnectionDisconnected,
		generationCancels: make(map[uint64]map[uint64]context.CancelFunc),
		generationDone:    make(map[uint64]chan struct{}),
	}, nil
}

// Connect starts a strict explicit preflight when disconnected. Calls while a
// generation is ready reuse only its in-memory binding and make no network
// request. A concurrent direct caller receives a stable in-progress error;
// lifecycle callers serialize target operations before reaching this method.
func (supervisor *ConnectionSupervisor) Connect(ctx context.Context) (ConnectionSnapshot, error) {
	attempt, attemptContext, snapshot, err := supervisor.beginConnect(ctx)
	if err != nil || attempt == 0 {
		return snapshot, err
	}
	defer attemptContext.cancel()

	preflight, probeErr := supervisor.prober.ProbeConnection(attemptContext.ctx, supervisor.hostAlias)
	return supervisor.finishConnect(attempt, preflight, probeErr)
}

type supervisedAttempt struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (supervisor *ConnectionSupervisor) beginConnect(ctx context.Context) (uint64, supervisedAttempt, ConnectionSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()

	switch supervisor.state {
	case ConnectionClosed:
		return 0, supervisedAttempt{}, supervisor.snapshotLocked(), lifecycleError(FailureDisconnected, ReasonDisconnected, ErrConnectionSupervisorClosed)
	case ConnectionReady:
		return 0, supervisedAttempt{}, supervisor.snapshotLocked(), nil
	case ConnectionConnecting:
		return 0, supervisedAttempt{}, supervisor.snapshotLocked(), lifecycleError(FailureConnect, ReasonConnectionInProgress, ErrConnectionInProgress)
	}

	attemptContext, cancel := context.WithCancel(ctx)
	supervisor.nextGeneration++
	supervisor.attempt++
	attempt := supervisor.attempt
	supervisor.state = ConnectionConnecting
	supervisor.liveGeneration = 0
	supervisor.failure = nil
	supervisor.attemptCancel = cancel
	return attempt, supervisedAttempt{ctx: attemptContext, cancel: cancel}, supervisor.snapshotLocked(), nil
}

func (supervisor *ConnectionSupervisor) finishConnect(attempt uint64, preflight ConnectionPreflight, probeErr error) (ConnectionSnapshot, error) {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()

	if supervisor.state != ConnectionConnecting || supervisor.attempt != attempt {
		return supervisor.revokedAttemptSnapshotLocked(), lifecycleError(FailureDisconnected, ReasonDisconnected, ErrConnectionGenerationRevoked)
	}
	supervisor.attemptCancel = nil

	if probeErr != nil {
		failure := connectionFailureFromError(probeErr)
		supervisor.state = ConnectionDisconnected
		supervisor.failure = &failure
		return supervisor.snapshotLocked(), probeErr
	}

	binding, err := bindingFromPreflight(preflight)
	if err != nil {
		failure := ConnectionFailure{Code: FailureConnect, Reason: ReasonHostKeyEvidence}
		supervisor.state = ConnectionDisconnected
		supervisor.failure = &failure
		return supervisor.snapshotLocked(), lifecycleError(failure.Code, failure.Reason, err)
	}
	if supervisor.hasBinding && supervisor.binding != binding {
		failure := ConnectionFailure{Code: FailureConnect, Reason: ReasonIdentityChanged}
		supervisor.state = ConnectionDisconnected
		supervisor.failure = &failure
		return supervisor.snapshotLocked(), lifecycleError(failure.Code, failure.Reason, ErrConnectionIdentityChanged)
	}

	supervisor.binding = binding
	supervisor.hasBinding = true
	supervisor.liveGeneration = supervisor.nextGeneration
	supervisor.state = ConnectionReady
	return supervisor.snapshotLocked(), nil
}

// Disconnect revokes the ready generation before cancelling an in-flight
// preflight. It is idempotent and never initiates network activity.
func (supervisor *ConnectionSupervisor) Disconnect() ConnectionSnapshot {
	return supervisor.revoke(false)
}

// WaitForGenerationIdle waits until all helper/bootstrap operations bound to a
// revoked generation have released their contexts. It is a teardown barrier:
// a caller may not start a replacement generation until this returns.
func (supervisor *ConnectionSupervisor) WaitForGenerationIdle(ctx context.Context, generation uint64) error {
	if generation == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	supervisor.mu.Lock()
	done := supervisor.generationDone[generation]
	supervisor.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close permanently revokes the generation and cancels an in-flight preflight.
// A closed supervisor cannot reconnect and must be replaced with a newly
// approved target-bound instance.
func (supervisor *ConnectionSupervisor) Close() ConnectionSnapshot {
	return supervisor.revoke(true)
}

func (supervisor *ConnectionSupervisor) revoke(close bool) ConnectionSnapshot {
	supervisor.mu.Lock()
	if supervisor.state == ConnectionClosed {
		snapshot := supervisor.snapshotLocked()
		supervisor.mu.Unlock()
		return snapshot
	}
	cancel := supervisor.attemptCancel
	supervisor.attemptCancel = nil
	generationCancels := supervisor.takeGenerationCancelsLocked(supervisor.liveGeneration)
	supervisor.attempt++ // Invalidates any late probe completion.
	supervisor.liveGeneration = 0
	if close {
		supervisor.state = ConnectionClosed
	} else {
		supervisor.state = ConnectionDisconnected
	}
	failure := ConnectionFailure{Code: FailureDisconnected, Reason: ReasonDisconnected}
	supervisor.failure = &failure
	snapshot := supervisor.snapshotLocked()
	supervisor.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	for _, generationCancel := range generationCancels {
		generationCancel()
	}
	return snapshot
}

// Snapshot returns a copy of current state. It cannot be used as authorization;
// callers must call ValidateGeneration immediately before dispatching a future
// helper operation.
func (supervisor *ConnectionSupervisor) Snapshot() ConnectionSnapshot {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	return supervisor.snapshotLocked()
}

// ValidateGeneration authorizes only the currently ready generation. Any
// disconnect, failed reconnect, close, or later generation fails closed.
func (supervisor *ConnectionSupervisor) ValidateGeneration(generation uint64) error {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if generation == 0 || supervisor.state != ConnectionReady || supervisor.liveGeneration != generation {
		return lifecycleError(FailureDisconnected, ReasonDisconnected, ErrConnectionGenerationRevoked)
	}
	return nil
}

func (supervisor *ConnectionSupervisor) revokedAttemptSnapshotLocked() ConnectionSnapshot {
	state := ConnectionDisconnected
	if supervisor.state == ConnectionClosed {
		state = ConnectionClosed
	}
	failure := ConnectionFailure{Code: FailureDisconnected, Reason: ReasonDisconnected}
	return ConnectionSnapshot{State: state, Failure: &failure}
}

// bindGenerationContext ties a bootstrap operation to an already-ready
// generation. It is package-private so only host-owned remote transport code
// can use it; public callers must never treat it as a workspace lease.
func (supervisor *ConnectionSupervisor) bindGenerationContext(parent context.Context, generation uint64) (context.Context, func(), error) {
	if parent == nil {
		parent = context.Background()
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if generation == 0 || supervisor.state != ConnectionReady || supervisor.liveGeneration != generation {
		return nil, nil, lifecycleError(FailureDisconnected, ReasonDisconnected, ErrConnectionGenerationRevoked)
	}
	ctx, cancel := context.WithCancel(parent)
	supervisor.nextContextID++
	contextID := supervisor.nextContextID
	contexts := supervisor.generationCancels[generation]
	if contexts == nil {
		contexts = make(map[uint64]context.CancelFunc)
		supervisor.generationCancels[generation] = contexts
		supervisor.generationDone[generation] = make(chan struct{})
	}
	contexts[contextID] = cancel
	var releaseOnce sync.Once
	return ctx, func() {
		releaseOnce.Do(func() {
			cancel()
			supervisor.mu.Lock()
			defer supervisor.mu.Unlock()
			delete(contexts, contextID)
			if len(contexts) == 0 {
				delete(supervisor.generationCancels, generation)
				if done := supervisor.generationDone[generation]; done != nil {
					close(done)
					delete(supervisor.generationDone, generation)
				}
			}
		})
	}, nil
}

func (supervisor *ConnectionSupervisor) takeGenerationCancelsLocked(generation uint64) []context.CancelFunc {
	if generation == 0 {
		return nil
	}
	contexts := supervisor.generationCancels[generation]
	delete(supervisor.generationCancels, generation)
	cancels := make([]context.CancelFunc, 0, len(contexts))
	for _, cancel := range contexts {
		cancels = append(cancels, cancel)
	}
	return cancels
}

func (supervisor *ConnectionSupervisor) snapshotLocked() ConnectionSnapshot {
	snapshot := ConnectionSnapshot{State: supervisor.state}
	if supervisor.state == ConnectionReady {
		snapshot.Generation = supervisor.liveGeneration
		snapshot.Binding = supervisor.binding
	}
	if supervisor.failure != nil {
		failure := *supervisor.failure
		snapshot.Failure = &failure
	}
	return snapshot
}

func bindingFromPreflight(preflight ConnectionPreflight) (ConnectionBinding, error) {
	if preflight.Config.Fingerprint == "" || preflight.HostKey.Algorithm == "" || preflight.HostKey.SHA256Hash == "" {
		return ConnectionBinding{}, errors.New("SSH preflight returned incomplete identity evidence")
	}
	return ConnectionBinding{
		ConfigFingerprint: preflight.Config.Fingerprint,
		HostKey:           preflight.HostKey,
	}, nil
}

func connectionFailureFromError(err error) ConnectionFailure {
	var probeErr *ConnectionProbeError
	if errors.As(err, &probeErr) {
		return probeErr.Failure
	}
	var lifecycleErr *ConnectionSupervisorError
	if errors.As(err, &lifecycleErr) {
		return lifecycleErr.Failure
	}
	return ConnectionFailure{Code: FailureConnect, Reason: ReasonUnknown}
}

func lifecycleError(code FailureCode, reason FailureReason, cause error) *ConnectionSupervisorError {
	return &ConnectionSupervisorError{
		Failure: ConnectionFailure{Code: code, Reason: reason},
		cause:   cause,
	}
}
