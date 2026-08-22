package remotessh

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	runtimeWorkspaceA = "workspace-0123456789abcdef0123456789abcdef"
	runtimeWorkspaceB = "workspace-fedcba9876543210fedcba9876543210"
	runtimeRootA      = "root-11111111111111111111111111111111"
	runtimeRootB      = "root-22222222222222222222222222222222"
)

type fakeHelperGeneration struct {
	generation    uint64
	done          chan struct{}
	finishOnce    sync.Once
	mu            sync.Mutex
	shutdownCalls int
	killCalls     int
	shutdownCheck func() error
	shutdownErr   error
	rootResponse  rootOpenResponse
	rootErr       error
	openedRoots   []string
	fileInfo      RuntimeFileInfo
	fileList      RuntimeFileList
	fileRead      RuntimeFileRead
	fileImage     RuntimeFileImage
	fileHash      RuntimeFileHash
	fileWrite     RuntimeFileWriteResult
	fileMkdir     RuntimeFileMkdirResult
	fileContent   RuntimeFileContent
	lastWrite     RuntimeFileWriteRequest
	searchFind    RuntimeSearchFindResult
	searchGrep    RuntimeSearchGrepResult
	gitResult     RuntimeGitReadResult
	bashResult    RuntimeBashResult
	fileErr       error
}

func newFakeHelperGeneration(generation uint64) *fakeHelperGeneration {
	return &fakeHelperGeneration{
		generation: generation, done: make(chan struct{}),
		rootResponse: rootOpenResponse{
			Handle: runtimeRootA, CanonicalPath: "/srv/repository", Device: 7, Inode: 11,
		},
		fileInfo:    RuntimeFileInfo{Path: "file.txt", Kind: "file", Size: 5, Mode: 0o644, ModTime: 1},
		fileList:    RuntimeFileList{Path: ".", Entries: []RuntimeFileInfo{{Path: "file.txt", Kind: "file", Size: 5, Mode: 0o644, ModTime: 1}}},
		fileRead:    RuntimeFileRead{Path: "file.txt", Content: "hello", StartLine: 1, EndLine: 1},
		fileImage:   testRuntimeImage("image.png"),
		fileHash:    RuntimeFileHash{Path: "file.txt", Size: 5, SHA256: strings.Repeat("a", 64)},
		fileWrite:   testRuntimeWriteResult("new.txt", []byte("new"), true),
		fileMkdir:   RuntimeFileMkdirResult{Path: "nested/deep", Created: []string{"nested", "nested/deep"}},
		fileContent: testRuntimeContent("file.txt", []byte("hello old")),
		searchFind:  RuntimeSearchFindResult{Paths: []string{"file.txt"}},
		searchGrep:  RuntimeSearchGrepResult{Matches: []RuntimeSearchGrepMatch{{Path: "file.txt", Line: 1, Text: "hello"}}},
		gitResult:   testRuntimeGitResult("status", map[string][]byte{"status": []byte("## main\x00")}),
		bashResult:  RuntimeBashResult{ProcessID: "process-0123456789abcdef0123456789abcdef", ExitCode: 0, Output: "ok\n", OutputBytes: 3},
	}
}

func (runtime *fakeHelperGeneration) Generation() uint64    { return runtime.generation }
func (runtime *fakeHelperGeneration) Done() <-chan struct{} { return runtime.done }

func (runtime *fakeHelperGeneration) Shutdown(context.Context) error {
	runtime.mu.Lock()
	runtime.shutdownCalls++
	check := runtime.shutdownCheck
	err := runtime.shutdownErr
	runtime.mu.Unlock()
	if check != nil {
		if checkErr := check(); checkErr != nil {
			return checkErr
		}
	}
	if err == nil {
		runtime.finish()
	}
	return err
}

func (runtime *fakeHelperGeneration) OpenRoot(_ context.Context, path string) (rootOpenResponse, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.openedRoots = append(runtime.openedRoots, path)
	return runtime.rootResponse, runtime.rootErr
}

func (runtime *fakeHelperGeneration) StatFile(context.Context, string, string) (RuntimeFileInfo, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.fileInfo, runtime.fileErr
}

func (runtime *fakeHelperGeneration) ListFiles(context.Context, string, string) (RuntimeFileList, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.fileList, runtime.fileErr
}

func (runtime *fakeHelperGeneration) ReadFile(context.Context, string, string, int, int) (RuntimeFileRead, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.fileRead, runtime.fileErr
}

func (runtime *fakeHelperGeneration) ReadImage(context.Context, string, string) (RuntimeFileImage, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.fileImage, runtime.fileErr
}

func (runtime *fakeHelperGeneration) HashFile(context.Context, string, string) (RuntimeFileHash, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.fileHash, runtime.fileErr
}

func (runtime *fakeHelperGeneration) WriteFile(_ context.Context, _ string, request RuntimeFileWriteRequest) (RuntimeFileWriteResult, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.lastWrite = request
	return runtime.fileWrite, runtime.fileErr
}

func (runtime *fakeHelperGeneration) Content(_ context.Context, _ string, _ string) (RuntimeFileContent, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.fileContent, runtime.fileErr
}

func (runtime *fakeHelperGeneration) Mkdir(_ context.Context, _ string, _ string) (RuntimeFileMkdirResult, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.fileMkdir, runtime.fileErr
}

func (runtime *fakeHelperGeneration) FindFiles(_ context.Context, _ string, _ RuntimeSearchFindRequest) (RuntimeSearchFindResult, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.searchFind, runtime.fileErr
}

func (runtime *fakeHelperGeneration) GrepFiles(_ context.Context, _ string, _ RuntimeSearchGrepRequest) (RuntimeSearchGrepResult, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.searchGrep, runtime.fileErr
}

func (runtime *fakeHelperGeneration) RunBash(_ context.Context, _ string, _ string) (RuntimeBashResult, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.bashResult, runtime.fileErr
}

func (runtime *fakeHelperGeneration) StartTerminal(_ context.Context, _ context.Context, _ string, _, _ int) (*RuntimeTerminalSession, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	done := make(chan struct{})
	close(done)
	return &RuntimeTerminalSession{processID: "process-0123456789abcdef0123456789abcdef", events: make(chan RuntimeTerminalEvent), done: done}, runtime.fileErr
}

func (runtime *fakeHelperGeneration) ReadGit(_ context.Context, _ string, _ RuntimeGitReadRequest) (RuntimeGitReadResult, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.gitResult, runtime.fileErr
}

func (runtime *fakeHelperGeneration) Kill() error {
	runtime.mu.Lock()
	runtime.killCalls++
	runtime.mu.Unlock()
	runtime.finish()
	return nil
}

func (runtime *fakeHelperGeneration) finish() {
	runtime.finishOnce.Do(func() { close(runtime.done) })
}

func (runtime *fakeHelperGeneration) calls() (int, int) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.shutdownCalls, runtime.killCalls
}

type fakeHelperGenerationFactory struct {
	mu              sync.Mutex
	calls           int
	started         chan struct{}
	startGate       <-chan struct{}
	startErr        error
	wrongGeneration bool
	runtimes        []*fakeHelperGeneration
}

func (factory *fakeHelperGenerationFactory) Start(startupContext, lifetimeContext context.Context, generation uint64) (HelperGeneration, error) {
	factory.mu.Lock()
	factory.calls++
	started := factory.started
	gate := factory.startGate
	err := factory.startErr
	wrongGeneration := factory.wrongGeneration
	factory.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if gate != nil {
		select {
		case <-gate:
		case <-startupContext.Done():
			return nil, startupContext.Err()
		case <-lifetimeContext.Done():
			return nil, lifetimeContext.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	if wrongGeneration {
		generation++
	}
	runtime := newFakeHelperGeneration(generation)
	factory.mu.Lock()
	factory.runtimes = append(factory.runtimes, runtime)
	factory.mu.Unlock()
	go func() {
		<-lifetimeContext.Done()
		runtime.finish()
	}()
	return runtime, nil
}

func (factory *fakeHelperGenerationFactory) snapshot() (int, []*fakeHelperGeneration) {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	return factory.calls, append([]*fakeHelperGeneration(nil), factory.runtimes...)
}

func readyRuntimeConnection(t *testing.T) (*ConnectionSupervisor, uint64) {
	t.Helper()
	connection := newTestSupervisor(t, connectionProberFunc(func(context.Context, string) (ConnectionPreflight, error) {
		return supervisorPreflight("config-a", "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), nil
	}))
	ready, err := connection.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return connection, ready.Generation
}

func openRuntimeRoot(t *testing.T, supervisor *RuntimeLeaseSupervisor, generation uint64) *RuntimeRootCapability {
	t.Helper()
	opened, err := supervisor.OpenRoot(context.Background(), RuntimeRootOpenRequest{
		Generation: generation, WorkspaceID: runtimeWorkspaceA, RequestedRoot: "/srv/repository",
	})
	if err != nil {
		t.Fatal(err)
	}
	return opened.Capability
}

func runtimeRequest(root *RuntimeRootCapability, owner string) RuntimeLeaseRequest {
	return RuntimeLeaseRequest{Root: root, OwnerID: owner}
}

func newTestRuntimeSupervisor(t *testing.T, factory HelperGenerationFactory, idle time.Duration) (*RuntimeLeaseSupervisor, *ConnectionSupervisor, uint64) {
	t.Helper()
	connection, generation := readyRuntimeConnection(t)
	supervisor, err := newRuntimeLeaseSupervisor(connection, factory, idle, 20*time.Millisecond, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	return supervisor, connection, generation
}

func TestRuntimeLeaseSupervisorMintsCapabilityOnlyAfterHelperRootOpen(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, _, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	synthetic, err := newRuntimeRootCapability(generation, runtimeWorkspaceA, runtimeRootA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.AcquireTask(context.Background(), RuntimeLeaseRequest{Root: synthetic, OwnerID: "forged"}); !errors.Is(err, ErrHelperRootBindingChanged) {
		t.Fatalf("unopened root capability error = %v", err)
	}
	opened, err := supervisor.OpenRoot(context.Background(), RuntimeRootOpenRequest{
		Generation: generation, WorkspaceID: runtimeWorkspaceA, RequestedRoot: "/srv/repository",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opened.Capability == nil || opened.Capability.Generation() != generation || opened.Capability.WorkspaceID() != runtimeWorkspaceA || opened.Capability.rootHandle != runtimeRootA || opened.Identity != (RuntimeRootIdentity{CanonicalPath: "/srv/repository", Device: 7, Inode: 11}) {
		t.Fatalf("opened root = %#v", opened)
	}
	lease, err := supervisor.AcquireTask(context.Background(), RuntimeLeaseRequest{Root: opened.Capability, OwnerID: "task-root"})
	if err != nil {
		t.Fatal(err)
	}
	_, runtimes := factory.snapshot()
	runtimes[0].mu.Lock()
	paths := append([]string(nil), runtimes[0].openedRoots...)
	runtimes[0].mu.Unlock()
	if len(paths) != 1 || paths[0] != "/srv/repository" || lease.RootHandle() != runtimeRootA {
		t.Fatalf("helper root paths=%#v lease=%#v", paths, lease)
	}
	lease.Release()
	_ = supervisor.Close(context.Background())
}

func TestRuntimeLeaseSupervisorRootIdentityDriftRevokesGeneration(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	expected := &RuntimeRootIdentity{CanonicalPath: "/srv/repository", Device: 7, Inode: 99}
	_, err := supervisor.OpenRoot(context.Background(), RuntimeRootOpenRequest{
		Generation: generation, WorkspaceID: runtimeWorkspaceA,
		RequestedRoot: "/srv/repository", Expected: expected,
	})
	if !errors.Is(err, ErrHelperRootBindingChanged) {
		t.Fatalf("root drift error = %v", err)
	}
	if connection.Snapshot().State != ConnectionDisconnected || supervisor.Snapshot().State != RuntimeStopped {
		t.Fatalf("root drift states: connection=%#v runtime=%#v", connection.Snapshot(), supervisor.Snapshot())
	}
}

func TestRuntimeLeaseSupervisorRejectsMalformedHelperRootAndKeepsSemanticFailureRetryable(t *testing.T) {
	t.Run("malformed response revokes", func(t *testing.T) {
		factory := &fakeHelperGenerationFactory{}
		supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
		_, err := supervisor.OpenRoot(context.Background(), RuntimeRootOpenRequest{
			Generation: generation, WorkspaceID: runtimeWorkspaceA, RequestedRoot: "/srv/repository",
		})
		if err != nil {
			t.Fatal(err)
		}
		// A second workspace cannot claim the helper's reused handle.
		_, err = supervisor.OpenRoot(context.Background(), RuntimeRootOpenRequest{
			Generation: generation, WorkspaceID: runtimeWorkspaceB, RequestedRoot: "/srv/repository",
		})
		if !errors.Is(err, ErrHelperRootBindingChanged) || connection.Snapshot().State != ConnectionDisconnected {
			t.Fatalf("shared helper handle error=%v connection=%#v", err, connection.Snapshot())
		}
	})

	t.Run("semantic open failure remains ready", func(t *testing.T) {
		factory := &fakeHelperGenerationFactory{}
		supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
		if err := supervisor.ensureRuntime(context.Background(), generation); err != nil {
			t.Fatal(err)
		}
		_, runtimes := factory.snapshot()
		runtimes[0].mu.Lock()
		runtimes[0].rootErr = runtimeLifecycleError(ErrHelperRootOpen)
		runtimes[0].mu.Unlock()
		_, err := supervisor.OpenRoot(context.Background(), RuntimeRootOpenRequest{
			Generation: generation, WorkspaceID: runtimeWorkspaceA, RequestedRoot: "/missing",
		})
		if !errors.Is(err, ErrHelperRootOpen) || connection.Snapshot().State != ConnectionReady || supervisor.Snapshot().State != RuntimeReady {
			t.Fatalf("semantic root failure=%v connection=%#v runtime=%#v", err, connection.Snapshot(), supervisor.Snapshot())
		}
		_ = supervisor.Close(context.Background())
	})
}

func TestRuntimeLeaseSupervisorSharesGenerationAndExpiresAfterLastLease(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, 30*time.Millisecond)
	root := openRuntimeRoot(t, supervisor, generation)
	task, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "task-1"))
	if err != nil {
		t.Fatal(err)
	}
	read, err := supervisor.AcquireRead(context.Background(), runtimeRequest(root, "read-1"))
	if err != nil {
		t.Fatal(err)
	}
	if task.Kind() != RuntimeTaskLease || task.Generation() != generation || task.WorkspaceID() != runtimeWorkspaceA || task.RootHandle() != runtimeRootA || task.OwnerID() != "task-1" {
		t.Fatalf("unexpected task lease metadata")
	}
	if snapshot := supervisor.Snapshot(); snapshot.State != RuntimeReady || snapshot.Generation != generation || snapshot.TaskLeases != 1 || snapshot.ReadLeases != 1 || snapshot.IdlePending {
		t.Fatalf("ready runtime snapshot = %#v", snapshot)
	}
	calls, runtimes := factory.snapshot()
	if calls != 1 || len(runtimes) != 1 {
		t.Fatalf("helper starts = %d, runtimes = %d", calls, len(runtimes))
	}

	task.Release()
	task.Release()
	if snapshot := supervisor.Snapshot(); snapshot.TaskLeases != 0 || snapshot.ReadLeases != 1 || snapshot.IdlePending {
		t.Fatalf("task release snapshot = %#v", snapshot)
	}
	read.Release()
	if snapshot := supervisor.Snapshot(); snapshot.ReadLeases != 0 || !snapshot.IdlePending {
		t.Fatalf("last release did not schedule idle shutdown: %#v", snapshot)
	}
	waitForRuntimeState(t, supervisor, RuntimeStopped)
	if snapshot := connection.Snapshot(); snapshot.State != ConnectionDisconnected || snapshot.Generation != 0 {
		t.Fatalf("idle expiry retained connection generation: %#v", snapshot)
	}
	shutdownCalls, killCalls := runtimes[0].calls()
	if shutdownCalls != 1 || killCalls != 0 {
		t.Fatalf("idle helper stop calls = shutdown %d kill %d", shutdownCalls, killCalls)
	}
}

func TestRuntimeLeaseSupervisorRevokesCapabilitiesBeforeShutdown(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	lease, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "task-1"))
	if err != nil {
		t.Fatal(err)
	}
	_, runtimes := factory.snapshot()
	runtimes[0].shutdownCheck = func() error {
		select {
		case <-lease.Context().Done():
			return nil
		default:
			return errors.New("lease was live during helper shutdown")
		}
	}
	if err := supervisor.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-lease.Context().Done():
	default:
		t.Fatal("disconnect did not revoke task capability")
	}
	if connection.Snapshot().State != ConnectionDisconnected || supervisor.Snapshot().State != RuntimeStopped {
		t.Fatalf("disconnect state: connection=%#v runtime=%#v", connection.Snapshot(), supervisor.Snapshot())
	}
	if _, err := supervisor.AcquireRead(context.Background(), runtimeRequest(root, "read-after-disconnect")); !errors.Is(err, ErrConnectionGenerationRevoked) {
		t.Fatalf("old generation acquisition error = %v", err)
	}
}

func TestRuntimeLeaseSupervisorUnexpectedExitRevokesAllLeases(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	task, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "task-1"))
	if err != nil {
		t.Fatal(err)
	}
	read, err := supervisor.AcquireRead(context.Background(), runtimeRequest(root, "read-1"))
	if err != nil {
		t.Fatal(err)
	}
	_, runtimes := factory.snapshot()
	runtimes[0].finish()
	waitForRuntimeState(t, supervisor, RuntimeStopped)
	for name, lease := range map[string]*RuntimeLease{"task": task, "read": read} {
		select {
		case <-lease.Context().Done():
		default:
			t.Fatalf("%s lease survived helper exit", name)
		}
	}
	if connection.Snapshot().State != ConnectionDisconnected {
		t.Fatalf("helper exit left connection ready: %#v", connection.Snapshot())
	}
}

func TestRuntimeLeaseSupervisorSingleFlightStart(t *testing.T) {
	gate := make(chan struct{})
	factory := &fakeHelperGenerationFactory{started: make(chan struct{}, 1), startGate: gate}
	supervisor, _, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	type result struct {
		root RuntimeRootOpenResult
		err  error
	}
	results := make(chan result, 2)
	open := func() {
		root, err := supervisor.OpenRoot(context.Background(), RuntimeRootOpenRequest{
			Generation: generation, WorkspaceID: runtimeWorkspaceA, RequestedRoot: "/srv/repository",
		})
		results <- result{root: root, err: err}
	}
	go open()
	select {
	case <-factory.started:
	case <-time.After(time.Second):
		t.Fatal("helper start did not begin")
	}
	go open()
	close(gate)
	var firstHandle string
	for range 2 {
		select {
		case value := <-results:
			if value.err != nil {
				t.Fatal(value.err)
			}
			if firstHandle == "" {
				firstHandle = value.root.Capability.rootHandle
			} else if value.root.Capability.rootHandle != firstHandle {
				t.Fatal("single-flight root handle changed")
			}
		case <-time.After(time.Second):
			t.Fatal("root acquisition did not finish")
		}
	}
	calls, _ := factory.snapshot()
	if calls != 1 {
		t.Fatalf("single-flight helper starts = %d", calls)
	}
	_ = supervisor.Close(context.Background())
}

func TestRuntimeLeaseSupervisorDisconnectCancelsInFlightStart(t *testing.T) {
	gate := make(chan struct{})
	factory := &fakeHelperGenerationFactory{started: make(chan struct{}, 1), startGate: gate}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	result := make(chan error, 1)
	go func() {
		_, err := supervisor.OpenRoot(context.Background(), RuntimeRootOpenRequest{
			Generation: generation, WorkspaceID: runtimeWorkspaceA, RequestedRoot: "/srv/repository",
		})
		result <- err
	}()
	select {
	case <-factory.started:
	case <-time.After(time.Second):
		t.Fatal("helper start did not begin")
	}
	if err := supervisor.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrConnectionGenerationRevoked) {
			t.Fatalf("cancelled start error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled start did not return")
	}
	if supervisor.Snapshot().State != RuntimeStopped || connection.Snapshot().State != ConnectionDisconnected {
		t.Fatalf("cancelled start states: runtime=%#v connection=%#v", supervisor.Snapshot(), connection.Snapshot())
	}
}

func TestRuntimeLeaseSupervisorNewLeaseCancelsStaleIdleTimer(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, _, generation := newTestRuntimeSupervisor(t, factory, 80*time.Millisecond)
	root := openRuntimeRoot(t, supervisor, generation)
	first, err := supervisor.AcquireRead(context.Background(), runtimeRequest(root, "read-1"))
	if err != nil {
		t.Fatal(err)
	}
	first.Release()
	time.Sleep(10 * time.Millisecond)
	second, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "task-1"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if snapshot := supervisor.Snapshot(); snapshot.State != RuntimeReady || snapshot.TaskLeases != 1 || snapshot.IdlePending {
		t.Fatalf("stale idle timer revoked live lease: %#v", snapshot)
	}
	calls, _ := factory.snapshot()
	if calls != 1 {
		t.Fatalf("new lease restarted helper: %d", calls)
	}
	second.Release()
	_ = supervisor.Close(context.Background())
}

func TestRuntimeLeaseSupervisorReadLeaseHasHardLifetime(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, _, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	supervisor.readTimeout = 20 * time.Millisecond
	lease, err := supervisor.AcquireRead(context.Background(), runtimeRequest(root, "read-timeout"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-lease.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("read lease exceeded its hard lifetime")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && supervisor.Snapshot().ReadLeases != 0 {
		time.Sleep(time.Millisecond)
	}
	if snapshot := supervisor.Snapshot(); snapshot.ReadLeases != 0 || !snapshot.IdlePending {
		t.Fatalf("expired read lease remained active: %#v", snapshot)
	}
	_ = supervisor.Close(context.Background())
}

func TestRuntimeLeaseSupervisorShutdownFailureKillsProcess(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	lease, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "task-1"))
	if err != nil {
		t.Fatal(err)
	}
	_, runtimes := factory.snapshot()
	runtimes[0].shutdownErr = errors.New("untrusted shutdown detail")
	err = supervisor.Disconnect(context.Background())
	if !errors.Is(err, ErrHelperRuntimeShutdown) || containsAny(err.Error(), "untrusted", "detail") {
		t.Fatalf("shutdown failure = %v", err)
	}
	shutdownCalls, killCalls := runtimes[0].calls()
	if shutdownCalls != 1 || killCalls != 1 {
		t.Fatalf("shutdown failure calls = shutdown %d kill %d", shutdownCalls, killCalls)
	}
	select {
	case <-lease.Context().Done():
	default:
		t.Fatal("shutdown failure retained lease")
	}
	if connection.Snapshot().State != ConnectionDisconnected {
		t.Fatalf("shutdown failure retained connection: %#v", connection.Snapshot())
	}
}

func TestRuntimeLeaseSupervisorRejectsDuplicateLimitsAndRootRebind(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, _, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	first, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "task-0"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "task-0")); !errors.Is(err, ErrHelperRuntimeDuplicate) {
		t.Fatalf("duplicate owner error = %v", err)
	}
	rebound := runtimeRequest(root, "read-rebound")
	rebound.Root = &RuntimeRootCapability{generation: generation, workspaceID: runtimeWorkspaceA, rootHandle: runtimeRootB}
	if _, err := supervisor.AcquireRead(context.Background(), rebound); !errors.Is(err, ErrHelperRootBindingChanged) {
		t.Fatalf("workspace root rebind error = %v", err)
	}
	otherWorkspace := runtimeRequest(root, "read-other")
	otherWorkspace.Root = &RuntimeRootCapability{generation: generation, workspaceID: runtimeWorkspaceB, rootHandle: runtimeRootA}
	if _, err := supervisor.AcquireRead(context.Background(), otherWorkspace); !errors.Is(err, ErrHelperRootBindingChanged) {
		t.Fatalf("shared root handle error = %v", err)
	}
	leases := []*RuntimeLease{first}
	for index := 1; index < maxRuntimeTaskLeases; index++ {
		lease, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, fmt.Sprintf("task-%d", index)))
		if err != nil {
			t.Fatal(err)
		}
		leases = append(leases, lease)
	}
	if _, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "task-over-limit")); !errors.Is(err, ErrHelperRuntimeLimit) {
		t.Fatalf("task limit error = %v", err)
	}
	var projected *ConnectionSupervisorError
	if _, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "task-over-limit-2")); !errors.As(err, &projected) || projected.Failure.Code != FailureResourceLimit || projected.Failure.Reason != ReasonResourceLimit {
		t.Fatalf("resource projection = %#v, %v", projected, err)
	}
	for _, lease := range leases {
		lease.Release()
	}
	_ = supervisor.Close(context.Background())
}

func TestRuntimeLeaseSupervisorStartFailureAndIdentityMismatchRevokeGeneration(t *testing.T) {
	for name, testCase := range map[string]struct {
		factory  *fakeHelperGenerationFactory
		expected error
	}{
		"start":    {factory: &fakeHelperGenerationFactory{startErr: errors.New("secret host failure")}, expected: ErrHelperRuntimeStart},
		"identity": {factory: &fakeHelperGenerationFactory{wrongGeneration: true}, expected: ErrHelperRuntimeIdentity},
	} {
		t.Run(name, func(t *testing.T) {
			supervisor, connection, generation := newTestRuntimeSupervisor(t, testCase.factory, time.Minute)
			_, err := supervisor.OpenRoot(context.Background(), RuntimeRootOpenRequest{
				Generation: generation, WorkspaceID: runtimeWorkspaceA, RequestedRoot: "/srv/repository",
			})
			if !errors.Is(err, testCase.expected) || err == nil || (name == "start" && containsAny(err.Error(), "secret", "host failure")) {
				t.Fatalf("start failure = %v", err)
			}
			if connection.Snapshot().State != ConnectionDisconnected {
				t.Fatalf("failed start retained generation: %#v", connection.Snapshot())
			}
		})
	}
}

func TestRuntimeLeaseSupervisorCloseIsTerminal(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	supervisor, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, supervisor, generation)
	lease, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "task-1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if supervisor.Snapshot().State != RuntimeClosed || connection.Snapshot().State != ConnectionClosed {
		t.Fatalf("close state: runtime=%#v connection=%#v", supervisor.Snapshot(), connection.Snapshot())
	}
	select {
	case <-lease.Context().Done():
	default:
		t.Fatal("close did not revoke lease")
	}
	if _, err := supervisor.AcquireTask(context.Background(), runtimeRequest(root, "task-2")); !errors.Is(err, ErrConnectionGenerationRevoked) {
		t.Fatalf("closed acquisition error = %v", err)
	}
	if err := supervisor.Close(context.Background()); err != nil {
		t.Fatalf("idempotent close = %v", err)
	}
}

func TestValidateRuntimeRootOpenRequest(t *testing.T) {
	valid := RuntimeRootOpenRequest{
		Generation: 1, WorkspaceID: runtimeWorkspaceA, RequestedRoot: "/srv/repository",
		Expected: &RuntimeRootIdentity{CanonicalPath: "/srv/repository", Device: 7, Inode: 11},
	}
	if err := validateRuntimeRootOpenRequest(valid); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*RuntimeRootOpenRequest){
		"generation": func(request *RuntimeRootOpenRequest) { request.Generation = 0 },
		"workspace":  func(request *RuntimeRootOpenRequest) { request.WorkspaceID = "workspace-invalid" },
		"relative":   func(request *RuntimeRootOpenRequest) { request.RequestedRoot = "relative" },
		"windows":    func(request *RuntimeRootOpenRequest) { request.RequestedRoot = `C:\\local-anchor` },
		"identity":   func(request *RuntimeRootOpenRequest) { request.Expected.Inode = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			request := valid
			expected := *valid.Expected
			request.Expected = &expected
			mutate(&request)
			if err := validateRuntimeRootOpenRequest(request); !errors.Is(err, ErrHelperRootInvalid) {
				t.Fatalf("invalid root request error = %v", err)
			}
		})
	}
}

func TestValidateRuntimeLeaseRequest(t *testing.T) {
	root, err := newRuntimeRootCapability(1, runtimeWorkspaceA, runtimeRootA)
	if err != nil {
		t.Fatal(err)
	}
	valid := runtimeRequest(root, "task:1")
	if err := validateRuntimeLeaseRequest(valid); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*RuntimeLeaseRequest){
		"missing":    func(request *RuntimeLeaseRequest) { request.Root = nil },
		"generation": func(request *RuntimeLeaseRequest) { request.Root.generation = 0 },
		"workspace":  func(request *RuntimeLeaseRequest) { request.Root.workspaceID = "workspace-ABC" },
		"root":       func(request *RuntimeLeaseRequest) { request.Root.rootHandle = "../root" },
		"owner":      func(request *RuntimeLeaseRequest) { request.OwnerID = "owner/escape" },
	} {
		t.Run(name, func(t *testing.T) {
			request := valid
			rootCopy := *valid.Root
			request.Root = &rootCopy
			mutate(&request)
			if err := validateRuntimeLeaseRequest(request); !errors.Is(err, ErrHelperRuntimeInvalidLease) {
				t.Fatalf("invalid lease error = %v", err)
			}
		})
	}
	select {
	case <-((*RuntimeLease)(nil)).Context().Done():
	default:
		t.Fatal("nil lease context was not fail-closed")
	}
}

func waitForRuntimeState(t *testing.T, supervisor *RuntimeLeaseSupervisor, state RuntimeState) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if supervisor.Snapshot().State == state {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("runtime state = %#v, want %s", supervisor.Snapshot(), state)
}
