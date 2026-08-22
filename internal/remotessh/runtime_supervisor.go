package remotessh

import (
	"context"
	"errors"
	"path"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	defaultRuntimeIdleTimeout     = 5 * time.Minute
	defaultRuntimeGracefulTimeout = 500 * time.Millisecond
	defaultRuntimeCleanupTimeout  = 5 * time.Second
	defaultRuntimeReadTimeout     = 2 * time.Minute
	maxRuntimeTaskLeases          = 16
	maxRuntimeReadLeases          = 32
)

var (
	ErrHelperRuntimeClosed       = errors.New("remote helper runtime supervisor is closed")
	ErrHelperRuntimeStopping     = errors.New("remote helper runtime is stopping")
	ErrHelperRuntimeStart        = errors.New("remote helper runtime start failed")
	ErrHelperRuntimeIdentity     = errors.New("remote helper runtime identity mismatch")
	ErrHelperRuntimeLimit        = errors.New("remote helper runtime lease limit reached")
	ErrHelperRuntimeDuplicate    = errors.New("remote helper runtime lease owner already exists")
	ErrHelperRootBindingChanged  = errors.New("remote helper root capability binding changed")
	ErrHelperRuntimeInvalidLease = errors.New("remote helper runtime lease request is invalid")
	ErrHelperRuntimeShutdown     = errors.New("remote helper runtime shutdown failed")
	ErrHelperRootInvalid         = errors.New("remote helper root request is invalid")
	ErrHelperRootOpen            = errors.New("remote helper root open failed")
	ErrHelperRootUnsupported     = errors.New("remote helper root identity is unsupported")
)

type RuntimeState string

const (
	RuntimeStopped  RuntimeState = "stopped"
	RuntimeStarting RuntimeState = "starting"
	RuntimeReady    RuntimeState = "ready"
	RuntimeStopping RuntimeState = "stopping"
	RuntimeClosed   RuntimeState = "closed"
)

type RuntimeLeaseKind string

const (
	RuntimeTaskLease RuntimeLeaseKind = "task"
	RuntimeReadLease RuntimeLeaseKind = "read"
)

// HelperGeneration is one already-authenticated helper process. Shutdown must
// ask the helper to terminate and wait for process exit. Kill must only kill
// the local OpenSSH process tree. Done must close exactly once when that tree
// exits. Implementations must bind process lifetime to the supplied lifetime
// context and must not reconnect or replay operations.
type HelperGeneration interface {
	Generation() uint64
	Shutdown(context.Context) error
	Kill() error
	Done() <-chan struct{}
}

// HelperGenerationFactory starts an exact helper for a generation that already
// passed strict connection preflight. startupContext bounds the handshake;
// lifetimeContext is cancelled by connection generation revoke.
type HelperGenerationFactory interface {
	Start(startupContext, lifetimeContext context.Context, generation uint64) (HelperGeneration, error)
}

// RuntimeRootCapability can only be minted by package-owned helper root-open
// code. There is intentionally no exported constructor: an appservice or UI
// string must never become a root capability.
type RuntimeRootCapability struct {
	generation  uint64
	workspaceID string
	rootHandle  string
}

func (capability *RuntimeRootCapability) Generation() uint64 {
	if capability == nil {
		return 0
	}
	return capability.generation
}

func (capability *RuntimeRootCapability) WorkspaceID() string {
	if capability == nil {
		return ""
	}
	return capability.workspaceID
}

func newRuntimeRootCapability(generation uint64, workspaceID, rootHandle string) (*RuntimeRootCapability, error) {
	capability := &RuntimeRootCapability{generation: generation, workspaceID: workspaceID, rootHandle: rootHandle}
	if !validRuntimeRootCapability(capability) {
		return nil, runtimeLifecycleError(ErrHelperRuntimeInvalidLease)
	}
	return capability, nil
}

type RuntimeLeaseRequest struct {
	Root    *RuntimeRootCapability
	OwnerID string
}

type RuntimeRootIdentity struct {
	CanonicalPath string
	Device        uint64
	Inode         uint64
}

type RuntimeRootOpenRequest struct {
	Generation    uint64
	WorkspaceID   string
	RequestedRoot string
	Expected      *RuntimeRootIdentity
}

type RuntimeRootOpenResult struct {
	Capability *RuntimeRootCapability
	Identity   RuntimeRootIdentity
}

type RuntimeSnapshot struct {
	State       RuntimeState
	Generation  uint64
	TaskLeases  int
	ReadLeases  int
	IdlePending bool
}

// RuntimeLease is a host-owned capability. Its context is cancelled before
// helper shutdown on disconnect, trust revoke, idle expiry, helper exit, or
// application close. Release is idempotent.
type RuntimeLease struct {
	kind        RuntimeLeaseKind
	generation  uint64
	workspaceID string
	rootHandle  string
	ownerID     string
	id          uint64
	ctx         context.Context
	supervisor  *RuntimeLeaseSupervisor
	once        sync.Once
}

func (lease *RuntimeLease) Kind() RuntimeLeaseKind {
	if lease == nil {
		return ""
	}
	return lease.kind
}

func (lease *RuntimeLease) Generation() uint64 {
	if lease == nil {
		return 0
	}
	return lease.generation
}

func (lease *RuntimeLease) WorkspaceID() string {
	if lease == nil {
		return ""
	}
	return lease.workspaceID
}

func (lease *RuntimeLease) RootHandle() string {
	if lease == nil {
		return ""
	}
	return lease.rootHandle
}

func (lease *RuntimeLease) OwnerID() string {
	if lease == nil {
		return ""
	}
	return lease.ownerID
}

func (lease *RuntimeLease) Context() context.Context {
	if lease == nil || lease.ctx == nil {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	return lease.ctx
}

func (lease *RuntimeLease) Release() {
	if lease == nil || lease.supervisor == nil {
		return
	}
	lease.once.Do(func() {
		lease.supervisor.releaseLease(lease.id)
	})
}

type rootOpeningGeneration interface {
	OpenRoot(context.Context, string) (rootOpenResponse, error)
}

type rootOpenResponse struct {
	Handle        string
	CanonicalPath string
	Device        uint64
	Inode         uint64
}

type runtimeLeaseRecord struct {
	kind        RuntimeLeaseKind
	workspaceID string
	rootHandle  string
	ownerKey    string
	cancel      context.CancelFunc
}

// RuntimeLeaseSupervisor owns one target's helper process and all host-side
// leases for its current connection generation. It never calls Connect: only
// an explicit caller may establish a disconnected target before acquisition.
type RuntimeLeaseSupervisor struct {
	connection *ConnectionSupervisor
	factory    HelperGenerationFactory

	mu             sync.Mutex
	state          RuntimeState
	epoch          uint64
	runtime        HelperGeneration
	runtimeContext context.Context
	runtimeRelease func()
	startDone      chan struct{}
	startCancel    context.CancelFunc
	nextLeaseID    uint64
	leases         map[uint64]runtimeLeaseRecord
	owners         map[string]uint64
	terminalLeases map[uint64]struct{}
	rootBindings   map[string]string
	rootOwners     map[string]string
	taskLeases     int
	readLeases     int
	idleTimer      *time.Timer

	idleTimeout     time.Duration
	readTimeout     time.Duration
	gracefulTimeout time.Duration
	cleanupTimeout  time.Duration
}

func NewRuntimeLeaseSupervisor(connection *ConnectionSupervisor, factory HelperGenerationFactory) (*RuntimeLeaseSupervisor, error) {
	return newRuntimeLeaseSupervisor(connection, factory, defaultRuntimeIdleTimeout, defaultRuntimeGracefulTimeout, defaultRuntimeCleanupTimeout)
}

func newRuntimeLeaseSupervisor(connection *ConnectionSupervisor, factory HelperGenerationFactory, idleTimeout, gracefulTimeout, cleanupTimeout time.Duration) (*RuntimeLeaseSupervisor, error) {
	if connection == nil || factory == nil {
		return nil, errors.New("connection supervisor and helper runtime factory are required")
	}
	if idleTimeout <= 0 || gracefulTimeout <= 0 || cleanupTimeout < gracefulTimeout {
		return nil, errors.New("remote helper runtime timeouts are invalid")
	}
	return &RuntimeLeaseSupervisor{
		connection:      connection,
		factory:         factory,
		state:           RuntimeStopped,
		leases:          make(map[uint64]runtimeLeaseRecord),
		owners:          make(map[string]uint64),
		terminalLeases:  make(map[uint64]struct{}),
		rootBindings:    make(map[string]string),
		rootOwners:      make(map[string]string),
		idleTimeout:     idleTimeout,
		readTimeout:     defaultRuntimeReadTimeout,
		gracefulTimeout: gracefulTimeout,
		cleanupTimeout:  cleanupTimeout,
	}, nil
}

// OpenRoot asks the exact helper to mint a root capability, then binds it to
// one immutable WorkspaceID. It cannot open a disconnected target and revokes
// the generation if helper identity or the expected persisted root drifts.
func (supervisor *RuntimeLeaseSupervisor) OpenRoot(ctx context.Context, request RuntimeRootOpenRequest) (RuntimeRootOpenResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateRuntimeRootOpenRequest(request); err != nil {
		return RuntimeRootOpenResult{}, err
	}
	if err := supervisor.connection.ValidateGeneration(request.Generation); err != nil {
		return RuntimeRootOpenResult{}, err
	}
	if err := supervisor.ensureRuntime(ctx, request.Generation); err != nil {
		return RuntimeRootOpenResult{}, err
	}

	supervisor.mu.Lock()
	runtime := supervisor.runtime
	epoch := supervisor.epoch
	if supervisor.state != RuntimeReady || runtime == nil || runtime.Generation() != request.Generation {
		supervisor.mu.Unlock()
		return RuntimeRootOpenResult{}, runtimeLifecycleError(ErrHelperRuntimeStopping)
	}
	opener, ok := runtime.(rootOpeningGeneration)
	supervisor.mu.Unlock()
	if !ok {
		supervisor.Disconnect(context.Background())
		return RuntimeRootOpenResult{}, runtimeLifecycleError(ErrHelperRootUnsupported)
	}

	response, err := opener.OpenRoot(ctx, request.RequestedRoot)
	if err != nil {
		if !errors.Is(err, ErrHelperRootInvalid) && !errors.Is(err, ErrHelperRootOpen) && !errors.Is(err, ErrHelperRootUnsupported) && !errors.Is(err, ErrHelperRuntimeLimit) {
			supervisor.Disconnect(context.Background())
		}
		return RuntimeRootOpenResult{}, err
	}
	identity := RuntimeRootIdentity{CanonicalPath: response.CanonicalPath, Device: response.Device, Inode: response.Inode}
	capability, err := newRuntimeRootCapability(request.Generation, request.WorkspaceID, response.Handle)
	if err != nil || !validRuntimeRootIdentity(identity) || request.Expected != nil && *request.Expected != identity {
		supervisor.Disconnect(context.Background())
		return RuntimeRootOpenResult{}, runtimeLifecycleError(ErrHelperRootBindingChanged)
	}

	supervisor.mu.Lock()
	if supervisor.state != RuntimeReady || supervisor.runtime != runtime || supervisor.epoch != epoch {
		supervisor.mu.Unlock()
		return RuntimeRootOpenResult{}, runtimeLifecycleError(ErrConnectionGenerationRevoked)
	}
	existingHandle, workspaceBound := supervisor.rootBindings[request.WorkspaceID]
	existingWorkspace, handleBound := supervisor.rootOwners[response.Handle]
	if workspaceBound && existingHandle != response.Handle || handleBound && existingWorkspace != request.WorkspaceID {
		supervisor.mu.Unlock()
		supervisor.Disconnect(context.Background())
		return RuntimeRootOpenResult{}, runtimeLifecycleError(ErrHelperRootBindingChanged)
	}
	supervisor.rootBindings[request.WorkspaceID] = response.Handle
	supervisor.rootOwners[response.Handle] = request.WorkspaceID
	supervisor.mu.Unlock()
	return RuntimeRootOpenResult{Capability: capability, Identity: identity}, nil
}

func (supervisor *RuntimeLeaseSupervisor) ValidateGeneration(generation uint64) error {
	if supervisor == nil || supervisor.connection == nil {
		return runtimeLifecycleError(ErrHelperRuntimeClosed)
	}
	return supervisor.connection.ValidateGeneration(generation)
}

func (supervisor *RuntimeLeaseSupervisor) AcquireTask(ctx context.Context, request RuntimeLeaseRequest) (*RuntimeLease, error) {
	return supervisor.acquire(ctx, RuntimeTaskLease, request)
}

func (supervisor *RuntimeLeaseSupervisor) AcquireRead(ctx context.Context, request RuntimeLeaseRequest) (*RuntimeLease, error) {
	return supervisor.acquire(ctx, RuntimeReadLease, request)
}

func (supervisor *RuntimeLeaseSupervisor) acquire(ctx context.Context, kind RuntimeLeaseKind, request RuntimeLeaseRequest) (*RuntimeLease, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateRuntimeLeaseRequest(request); err != nil {
		return nil, err
	}
	generation := request.Root.generation
	workspaceID := request.Root.workspaceID
	rootHandle := request.Root.rootHandle
	if err := supervisor.connection.ValidateGeneration(generation); err != nil {
		return nil, err
	}
	if err := supervisor.ensureRuntime(ctx, generation); err != nil {
		return nil, err
	}

	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if supervisor.state != RuntimeReady || supervisor.runtime == nil || supervisor.runtime.Generation() != generation {
		return nil, runtimeLifecycleError(ErrHelperRuntimeStopping)
	}
	if err := supervisor.connection.ValidateGeneration(generation); err != nil {
		return nil, err
	}
	ownerKey := string(kind) + "\x00" + workspaceID + "\x00" + request.OwnerID
	if _, exists := supervisor.owners[ownerKey]; exists {
		return nil, runtimeResourceError(ErrHelperRuntimeDuplicate)
	}
	if kind == RuntimeTaskLease && supervisor.taskLeases >= maxRuntimeTaskLeases || kind == RuntimeReadLease && supervisor.readLeases >= maxRuntimeReadLeases {
		return nil, runtimeResourceError(ErrHelperRuntimeLimit)
	}
	existingHandle, workspaceBound := supervisor.rootBindings[workspaceID]
	existingWorkspace, handleBound := supervisor.rootOwners[rootHandle]
	if !workspaceBound || existingHandle != rootHandle || !handleBound || existingWorkspace != workspaceID {
		return nil, runtimeLifecycleError(ErrHelperRootBindingChanged)
	}
	if supervisor.idleTimer != nil {
		supervisor.idleTimer.Stop()
		supervisor.idleTimer = nil
	}
	supervisor.nextLeaseID++
	leaseID := supervisor.nextLeaseID
	leaseParent := supervisor.runtimeContext
	var timeoutCancel context.CancelFunc
	if kind == RuntimeReadLease {
		leaseParent, timeoutCancel = context.WithTimeout(leaseParent, supervisor.readTimeout)
	}
	leaseContext, cancel := context.WithCancel(leaseParent)
	releaseCancel := cancel
	if timeoutCancel != nil {
		releaseCancel = func() {
			cancel()
			timeoutCancel()
		}
	}
	supervisor.leases[leaseID] = runtimeLeaseRecord{
		kind: kind, workspaceID: workspaceID, rootHandle: rootHandle,
		ownerKey: ownerKey, cancel: releaseCancel,
	}
	supervisor.owners[ownerKey] = leaseID
	if kind == RuntimeTaskLease {
		supervisor.taskLeases++
	} else {
		supervisor.readLeases++
	}
	lease := &RuntimeLease{
		kind: kind, generation: generation, workspaceID: workspaceID,
		rootHandle: rootHandle, ownerID: request.OwnerID,
		id: leaseID, ctx: leaseContext, supervisor: supervisor,
	}
	if kind == RuntimeReadLease {
		go func() {
			<-leaseContext.Done()
			lease.Release()
		}()
	}
	return lease, nil
}

func (supervisor *RuntimeLeaseSupervisor) ensureRuntime(ctx context.Context, generation uint64) error {
	for {
		supervisor.mu.Lock()
		switch supervisor.state {
		case RuntimeClosed:
			supervisor.mu.Unlock()
			return runtimeLifecycleError(ErrHelperRuntimeClosed)
		case RuntimeReady:
			runtime := supervisor.runtime
			supervisor.mu.Unlock()
			if runtime == nil || runtime.Generation() != generation {
				return runtimeLifecycleError(ErrHelperRuntimeIdentity)
			}
			return nil
		case RuntimeStopping:
			supervisor.mu.Unlock()
			return runtimeLifecycleError(ErrHelperRuntimeStopping)
		case RuntimeStarting:
			done := supervisor.startDone
			supervisor.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-done:
				continue
			}
		case RuntimeStopped:
			if err := supervisor.connection.ValidateGeneration(generation); err != nil {
				supervisor.mu.Unlock()
				return err
			}
			supervisor.epoch++
			epoch := supervisor.epoch
			startupContext, startupCancel := context.WithCancel(ctx)
			done := make(chan struct{})
			supervisor.state = RuntimeStarting
			supervisor.startDone = done
			supervisor.startCancel = startupCancel
			supervisor.mu.Unlock()

			lifetimeContext, lifetimeRelease, err := supervisor.connection.bindGenerationContext(context.Background(), generation)
			if err == nil {
				var runtime HelperGeneration
				runtime, err = supervisor.factory.Start(startupContext, lifetimeContext, generation)
				if err == nil && (runtime == nil || runtime.Generation() != generation || runtime.Done() == nil) {
					err = ErrHelperRuntimeIdentity
				}
				supervisor.finishRuntimeStart(epoch, generation, runtime, lifetimeContext, lifetimeRelease, err)
			} else {
				supervisor.finishRuntimeStart(epoch, generation, nil, nil, nil, err)
			}
			startupCancel()
			close(done)
			if err != nil {
				return supervisor.runtimeStartError(epoch, err)
			}
		}
	}
}

func (supervisor *RuntimeLeaseSupervisor) finishRuntimeStart(epoch, generation uint64, runtime HelperGeneration, lifetimeContext context.Context, lifetimeRelease func(), startErr error) {
	supervisor.mu.Lock()
	accepted := startErr == nil && supervisor.state == RuntimeStarting && supervisor.epoch == epoch
	if accepted {
		supervisor.runtime = runtime
		supervisor.runtimeContext = lifetimeContext
		supervisor.runtimeRelease = lifetimeRelease
		supervisor.state = RuntimeReady
		supervisor.startCancel = nil
		supervisor.startDone = nil
		supervisor.idleTimer = time.AfterFunc(supervisor.idleTimeout, func() {
			supervisor.expireIdle(epoch)
		})
	} else if supervisor.state == RuntimeStarting && supervisor.epoch == epoch {
		supervisor.state = RuntimeStopped
		supervisor.startCancel = nil
		supervisor.startDone = nil
	}
	supervisor.mu.Unlock()

	if !accepted {
		if runtime != nil {
			_ = runtime.Kill()
		}
		if lifetimeRelease != nil {
			lifetimeRelease()
		}
		return
	}
	go supervisor.watchRuntime(runtime, generation, epoch)
}

func (supervisor *RuntimeLeaseSupervisor) runtimeStartError(epoch uint64, startErr error) error {
	supervisor.mu.Lock()
	revoked := supervisor.epoch != epoch || supervisor.state == RuntimeClosed || supervisor.state == RuntimeStopping
	supervisor.mu.Unlock()
	if revoked {
		return runtimeLifecycleError(ErrConnectionGenerationRevoked)
	}
	supervisor.connection.Disconnect()
	if errors.Is(startErr, context.Canceled) || errors.Is(startErr, ErrConnectionGenerationRevoked) {
		return runtimeLifecycleError(ErrConnectionGenerationRevoked)
	}
	if errors.Is(startErr, ErrHelperRuntimeIdentity) {
		return lifecycleError(FailureConnect, ReasonIdentityChanged, ErrHelperRuntimeIdentity)
	}
	return lifecycleError(FailureConnect, ReasonUnknown, ErrHelperRuntimeStart)
}

func (supervisor *RuntimeLeaseSupervisor) watchRuntime(runtime HelperGeneration, generation, epoch uint64) {
	<-runtime.Done()
	supervisor.mu.Lock()
	if supervisor.runtime != runtime || supervisor.epoch != epoch || supervisor.state != RuntimeReady {
		supervisor.mu.Unlock()
		return
	}
	supervisor.epoch++
	_, cancels, release := supervisor.detachRuntimeLocked(RuntimeStopping)
	supervisor.mu.Unlock()
	cancelAll(cancels)
	if release != nil {
		release()
	}
	supervisor.connection.Disconnect()
	supervisor.mu.Lock()
	if supervisor.state != RuntimeClosed {
		supervisor.state = RuntimeStopped
	}
	supervisor.mu.Unlock()
}

func (supervisor *RuntimeLeaseSupervisor) releaseLease(leaseID uint64) {
	supervisor.mu.Lock()
	record, ok := supervisor.leases[leaseID]
	if !ok {
		supervisor.mu.Unlock()
		return
	}
	delete(supervisor.leases, leaseID)
	delete(supervisor.owners, record.ownerKey)
	if record.kind == RuntimeTaskLease {
		supervisor.taskLeases--
	} else {
		supervisor.readLeases--
	}
	record.cancel()
	if len(supervisor.leases) == 0 && supervisor.state == RuntimeReady && supervisor.idleTimer == nil {
		epoch := supervisor.epoch
		supervisor.idleTimer = time.AfterFunc(supervisor.idleTimeout, func() {
			supervisor.expireIdle(epoch)
		})
	}
	supervisor.mu.Unlock()
}

func (supervisor *RuntimeLeaseSupervisor) expireIdle(epoch uint64) {
	supervisor.mu.Lock()
	if supervisor.state != RuntimeReady || supervisor.epoch != epoch || len(supervisor.leases) != 0 {
		supervisor.mu.Unlock()
		return
	}
	supervisor.idleTimer = nil
	supervisor.mu.Unlock()
	_ = supervisor.Disconnect(context.Background())
}

// Disconnect revokes all lease contexts before asking the helper to stop. It
// never reconnects. The connection generation is revoked after the bounded
// graceful attempt, and a late helper completion cannot restore readiness.
func (supervisor *RuntimeLeaseSupervisor) Disconnect(ctx context.Context) error {
	return supervisor.revoke(ctx, false)
}

// Close permanently closes both runtime and connection supervisors. It is
// idempotent and applies the same revoke-before-termination ordering.
func (supervisor *RuntimeLeaseSupervisor) Close(ctx context.Context) error {
	return supervisor.revoke(ctx, true)
}

func (supervisor *RuntimeLeaseSupervisor) revoke(ctx context.Context, closeSupervisor bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	connectionGeneration := supervisor.connection.Snapshot().Generation
	supervisor.mu.Lock()
	if supervisor.state == RuntimeClosed {
		supervisor.mu.Unlock()
		return nil
	}
	supervisor.epoch++
	state := RuntimeStopping
	if closeSupervisor {
		state = RuntimeClosed
	}
	startDone := supervisor.startDone
	startCancel := supervisor.startCancel
	runtime, cancels, release := supervisor.detachRuntimeLocked(state)
	supervisor.mu.Unlock()

	cancelAll(cancels)
	if startCancel != nil {
		startCancel()
	}

	shutdownErr := supervisor.stopRuntime(ctx, runtime)
	if release != nil {
		release()
	}
	if closeSupervisor {
		supervisor.connection.Close()
	} else {
		supervisor.connection.Disconnect()
	}
	if waitErr := supervisor.connection.WaitForGenerationIdle(ctx, connectionGeneration); waitErr != nil {
		shutdownErr = errors.Join(shutdownErr, waitErr)
	}
	if startDone != nil {
		cleanupContext, cancel := boundedContext(ctx, supervisor.cleanupTimeout)
		select {
		case <-startDone:
		case <-cleanupContext.Done():
		}
		cancel()
	}
	supervisor.mu.Lock()
	if supervisor.state != RuntimeClosed {
		supervisor.state = RuntimeStopped
	}
	supervisor.mu.Unlock()
	return shutdownErr
}

func (supervisor *RuntimeLeaseSupervisor) detachRuntimeLocked(state RuntimeState) (HelperGeneration, []context.CancelFunc, func()) {
	if supervisor.idleTimer != nil {
		supervisor.idleTimer.Stop()
		supervisor.idleTimer = nil
	}
	cancels := make([]context.CancelFunc, 0, len(supervisor.leases))
	for _, lease := range supervisor.leases {
		cancels = append(cancels, lease.cancel)
	}
	supervisor.leases = make(map[uint64]runtimeLeaseRecord)
	supervisor.owners = make(map[string]uint64)
	supervisor.terminalLeases = make(map[uint64]struct{})
	supervisor.rootBindings = make(map[string]string)
	supervisor.rootOwners = make(map[string]string)
	supervisor.taskLeases = 0
	supervisor.readLeases = 0
	runtime := supervisor.runtime
	supervisor.runtime = nil
	release := supervisor.runtimeRelease
	supervisor.runtimeRelease = nil
	supervisor.runtimeContext = nil
	supervisor.state = state
	return runtime, cancels, release
}

func (supervisor *RuntimeLeaseSupervisor) stopRuntime(parent context.Context, runtime HelperGeneration) error {
	if runtime == nil {
		return nil
	}
	shutdownContext, cancel := boundedContext(parent, supervisor.gracefulTimeout)
	err := runtime.Shutdown(shutdownContext)
	cancel()
	if err == nil {
		return nil
	}
	_ = runtime.Kill()
	cleanupContext, cleanupCancel := boundedContext(parent, supervisor.cleanupTimeout)
	defer cleanupCancel()
	select {
	case <-runtime.Done():
	case <-cleanupContext.Done():
	}
	return runtimeLifecycleError(ErrHelperRuntimeShutdown)
}

func (supervisor *RuntimeLeaseSupervisor) Snapshot() RuntimeSnapshot {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	snapshot := RuntimeSnapshot{
		State: supervisor.state, TaskLeases: supervisor.taskLeases,
		ReadLeases: supervisor.readLeases, IdlePending: supervisor.idleTimer != nil,
	}
	if supervisor.runtime != nil && supervisor.state == RuntimeReady {
		snapshot.Generation = supervisor.runtime.Generation()
	}
	return snapshot
}

func boundedContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, timeout)
}

func cancelAll(cancels []context.CancelFunc) {
	for _, cancel := range cancels {
		cancel()
	}
}

func validateRuntimeRootOpenRequest(request RuntimeRootOpenRequest) error {
	if request.Generation == 0 || !validRuntimeIdentity("workspace-", request.WorkspaceID) || !validRemoteRootPath(request.RequestedRoot) || request.Expected != nil && !validRuntimeRootIdentity(*request.Expected) {
		return runtimeLifecycleError(ErrHelperRootInvalid)
	}
	return nil
}

func validRuntimeRootIdentity(identity RuntimeRootIdentity) bool {
	return validRemoteRootPath(identity.CanonicalPath) && identity.Device != 0 && identity.Inode != 0
}

func validRemoteRootPath(value string) bool {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) || !path.IsAbs(value) || path.Clean(value) != value || strings.Contains(value, "\\") {
		return false
	}
	for _, char := range value {
		if char == 0 || unicode.IsControl(char) || unicode.Is(unicode.Cf, char) {
			return false
		}
	}
	return true
}

func validateRuntimeLeaseRequest(request RuntimeLeaseRequest) error {
	if !validRuntimeRootCapability(request.Root) || !validLeaseOwner(request.OwnerID) {
		return runtimeLifecycleError(ErrHelperRuntimeInvalidLease)
	}
	return nil
}

func validRuntimeRootCapability(capability *RuntimeRootCapability) bool {
	return capability != nil && capability.generation != 0 && validRuntimeIdentity("workspace-", capability.workspaceID) && validRuntimeIdentity("root-", capability.rootHandle)
}

func validRuntimeIdentity(prefix, value string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+32 {
		return false
	}
	for _, char := range value[len(prefix):] {
		if char < '0' || (char > '9' && char < 'a') || char > 'f' {
			return false
		}
	}
	return true
}

func validLeaseOwner(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("-_.:", char) {
			continue
		}
		return false
	}
	return true
}

func (supervisor *RuntimeLeaseSupervisor) revokeOutcomeUnknown(err error) {
	if errors.Is(err, ErrRuntimeOutcomeUnknown) {
		_ = supervisor.Disconnect(context.Background())
	}
}

func runtimeLifecycleError(cause error) error {
	return lifecycleError(FailureDisconnected, ReasonDisconnected, cause)
}

func runtimeResourceError(cause error) error {
	return &ConnectionSupervisorError{
		Failure: ConnectionFailure{Code: FailureResourceLimit, Reason: ReasonResourceLimit},
		cause:   cause,
	}
}
