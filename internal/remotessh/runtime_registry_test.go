package remotessh

import (
	"context"
	"testing"
	"time"
)

const runtimeTargetA = "target-0123456789abcdef0123456789abcdef"

func TestRuntimeRegistryRevokesTargetAndClosesAll(t *testing.T) {
	factory := &fakeHelperGenerationFactory{}
	runtime, connection, generation := newTestRuntimeSupervisor(t, factory, time.Minute)
	root := openRuntimeRoot(t, runtime, generation)
	lease, err := runtime.AcquireTask(context.Background(), runtimeRequest(root, "registry-task"))
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRuntimeRegistry()
	if err := registry.Register(runtimeTargetA, runtime); err != nil {
		t.Fatal(err)
	}
	if err := registry.RevokeTarget(context.Background(), runtimeTargetA); err != nil {
		t.Fatal(err)
	}
	select {
	case <-lease.Context().Done():
	default:
		t.Fatal("target revoke did not cancel its lease")
	}
	if snapshot := connection.Snapshot(); snapshot.State != ConnectionDisconnected {
		t.Fatalf("connection snapshot = %#v", snapshot)
	}
	if err := registry.Close(time.Second); err != nil {
		t.Fatal(err)
	}
	if snapshot := runtime.Snapshot(); snapshot.State != RuntimeClosed {
		t.Fatalf("runtime snapshot = %#v", snapshot)
	}
}

func TestRuntimeRegistryRejectsDuplicateTargetOwner(t *testing.T) {
	registry := NewRuntimeRegistry()
	first, _, _ := newTestRuntimeSupervisor(t, &fakeHelperGenerationFactory{}, time.Minute)
	second, _, _ := newTestRuntimeSupervisor(t, &fakeHelperGenerationFactory{}, time.Minute)
	if err := registry.Register(runtimeTargetA, first); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(runtimeTargetA, second); err == nil {
		t.Fatal("duplicate target runtime was accepted")
	}
	_ = first.Close(context.Background())
	_ = second.Close(context.Background())
}
