package appservice

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pi-desk/internal/domain"
	"pi-desk/internal/piruntime"
	"pi-desk/internal/remotessh"
	"pi-desk/internal/repository"
	terminalruntime "pi-desk/internal/terminal"
	"pi-desk/internal/workspace"
)

func TestLiveRemoteWorkspaceLifecycle(t *testing.T) {
	hostAlias := os.Getenv("PI_DESK_SSH_LIVE_TARGET")
	rootPath := os.Getenv("PI_DESK_SSH_LIVE_DIRECTORY")
	remoteOS := os.Getenv("PI_DESK_SSH_LIVE_HELPER_OS")
	remoteArch := os.Getenv("PI_DESK_SSH_LIVE_HELPER_ARCH")
	if hostAlias == "" || rootPath == "" || remoteOS == "" || remoteArch == "" {
		t.Skip("set PI_DESK_SSH_LIVE_TARGET, PI_DESK_SSH_LIVE_DIRECTORY and helper platform variables")
	}
	if !path.IsAbs(rootPath) || path.Clean(rootPath) != rootPath {
		t.Fatal("live root must be a normalized absolute POSIX path")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	bundle, err := remotessh.NewHelperArtifactBundle(os.DirFS("../.."), "build/remote-helper/artifacts")
	if err != nil {
		t.Fatal(err)
	}
	catalog := workspace.NewCatalog(filepath.Join(t.TempDir(), "state.json"))
	runtimeRegistry := remotessh.NewRuntimeRegistry()
	repositoryService := NewRepositoryService(catalog, repository.New())
	terminalService := NewTerminalService(catalog)
	terminalService.remote = terminalruntime.NewRemoteManager(ctx, nil)
	backends, err := NewRemoteBackendCoordinator(catalog, repositoryService, terminalService)
	if err != nil {
		t.Fatal(err)
	}
	remoteCatalog, err := NewRemoteCatalogCoordinator(catalog, runtimeRegistry, backends)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := NewRemoteWorkspaceLifecycle(catalog, remotessh.NewLocator(), bundle, runtimeRegistry, backends, remoteCatalog)
	if err != nil {
		t.Fatal(err)
	}
	defer lifecycle.Close(context.Background())

	targetID, err := lifecycle.ConnectNewTarget(ctx, "Live SSH", hostAlias)
	if err != nil {
		t.Fatalf("connect target=%q err=%v", targetID, err)
	}
	readyGeneration := lifecycle.targets[targetID].connection.Snapshot().Generation
	if readyGeneration == 0 {
		t.Fatal("connect returned zero generation")
	}
	candidate, err := lifecycle.PrepareRootTrust(ctx, targetID, "Live workspace", rootPath, "0.84.2")
	if err != nil || candidate.Root.CanonicalPath != rootPath {
		t.Fatalf("candidate=%#v err=%v", candidate, err)
	}
	if _, err := lifecycle.PrepareRootTrust(ctx, targetID, "Duplicate pending", rootPath, "0.84.2"); err == nil {
		t.Fatal("second root candidate bypassed the pending trust decision")
	}
	workspaceRecord, err := lifecycle.DecideRootTrust(ctx, candidate.Token, "approve")
	if err != nil || workspaceRecord.ID == "" || workspaceRecord.Trust != "approve" ||
		workspaceRecord.Location.SSH.RemoteOS != remoteOS || workspaceRecord.Location.SSH.RemoteArch != remoteArch {
		t.Fatalf("workspace=%#v err=%v", workspaceRecord, err)
	}
	if _, err := lifecycle.DecideRootTrust(ctx, candidate.Token, "approve"); err == nil {
		t.Fatal("one-shot root trust token was reused")
	}
	lease, err := lifecycle.AcquireTask(ctx, "thread-live-lifecycle", workspaceRecord.ID)
	if err != nil || lease.Kind() != remotessh.RuntimeTaskLease || lease.Generation() != readyGeneration {
		t.Fatalf("lease=%#v err=%v", lease, err)
	}
	if _, err := repositoryService.Snapshot(domain.RepositoryRequest{WorkspaceID: workspaceRecord.ID}); err != nil {
		t.Fatalf("bound Repository snapshot: %v", err)
	}
	if err := lifecycle.StopTask("thread-live-lifecycle"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-lease.Context().Done():
	default:
		t.Fatal("task stop did not release the task lease")
	}
	disconnectLease, err := lifecycle.AcquireTask(ctx, "thread-live-disconnect", workspaceRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.DisconnectTarget(ctx, targetID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-disconnectLease.Context().Done():
	default:
		t.Fatal("disconnect did not revoke the task lease")
	}
	if _, err := repositoryService.Snapshot(domain.RepositoryRequest{WorkspaceID: workspaceRecord.ID}); err == nil {
		t.Fatal("disconnect retained the Repository backend")
	}
	remoteService, err := NewRemoteWorkspaceService(catalog, lifecycle, piruntime.NewLocator())
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := remoteService.ResumeRemoteWorkspace(domain.ResumeRemoteWorkspaceRequest{
		WorkspaceID: workspaceRecord.ID,
	})
	if err != nil || resumed.ID != workspaceRecord.ID {
		t.Fatalf("resume=%#v err=%v", resumed, err)
	}
	resumedState := lifecycle.TargetState(targetID)
	if resumedState != remotessh.ConnectionReady {
		t.Fatalf("resume target state=%s", resumedState)
	}
	reconnectedGeneration := lifecycle.targets[targetID].connection.Snapshot().Generation
	if reconnectedGeneration <= readyGeneration {
		t.Fatalf("resume generation=%d want > %d", reconnectedGeneration, readyGeneration)
	}
	if _, err := repositoryService.Snapshot(domain.RepositoryRequest{WorkspaceID: workspaceRecord.ID}); err != nil {
		t.Fatalf("reconnected Repository snapshot: %v", err)
	}
	piSupervisor := piruntime.NewSupervisor(ctx, piruntime.NewExecStarter(piruntime.NewLocator()), nil)
	agent := NewAgentService(piruntime.NewLocator(), nil, lifecycle, filepath.Join(t.TempDir(), "anchors"))
	agent.runtime = piSupervisor
	live, err := agent.StartSession(domain.StartSessionRequest{
		ThreadID: "thread-live-agent", WorkspaceID: workspaceRecord.ID, Trust: "approve",
		NoSession: true, Offline: true, DisableThemes: true, DisableSkills: true, DisablePlugins: true,
	})
	if err != nil || live.Generation == 0 {
		t.Fatalf("start remote Pi adapter session=%#v err=%v", live, err)
	}
	bashResult, err := agent.Bash(domain.BashRequest{ThreadID: "thread-live-agent", Command: "printf pi-desk-remote-adapter"})
	if err != nil || !strings.Contains(bashResult.DataJSON, "pi-desk-remote-adapter") {
		diagnostics, _ := agent.GetDiagnostics(domain.ThreadRequest{ThreadID: "thread-live-agent"})
		remoteRuntime, taskLease, _, taskErr := lifecycle.taskBackend("thread-live-agent")
		var leaseErr error
		var runtimeSnapshot any
		if taskLease != nil {
			leaseErr = taskLease.Context().Err()
		}
		if remoteRuntime != nil {
			runtimeSnapshot = remoteRuntime.Snapshot()
		}
		t.Fatalf("remote Pi Bash data=%s err=%v diagnostics=%s taskErr=%v leaseErr=%v runtime=%#v", bashResult.DataJSON, err, diagnostics, taskErr, leaseErr, runtimeSnapshot)
	}
	if err := agent.StopSession(domain.ThreadRequest{ThreadID: "thread-live-agent"}); err != nil {
		t.Fatal(err)
	}
	piSupervisor.Shutdown()
	if err := lifecycle.DisconnectTarget(ctx, targetID); err != nil {
		t.Fatal(err)
	}
	if err := remoteCatalog.RemoveWorkspace(ctx, workspaceRecord.ID); err != nil {
		t.Fatal(err)
	}
	if err := remoteService.RemoveRemoteTarget(domain.RemoteTargetRequest{TargetID: targetID}); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ResolveTarget(targetID); err == nil {
		t.Fatal("removed live target remained in the catalog")
	}

	deniedTargetID, err := lifecycle.ConnectNewTarget(ctx, "Rejected SSH", hostAlias)
	if err != nil {
		t.Fatal(err)
	}
	deniedCandidate, err := lifecycle.PrepareRootTrust(ctx, deniedTargetID, "Rejected workspace", rootPath, "0.84.2")
	if err != nil {
		t.Fatal(err)
	}
	deniedWorkspace, err := lifecycle.DecideRootTrust(ctx, deniedCandidate.Token, "deny")
	if err != nil || deniedWorkspace.Trust != "deny" {
		t.Fatalf("denied workspace=%#v err=%v", deniedWorkspace, err)
	}
	if state := lifecycle.TargetState(deniedTargetID); state != remotessh.ConnectionDisconnected {
		t.Fatalf("denied target state=%s", state)
	}
	if _, err := repositoryService.Snapshot(domain.RepositoryRequest{WorkspaceID: deniedWorkspace.ID}); err == nil {
		t.Fatal("denied root retained the Repository backend")
	}
	if err := remoteCatalog.RemoveWorkspace(ctx, deniedWorkspace.ID); err != nil {
		t.Fatal(err)
	}
	if err := remoteService.RemoveRemoteTarget(domain.RemoteTargetRequest{TargetID: deniedTargetID}); err != nil {
		t.Fatal(err)
	}
}
