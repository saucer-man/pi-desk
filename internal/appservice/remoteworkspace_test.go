package appservice

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pi-desk/internal/remotessh"
	"pi-desk/internal/workspace"
)

func TestRemoteWorkspaceLifecycleIdentityHelpers(t *testing.T) {
	target := workspace.TargetRecord{
		HostKey: workspace.HostKeyBinding{
			Algorithm: "ssh-ed25519", SHA256: "SHA256:" + strings.Repeat("A", 43), ConfigFingerprint: strings.Repeat("a", 64),
		},
	}
	snapshot := remotessh.ConnectionSnapshot{
		State: remotessh.ConnectionReady, Generation: 3,
		Binding: remotessh.ConnectionBinding{
			ConfigFingerprint: target.HostKey.ConfigFingerprint,
			HostKey:           remotessh.HostKeyEvidence{Algorithm: target.HostKey.Algorithm, SHA256Hash: target.HostKey.SHA256},
		},
	}
	if !matchesCatalogTarget(snapshot, target) {
		t.Fatal("matching catalog target was rejected")
	}
	snapshot.Generation = 0
	if matchesCatalogTarget(snapshot, target) {
		t.Fatal("zero connection generation was accepted")
	}
	if !validRemoteThreadID("thread-一") || validRemoteThreadID("thread\ninvalid") {
		t.Fatal("remote thread identity validation is incorrect")
	}
	if first, second := remoteTaskOwner("thread-一"), remoteTaskOwner("thread-一"); first != second || len(first) != len("task-")+32 {
		t.Fatalf("task owner is not stable and bounded: %q %q", first, second)
	}
	token, err := remoteTrustToken()
	if err != nil || !validRemoteTrustToken(token) || validRemoteTrustToken(strings.ToUpper(token)) {
		t.Fatalf("trust token=%q err=%v", token, err)
	}
	if !validRemoteDisplayName("Remote workspace") || validRemoteDisplayName("bad\nname") {
		t.Fatal("remote display name validation is incorrect")
	}
}

func TestRemoteWorkspaceLifecycleRemoveTargetRequiresNoWorkspaceReferences(t *testing.T) {
	catalog := workspace.NewCatalog(filepath.Join(t.TempDir(), "state.json"))
	registry := remotessh.NewRuntimeRegistry()
	coordinator := newTestRemoteCatalogCoordinator(t, catalog, registry)
	registration := workspace.TargetRegistration{
		Name: "Build host", HostAlias: "build-host", ConfigFingerprint: strings.Repeat("a", 64),
		HostKeyAlgorithm: "ssh-ed25519", HostKeySHA256: "SHA256:" + strings.Repeat("A", 43),
	}
	target, err := catalog.RegisterTarget(registration)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := &RemoteWorkspaceLifecycle{
		catalog: catalog, runtimes: registry, remoteCatalog: coordinator,
		targets: make(map[string]*remoteLifecycleTarget), tasks: make(map[string]remoteLifecycleTask),
		pendingRoots:    map[string]remotePendingRoot{"pending": {targetID: target.ID}},
		pendingByTarget: map[string]string{target.ID: "pending"},
		connectAttempts: make(map[*remotessh.ConnectionSupervisor]struct{}),
	}
	workspaceRecord, err := catalog.AddSSHWorkspace(workspace.SSHWorkspaceRegistration{
		Name: "Remote", TargetID: target.ID, RequestedRoot: "/srv/repo", CanonicalRoot: "/srv/repo",
		Device: 1, Inode: 2, RemoteOS: "linux", RemoteArch: "amd64", Trust: "approve",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.RemoveTarget(context.Background(), target.ID); err == nil {
		t.Fatal("referenced target was removed")
	}
	if _, err := catalog.ResolveTarget(target.ID); err != nil {
		t.Fatalf("referenced target disappeared: %v", err)
	}
	if err := catalog.Remove(workspaceRecord.ID); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.RemoveTarget(context.Background(), target.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ResolveTarget(target.ID); err == nil {
		t.Fatal("unreferenced target remains in catalog")
	}
	if len(lifecycle.pendingRoots) != 0 || len(lifecycle.pendingByTarget) != 0 {
		t.Fatal("removed target retained a pending root candidate")
	}
}

func TestRemoteWorkspaceLifecycleCloseDoesNotWaitForTargetOperation(t *testing.T) {
	registry := remotessh.NewRuntimeRegistry()
	target, err := remotessh.NewTarget("build-host")
	if err != nil {
		t.Fatal(err)
	}
	connection, err := remotessh.NewConnectionSupervisor(remotessh.NewLocator(), target)
	if err != nil {
		t.Fatal(err)
	}
	session := &remoteLifecycleTarget{connection: connection, generation: 7, roots: map[string]remoteOpenedRoot{"workspace": {generation: 7}}}
	lifecycle := &RemoteWorkspaceLifecycle{
		runtimes: registry, backends: &RemoteBackendCoordinator{workspaces: make(map[string]remoteWorkspaceBinding), tasks: make(map[string]remoteTaskBinding)},
		targets: map[string]*remoteLifecycleTarget{"target-1": session}, tasks: make(map[string]remoteLifecycleTask),
		pendingRoots: make(map[string]remotePendingRoot), pendingByTarget: make(map[string]string), connectAttempts: make(map[*remotessh.ConnectionSupervisor]struct{}),
	}

	session.mu.Lock()
	closed := make(chan error, 1)
	go func() { closed <- lifecycle.Close(context.Background()) }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close waited for an active target operation")
	}
	session.mu.Unlock()
	if snapshot := session.connection.Snapshot(); snapshot.State != remotessh.ConnectionClosed {
		t.Fatalf("connection state after Close = %q", snapshot.State)
	}
	if err := lifecycle.RemoveTarget(context.Background(), "target-1"); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("mutation after Close error = %v, want closed lifecycle", err)
	}
}

func TestRemoteWorkspaceLifecycleRequiresAllHostDependencies(t *testing.T) {
	if _, err := NewRemoteWorkspaceLifecycle(nil, nil, nil, nil, nil, nil); err == nil {
		t.Fatal("missing remote lifecycle dependencies were accepted")
	}
}
