package appservice

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"pi-desk/internal/domain"
	"pi-desk/internal/remotessh"
	"pi-desk/internal/workspace"
)

func TestRemoteWorkspaceServiceTargetEpochsRejectRevokedCompletions(t *testing.T) {
	lifecycle := &RemoteWorkspaceLifecycle{
		runtimes: remotessh.NewRuntimeRegistry(),
		backends: &RemoteBackendCoordinator{workspaces: make(map[string]remoteWorkspaceBinding), tasks: make(map[string]remoteTaskBinding)},
		targets:  make(map[string]*remoteLifecycleTarget), pendingRoots: make(map[string]remotePendingRoot), pendingByTarget: make(map[string]string),
	}
	service := &RemoteWorkspaceService{lifecycle: lifecycle, targetEpoch: make(map[string]uint64)}
	knownEpoch := service.beginTargetOperation("target-a")
	otherEpoch := service.beginTargetOperation("target-b")
	newEpoch := service.beginNewTargetOperation()
	service.trackPendingTarget("candidate-a", "target-a")

	service.revokeTargetOperations("target-a")
	if targetID := service.takePendingTarget("candidate-a"); targetID != "" {
		t.Fatalf("revoked target retained candidate mapping: %q", targetID)
	}

	if err := service.finishTargetOperation(context.Background(), "target-a", knownEpoch); err == nil || !strings.HasPrefix(err.Error(), "REMOTE_DISCONNECTED:") {
		t.Fatalf("revoked target completion error = %v", err)
	}
	if err := service.finishTargetOperation(context.Background(), "target-b", otherEpoch); err != nil {
		t.Fatalf("unrelated known target was revoked: %v", err)
	}
	if err := service.finishNewTargetOperation(context.Background(), "target-new", newEpoch); err == nil || !strings.HasPrefix(err.Error(), "REMOTE_DISCONNECTED:") {
		t.Fatalf("unknown target completion error = %v", err)
	}
}

func TestRemoteWorkspaceServiceInvalidTargetDoesNotRevokeOperations(t *testing.T) {
	service := &RemoteWorkspaceService{catalog: workspace.NewCatalog(filepath.Join(t.TempDir(), "state.json")), targetEpoch: make(map[string]uint64)}
	epoch := service.beginNewTargetOperation()

	if err := service.DisconnectRemoteTarget(domain.RemoteTargetRequest{TargetID: "target-missing"}); err == nil {
		t.Fatal("missing target disconnect was accepted")
	}
	if err := service.RemoveRemoteTarget(domain.RemoteTargetRequest{TargetID: "target-missing"}); err == nil {
		t.Fatal("missing target removal was accepted")
	}
	if current := service.beginNewTargetOperation(); current != epoch || len(service.targetEpoch) != 0 {
		t.Fatalf("invalid target changed epochs: global=%d want=%d targets=%#v", current, epoch, service.targetEpoch)
	}
}

func TestRemoteWorkspaceServiceInvalidDecisionDoesNotConsumePendingTarget(t *testing.T) {
	token := strings.Repeat("a", 64)
	service := &RemoteWorkspaceService{
		lifecycle: &RemoteWorkspaceLifecycle{}, targetEpoch: make(map[string]uint64),
		pendingTargets: map[string]string{token: "target-a"},
	}

	if _, err := service.DecideRemoteRoot(domain.DecideRemoteRootRequest{Token: token, Trust: "maybe"}); err == nil {
		t.Fatal("invalid root decision was accepted")
	}
	if targetID := service.takePendingTarget(token); targetID != "target-a" {
		t.Fatalf("invalid decision consumed pending target mapping: %q", targetID)
	}
}

func TestRemoteWorkspaceServiceRejectsMixedExistingTargetRequest(t *testing.T) {
	service := &RemoteWorkspaceService{lifecycle: &RemoteWorkspaceLifecycle{}}
	_, err := service.ConnectRemoteTarget(domain.ConnectRemoteTargetRequest{
		TargetID: "target-a", Name: "changed",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot include") {
		t.Fatalf("ConnectRemoteTarget error = %v", err)
	}
}
