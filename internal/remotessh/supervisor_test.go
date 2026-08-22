package remotessh

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type connectionProberFunc func(context.Context, string) (ConnectionPreflight, error)

func (fn connectionProberFunc) ProbeConnection(ctx context.Context, hostAlias string) (ConnectionPreflight, error) {
	return fn(ctx, hostAlias)
}

func supervisorPreflight(configFingerprint, hostFingerprint string) ConnectionPreflight {
	return ConnectionPreflight{
		Config: EffectiveConfig{Fingerprint: configFingerprint},
		HostKey: HostKeyEvidence{
			Algorithm:  "ssh-ed25519",
			SHA256Hash: hostFingerprint,
		},
	}
}

func newTestSupervisor(t *testing.T, prober connectionProber) *ConnectionSupervisor {
	t.Helper()
	target, err := NewTarget("build-prod")
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := newConnectionSupervisor(prober, target)
	if err != nil {
		t.Fatal(err)
	}
	return supervisor
}

func TestConnectionSupervisorConnectReusesReadyGeneration(t *testing.T) {
	var calls int
	supervisor := newTestSupervisor(t, connectionProberFunc(func(_ context.Context, hostAlias string) (ConnectionPreflight, error) {
		calls++
		if hostAlias != "build-prod" {
			t.Fatalf("ProbeConnection alias = %q", hostAlias)
		}
		return supervisorPreflight("config-a", "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), nil
	}))

	first, err := supervisor.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	if first.State != ConnectionReady || first.Generation == 0 || first.Failure != nil {
		t.Fatalf("unexpected ready snapshot: %#v", first)
	}
	if first.Binding.ConfigFingerprint != "config-a" || first.Binding.HostKey.Algorithm != "ssh-ed25519" {
		t.Fatalf("unexpected binding: %#v", first.Binding)
	}
	if err := supervisor.ValidateGeneration(first.Generation); err != nil {
		t.Fatalf("ready generation was rejected: %v", err)
	}

	second, err := supervisor.Connect(context.Background())
	if err != nil {
		t.Fatalf("second Connect returned error: %v", err)
	}
	if second != first || calls != 1 {
		t.Fatalf("ready connection was reprobed or changed: first=%#v second=%#v calls=%d", first, second, calls)
	}
}

func TestConnectionSupervisorDisconnectCancelsBoundGenerationContext(t *testing.T) {
	supervisor := newTestSupervisor(t, connectionProberFunc(func(_ context.Context, _ string) (ConnectionPreflight, error) {
		return supervisorPreflight("config-a", "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), nil
	}))
	ready, err := supervisor.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, release, err := supervisor.bindGenerationContext(context.Background(), ready.Generation)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	supervisor.Disconnect()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("Disconnect did not cancel a bound generation context")
	}
	if _, _, err := supervisor.bindGenerationContext(context.Background(), ready.Generation); !errors.Is(err, ErrConnectionGenerationRevoked) {
		t.Fatalf("revoked generation context error = %v", err)
	}
}

func TestConnectionSupervisorWaitsForRevokedGenerationOperations(t *testing.T) {
	supervisor := newTestSupervisor(t, connectionProberFunc(func(_ context.Context, _ string) (ConnectionPreflight, error) {
		return supervisorPreflight("config-a", "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), nil
	}))
	ready, err := supervisor.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, release, err := supervisor.bindGenerationContext(context.Background(), ready.Generation)
	if err != nil {
		t.Fatal(err)
	}
	supervisor.Disconnect()
	short, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := supervisor.WaitForGenerationIdle(short, ready.Generation); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitForGenerationIdle before release = %v", err)
	}
	release()
	if err := supervisor.WaitForGenerationIdle(context.Background(), ready.Generation); err != nil {
		t.Fatalf("WaitForGenerationIdle after release = %v", err)
	}
}

func TestConnectionSupervisorRejectsConcurrentConnect(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	supervisor := newTestSupervisor(t, connectionProberFunc(func(_ context.Context, _ string) (ConnectionPreflight, error) {
		close(started)
		<-release
		return supervisorPreflight("config-a", "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), nil
	}))

	firstDone := make(chan error, 1)
	go func() {
		_, err := supervisor.Connect(context.Background())
		firstDone <- err
	}()
	<-started
	_, err := supervisor.Connect(context.Background())
	var lifecycleErr *ConnectionSupervisorError
	if !errors.As(err, &lifecycleErr) || lifecycleErr.Failure.Code != FailureConnect || lifecycleErr.Failure.Reason != ReasonConnectionInProgress || !errors.Is(err, ErrConnectionInProgress) {
		t.Fatalf("concurrent Connect error = %v, want connection-in-progress failure", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("initial Connect returned error: %v", err)
	}
}

func TestConnectionSupervisorDisconnectRevokesRunningAttempt(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	supervisor := newTestSupervisor(t, connectionProberFunc(func(ctx context.Context, _ string) (ConnectionPreflight, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return ConnectionPreflight{}, &ConnectionProbeError{
			Failure: ConnectionFailure{Code: FailureCancelled, Reason: ReasonCancelled},
			cause:   ctx.Err(),
		}
	}))

	type result struct {
		snapshot ConnectionSnapshot
		err      error
	}
	done := make(chan result, 1)
	go func() {
		snapshot, err := supervisor.Connect(context.Background())
		done <- result{snapshot: snapshot, err: err}
	}()
	<-started
	snapshot := supervisor.Disconnect()
	if snapshot.State != ConnectionDisconnected || snapshot.Generation != 0 || snapshot.Failure == nil || *snapshot.Failure != (ConnectionFailure{Code: FailureDisconnected, Reason: ReasonDisconnected}) {
		t.Fatalf("Disconnect snapshot = %#v", snapshot)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("Disconnect did not cancel the in-flight probe")
	}
	resultValue := <-done
	var lifecycleErr *ConnectionSupervisorError
	if !errors.As(resultValue.err, &lifecycleErr) || lifecycleErr.Failure.Code != FailureDisconnected || !errors.Is(resultValue.err, ErrConnectionGenerationRevoked) {
		t.Fatalf("cancelled Connect error = %v, want revoked generation", resultValue.err)
	}
	if resultValue.snapshot.State != ConnectionDisconnected || resultValue.snapshot.Generation != 0 {
		t.Fatalf("cancelled Connect returned an authorizing snapshot: %#v", resultValue.snapshot)
	}
}

func TestConnectionSupervisorCallerCancellationIsStableFailure(t *testing.T) {
	supervisor := newTestSupervisor(t, connectionProberFunc(func(ctx context.Context, _ string) (ConnectionPreflight, error) {
		<-ctx.Done()
		return ConnectionPreflight{}, &ConnectionProbeError{
			Failure: ConnectionFailure{Code: FailureCancelled, Reason: ReasonCancelled},
			cause:   ctx.Err(),
		}
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := supervisor.Connect(ctx)
	var probeErr *ConnectionProbeError
	if !errors.As(err, &probeErr) || probeErr.Failure.Code != FailureCancelled || probeErr.Failure.Reason != ReasonCancelled {
		t.Fatalf("Connect error = %v, want cancelled probe failure", err)
	}
	snapshot := supervisor.Snapshot()
	if snapshot.State != ConnectionDisconnected || snapshot.Failure == nil || *snapshot.Failure != probeErr.Failure {
		t.Fatalf("cancelled state = %#v", snapshot)
	}
}

func TestConnectionSupervisorRejectsIdentityDrift(t *testing.T) {
	preflights := []ConnectionPreflight{
		supervisorPreflight("config-a", "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"),
		supervisorPreflight("config-b", "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"),
	}
	var mu sync.Mutex
	call := 0
	supervisor := newTestSupervisor(t, connectionProberFunc(func(_ context.Context, _ string) (ConnectionPreflight, error) {
		mu.Lock()
		defer mu.Unlock()
		result := preflights[call]
		call++
		return result, nil
	}))

	first, err := supervisor.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	supervisor.Disconnect()
	second, err := supervisor.Connect(context.Background())
	var lifecycleErr *ConnectionSupervisorError
	if !errors.As(err, &lifecycleErr) || lifecycleErr.Failure.Code != FailureConnect || lifecycleErr.Failure.Reason != ReasonIdentityChanged || !errors.Is(err, ErrConnectionIdentityChanged) {
		t.Fatalf("identity drift error = %v", err)
	}
	if second.State != ConnectionDisconnected || second.Generation != 0 || second.Failure == nil || *second.Failure != lifecycleErr.Failure {
		t.Fatalf("identity drift snapshot = %#v", second)
	}
	if err := supervisor.ValidateGeneration(first.Generation); !errors.Is(err, ErrConnectionGenerationRevoked) {
		t.Fatalf("old generation validation error = %v, want revoked", err)
	}
}

func TestConnectionSupervisorLateProbeCannotRestoreReadyState(t *testing.T) {
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	supervisor := newTestSupervisor(t, connectionProberFunc(func(_ context.Context, _ string) (ConnectionPreflight, error) {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		if call == 1 {
			close(firstStarted)
			<-firstRelease // Deliberately ignore cancellation to model a stuck child process.
			return supervisorPreflight("config-a", "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), nil
		}
		return supervisorPreflight("config-a", "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), nil
	}))

	firstDone := make(chan struct {
		snapshot ConnectionSnapshot
		err      error
	}, 1)
	go func() {
		snapshot, err := supervisor.Connect(context.Background())
		firstDone <- struct {
			snapshot ConnectionSnapshot
			err      error
		}{snapshot, err}
	}()
	<-firstStarted
	supervisor.Disconnect()
	second, err := supervisor.Connect(context.Background())
	if err != nil || second.State != ConnectionReady {
		t.Fatalf("replacement Connect = %#v, %v", second, err)
	}
	close(firstRelease)
	first := <-firstDone
	if !errors.Is(first.err, ErrConnectionGenerationRevoked) || first.snapshot.State != ConnectionDisconnected || first.snapshot.Generation != 0 {
		t.Fatalf("late first attempt result = %#v, %v", first.snapshot, first.err)
	}
	current := supervisor.Snapshot()
	if current != second || current.State != ConnectionReady {
		t.Fatalf("late attempt replaced ready state: current=%#v second=%#v", current, second)
	}
}

func TestConnectionSupervisorCloseIsTerminal(t *testing.T) {
	supervisor := newTestSupervisor(t, connectionProberFunc(func(_ context.Context, _ string) (ConnectionPreflight, error) {
		return supervisorPreflight("config-a", "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), nil
	}))
	supervisor.Close()
	snapshot, err := supervisor.Connect(context.Background())
	if snapshot.State != ConnectionClosed {
		t.Fatalf("closed Connect snapshot = %#v", snapshot)
	}
	if !errors.Is(err, ErrConnectionSupervisorClosed) {
		t.Fatalf("closed Connect error = %v", err)
	}
}
