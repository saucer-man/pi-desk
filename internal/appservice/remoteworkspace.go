package appservice

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"pi-desk/internal/remotessh"
	"pi-desk/internal/workspace"
)

type RemoteRootCandidate struct {
	Token    string
	TargetID string
	Root     remotessh.RuntimeRootIdentity
}

type remoteOpenedRoot struct {
	generation uint64
	capability *remotessh.RuntimeRootCapability
	identity   remotessh.RuntimeRootIdentity
}

type remoteLifecycleTarget struct {
	mu          sync.Mutex
	connection  *remotessh.ConnectionSupervisor
	installer   *remotessh.HelperInstaller
	runtime     *remotessh.RuntimeLeaseSupervisor
	artifact    remotessh.HelperArtifact
	provisional bool
	generation  uint64
	roots       map[string]remoteOpenedRoot
}

type remoteLifecycleTask struct {
	workspaceID string
	lease       *remotessh.RuntimeLease
}

type remotePendingRoot struct {
	workspaceID   string
	targetID      string
	name          string
	requestedRoot string
	remoteOS      string
	remoteArch    string
	root          remoteOpenedRoot
	lease         *remotessh.RuntimeLease
	session       *remoteLifecycleTarget
}

// RemoteWorkspaceLifecycle is the host-only orchestration path for
// SSH targets and workspaces. Initial identity remains an in-memory one-shot
// candidate until explicit approve/deny. It does not register Wails methods,
// accept SSH options, or start Pi.
type RemoteWorkspaceLifecycle struct {
	catalog       *workspace.Catalog
	locator       *remotessh.Locator
	artifacts     *remotessh.HelperArtifactBundle
	runtimes      *remotessh.RuntimeRegistry
	backends      *RemoteBackendCoordinator
	remoteCatalog *RemoteCatalogCoordinator

	mu              sync.Mutex
	targets         map[string]*remoteLifecycleTarget
	tasks           map[string]remoteLifecycleTask
	pendingRoots    map[string]remotePendingRoot
	pendingByTarget map[string]string
	connectAttempts map[*remotessh.ConnectionSupervisor]struct{}
	closed          bool
}

func NewRemoteWorkspaceLifecycle(
	catalog *workspace.Catalog,
	locator *remotessh.Locator,
	artifacts *remotessh.HelperArtifactBundle,
	runtimes *remotessh.RuntimeRegistry,
	backends *RemoteBackendCoordinator,
	remoteCatalog *RemoteCatalogCoordinator,
) (*RemoteWorkspaceLifecycle, error) {
	if catalog == nil || locator == nil || artifacts == nil || runtimes == nil || backends == nil || remoteCatalog == nil {
		return nil, errors.New("remote workspace lifecycle dependencies are required")
	}
	return &RemoteWorkspaceLifecycle{
		catalog: catalog, locator: locator, artifacts: artifacts, runtimes: runtimes,
		backends: backends, remoteCatalog: remoteCatalog,
		targets: make(map[string]*remoteLifecycleTarget), tasks: make(map[string]remoteLifecycleTask),
		pendingRoots: make(map[string]remotePendingRoot), pendingByTarget: make(map[string]string),
		connectAttempts: make(map[*remotessh.ConnectionSupervisor]struct{}),
	}, nil
}

func (lifecycle *RemoteWorkspaceLifecycle) TargetState(targetID string) remotessh.ConnectionState {
	lifecycle.mu.Lock()
	session := lifecycle.targets[strings.TrimSpace(targetID)]
	lifecycle.mu.Unlock()
	if session == nil {
		return remotessh.ConnectionDisconnected
	}
	return session.connection.Snapshot().State
}

func (lifecycle *RemoteWorkspaceLifecycle) ConnectNewTarget(ctx context.Context, name, hostAlias string) (string, error) {
	name, hostAlias = strings.TrimSpace(name), strings.TrimSpace(hostAlias)
	if !validRemoteDisplayName(name) {
		return "", errors.New("remote target name is invalid")
	}
	sshTarget, err := remotessh.NewTarget(hostAlias)
	if err != nil {
		return "", err
	}
	targets, err := lifecycle.catalog.ListTargets()
	if err != nil {
		return "", err
	}
	for _, target := range targets {
		if strings.EqualFold(target.HostAlias, sshTarget.HostAlias) {
			return target.ID, lifecycle.ConnectTarget(ctx, target.ID)
		}
	}
	connection, err := remotessh.NewConnectionSupervisor(lifecycle.locator, sshTarget)
	if err != nil {
		return "", err
	}
	lifecycle.mu.Lock()
	if lifecycle.closed {
		lifecycle.mu.Unlock()
		connection.Close()
		return "", errors.New("remote workspace lifecycle is closed")
	}
	lifecycle.connectAttempts[connection] = struct{}{}
	lifecycle.mu.Unlock()
	defer func() {
		lifecycle.mu.Lock()
		delete(lifecycle.connectAttempts, connection)
		lifecycle.mu.Unlock()
	}()
	ready, err := connection.Connect(ctx)
	if err != nil {
		connection.Close()
		return "", err
	}
	lifecycle.mu.Lock()
	closed := lifecycle.closed
	lifecycle.mu.Unlock()
	if closed {
		connection.Close()
		return "", errors.New("remote workspace lifecycle is closed")
	}
	record, err := lifecycle.catalog.RegisterTarget(workspace.TargetRegistration{
		Name: name, HostAlias: sshTarget.HostAlias,
		ConfigFingerprint: ready.Binding.ConfigFingerprint,
		HostKeyAlgorithm:  ready.Binding.HostKey.Algorithm, HostKeySHA256: ready.Binding.HostKey.SHA256Hash,
	})
	if err != nil {
		connection.Close()
		return "", err
	}
	session, err := lifecycle.sessionForConnection(connection)
	if err != nil {
		connection.Close()
		return "", err
	}
	session.generation = ready.Generation
	lifecycle.mu.Lock()
	if lifecycle.closed {
		lifecycle.mu.Unlock()
		connection.Close()
		_ = lifecycle.catalog.RemoveTarget(record.ID)
		return "", errors.New("remote workspace lifecycle is closed")
	}
	if existing := lifecycle.targets[record.ID]; existing != nil {
		lifecycle.mu.Unlock()
		connection.Close()
		return record.ID, lifecycle.ConnectTarget(ctx, record.ID)
	}
	session.provisional = true
	lifecycle.targets[record.ID] = session
	lifecycle.mu.Unlock()
	return record.ID, nil
}

func (lifecycle *RemoteWorkspaceLifecycle) PrepareRootTrust(ctx context.Context, targetID, name, requestedRoot, piVersion string) (RemoteRootCandidate, error) {
	targetID, name, requestedRoot = strings.TrimSpace(targetID), strings.TrimSpace(name), strings.TrimSpace(requestedRoot)
	piVersion = strings.TrimSpace(piVersion)
	if !validRemoteDisplayName(name) {
		return RemoteRootCandidate{}, errors.New("remote workspace name is invalid")
	}
	target, err := lifecycle.catalog.ResolveTarget(targetID)
	if err != nil {
		return RemoteRootCandidate{}, err
	}
	session, err := lifecycle.target(target)
	if err != nil {
		return RemoteRootCandidate{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if err := lifecycle.requireOpen(); err != nil {
		return RemoteRootCandidate{}, err
	}
	ready := session.connection.Snapshot()
	if ready.State != remotessh.ConnectionReady {
		if session.runtime != nil {
			_ = lifecycle.runtimes.RevokeAndRemoveTarget(ctx, target.ID)
			session.runtime = nil
			session.artifact = remotessh.HelperArtifact{}
		}
		var connectErr error
		ready, connectErr = session.connection.Connect(ctx)
		if connectErr != nil {
			return RemoteRootCandidate{}, connectErr
		}
	}
	if !matchesCatalogTarget(ready, target) {
		return RemoteRootCandidate{}, errors.New("remote target is disconnected or changed")
	}
	if session.generation != 0 && session.generation != ready.Generation {
		_ = lifecycle.runtimes.RevokeAndRemoveTarget(ctx, target.ID)
		session.runtime = nil
		session.artifact = remotessh.HelperArtifact{}
		session.roots = make(map[string]remoteOpenedRoot)
		lifecycle.backends.UnbindTarget(target.ID)
	}
	session.generation = ready.Generation
	var remoteOS, remoteArch string
	if session.runtime != nil {
		remoteOS, remoteArch = session.artifact.OS, session.artifact.Architecture
	} else {
		var platform remotessh.RemotePlatform
		var probeErr error
		for probeAttempt := 0; probeAttempt < 2; probeAttempt++ {
			platform, probeErr = session.connection.ProbePlatform(ctx, ready.Generation)
			if probeErr == nil {
				break
			}
			current := session.connection.Snapshot()
			if current.State != remotessh.ConnectionReady {
				current, probeErr = session.connection.Connect(ctx)
				if probeErr != nil {
					break
				}
			}
			if current.State != remotessh.ConnectionReady || current.Generation == ready.Generation {
				break
			}
			ready = current
			session.generation = current.Generation
		}
		if probeErr != nil {
			session.connection.Disconnect()
			session.generation = 0
			session.roots = make(map[string]remoteOpenedRoot)
			lifecycle.backends.UnbindTarget(target.ID)
			return RemoteRootCandidate{}, probeErr
		}
		remoteOS, remoteArch = platform.OS, platform.Arch
	}
	lifecycle.mu.Lock()
	if priorToken := lifecycle.pendingByTarget[target.ID]; priorToken != "" {
		prior := lifecycle.pendingRoots[priorToken]
		if prior.root.generation == ready.Generation {
			lifecycle.mu.Unlock()
			return RemoteRootCandidate{}, errors.New("remote target already has a pending root trust decision")
		}
		delete(lifecycle.pendingRoots, priorToken)
		delete(lifecycle.pendingByTarget, target.ID)
		if prior.lease != nil {
			prior.lease.Release()
		}
	}
	lifecycle.mu.Unlock()
	runtime, err := lifecycle.ensureRuntimeLocked(ctx, target.ID, session, ready.Generation, remoteOS, remoteArch, piVersion)
	if err != nil {
		session.roots = make(map[string]remoteOpenedRoot)
		lifecycle.backends.UnbindTarget(target.ID)
		return RemoteRootCandidate{}, err
	}
	workspaceID := ""
	if existing, found, err := lifecycle.catalog.FindSSHWorkspaceByRequestedRoot(target.ID, requestedRoot); err != nil {
		return RemoteRootCandidate{}, err
	} else if found {
		workspaceID = existing.ID
	}
	if workspaceID == "" {
		workspaceID, err = workspace.NewWorkspaceID()
		if err != nil {
			return RemoteRootCandidate{}, err
		}
	}
	opened, err := runtime.OpenRoot(ctx, remotessh.RuntimeRootOpenRequest{
		Generation: ready.Generation, WorkspaceID: workspaceID, RequestedRoot: requestedRoot,
	})
	if err != nil {
		if session.connection.Snapshot().State != remotessh.ConnectionReady {
			session.roots = make(map[string]remoteOpenedRoot)
			lifecycle.backends.UnbindTarget(target.ID)
		}
		return RemoteRootCandidate{}, err
	}
	if existing, found, err := lifecycle.catalog.FindSSHWorkspaceByRemoteIdentity(target.ID, opened.Identity.CanonicalPath, opened.Identity.Device, opened.Identity.Inode); err != nil {
		return RemoteRootCandidate{}, err
	} else if found && existing.ID != workspaceID {
		// The helper capability must carry the immutable catalog WorkspaceID.
		// Reopen the same verified root with the existing ID before creating the
		// pending candidate, otherwise the backend join point rejects the bind.
		reopened, err := runtime.OpenRoot(ctx, remotessh.RuntimeRootOpenRequest{
			Generation: ready.Generation, WorkspaceID: existing.ID, RequestedRoot: requestedRoot,
		})
		if err != nil {
			return RemoteRootCandidate{}, err
		}
		opened = reopened
		workspaceID = existing.ID
	}
	token, err := remoteTrustToken()
	if err != nil {
		return RemoteRootCandidate{}, err
	}
	pendingLease, err := runtime.AcquireTask(ctx, remotessh.RuntimeLeaseRequest{
		Root: opened.Capability, OwnerID: remotePendingRootOwner(token),
	})
	if err != nil {
		return RemoteRootCandidate{}, err
	}
	pending := remotePendingRoot{
		workspaceID: workspaceID, targetID: target.ID,
		name: name, requestedRoot: requestedRoot, remoteOS: remoteOS, remoteArch: remoteArch,
		root: remoteOpenedRoot{generation: ready.Generation, capability: opened.Capability, identity: opened.Identity}, lease: pendingLease, session: session,
	}
	lifecycle.mu.Lock()
	if lifecycle.closed {
		lifecycle.mu.Unlock()
		pendingLease.Release()
		return RemoteRootCandidate{}, errors.New("remote workspace lifecycle is closed")
	}
	lifecycle.pendingRoots[token] = pending
	lifecycle.pendingByTarget[target.ID] = token
	lifecycle.mu.Unlock()
	return RemoteRootCandidate{Token: token, TargetID: target.ID, Root: opened.Identity}, nil
}

func (lifecycle *RemoteWorkspaceLifecycle) DecideRootTrust(ctx context.Context, token, trust string) (workspace.Record, error) {
	token, trust = strings.TrimSpace(token), strings.TrimSpace(trust)
	if !validRemoteTrustToken(token) || trust != "approve" && trust != "deny" {
		return workspace.Record{}, errors.New("remote root trust decision is invalid")
	}
	lifecycle.mu.Lock()
	pending, ok := lifecycle.pendingRoots[token]
	if ok {
		delete(lifecycle.pendingRoots, token)
		delete(lifecycle.pendingByTarget, pending.targetID)
	}
	lifecycle.mu.Unlock()
	if !ok {
		return workspace.Record{}, errors.New("remote root trust candidate expired")
	}
	pending.session.mu.Lock()
	defer pending.session.mu.Unlock()
	if err := lifecycle.requireOpen(); err != nil {
		return workspace.Record{}, err
	}
	if pending.session.connection.ValidateGeneration(pending.root.generation) != nil || pending.session.runtime == nil || pending.session.runtime.ValidateGeneration(pending.root.generation) != nil {
		if pending.lease != nil {
			pending.lease.Release()
		}
		return workspace.Record{}, errors.New("remote root trust candidate generation was revoked")
	}
	if pending.lease != nil {
		defer pending.lease.Release()
	}
	identity := pending.root.identity
	record, err := lifecycle.remoteCatalog.AddSSHWorkspace(ctx, workspace.SSHWorkspaceRegistration{
		WorkspaceID: pending.workspaceID, Name: pending.name, TargetID: pending.targetID,
		RequestedRoot: pending.requestedRoot, CanonicalRoot: identity.CanonicalPath,
		Device: identity.Device, Inode: identity.Inode,
		RemoteOS: pending.remoteOS, RemoteArch: pending.remoteArch, Trust: trust,
	})
	if err != nil {
		return workspace.Record{}, err
	}
	pending.session.provisional = false
	if trust == "deny" {
		pending.session.generation = 0
		pending.session.roots = make(map[string]remoteOpenedRoot)
		return record, nil
	}
	lifecycle.mu.Lock()
	closed := lifecycle.closed
	lifecycle.mu.Unlock()
	if closed {
		return workspace.Record{}, errors.New("remote workspace lifecycle is closed")
	}
	if err := lifecycle.backends.BindWorkspace(record.ID, pending.targetID, pending.session.runtime, pending.root.capability); err != nil {
		return workspace.Record{}, err
	}
	lifecycle.mu.Lock()
	if lifecycle.closed {
		lifecycle.mu.Unlock()
		lifecycle.backends.UnbindTarget(pending.targetID)
		return workspace.Record{}, errors.New("remote workspace lifecycle is closed")
	}
	pending.session.roots[record.ID] = pending.root
	lifecycle.mu.Unlock()
	return record, nil
}

func (lifecycle *RemoteWorkspaceLifecycle) ConnectTarget(ctx context.Context, targetID string) error {
	targetID = strings.TrimSpace(targetID)
	target, err := lifecycle.catalog.ResolveTarget(targetID)
	if err != nil {
		return err
	}
	session, err := lifecycle.target(target)
	if err != nil {
		return err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if err := lifecycle.requireOpen(); err != nil {
		return err
	}
	if snapshot := session.connection.Snapshot(); snapshot.State != remotessh.ConnectionReady && session.generation != 0 {
		_ = lifecycle.runtimes.RevokeAndRemoveTarget(ctx, target.ID)
		session.runtime = nil
		session.artifact = remotessh.HelperArtifact{}
		session.roots = make(map[string]remoteOpenedRoot)
		lifecycle.mu.Lock()
		var pending remotePendingRoot
		var hasPending bool
		if token := lifecycle.pendingByTarget[target.ID]; token != "" {
			pending, hasPending = lifecycle.pendingRoots[token]
			delete(lifecycle.pendingRoots, token)
			delete(lifecycle.pendingByTarget, target.ID)
		}
		lifecycle.mu.Unlock()
		if hasPending && pending.lease != nil {
			pending.lease.Release()
		}
		lifecycle.backends.UnbindTarget(target.ID)
	}
	ready, err := session.connection.Connect(ctx)
	if err != nil {
		return err
	}
	if !matchesCatalogTarget(ready, target) {
		session.connection.Disconnect()
		session.roots = make(map[string]remoteOpenedRoot)
		_ = lifecycle.runtimes.RevokeAndRemoveTarget(ctx, target.ID)
		session.runtime = nil
		session.artifact = remotessh.HelperArtifact{}
		lifecycle.backends.UnbindTarget(target.ID)
		return workspace.ErrTargetIdentityChanged
	}
	if err := lifecycle.requireOpen(); err != nil {
		return err
	}
	if _, err := lifecycle.catalog.RegisterTarget(workspace.TargetRegistration{
		Name: target.Name, HostAlias: target.HostAlias,
		ConfigFingerprint: ready.Binding.ConfigFingerprint,
		HostKeyAlgorithm:  ready.Binding.HostKey.Algorithm, HostKeySHA256: ready.Binding.HostKey.SHA256Hash,
	}); err != nil {
		session.connection.Disconnect()
		return err
	}
	lifecycle.mu.Lock()
	if lifecycle.closed {
		lifecycle.mu.Unlock()
		session.connection.Disconnect()
		return errors.New("remote workspace lifecycle is closed")
	}
	session.generation = ready.Generation
	lifecycle.mu.Unlock()
	return nil
}

func (lifecycle *RemoteWorkspaceLifecycle) OpenWorkspace(ctx context.Context, workspaceID, piVersion string) error {
	record, target, session, err := lifecycle.approvedWorkspace(workspaceID)
	if err != nil {
		return err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if err := lifecycle.requireOpen(); err != nil {
		return err
	}
	ready := session.connection.Snapshot()
	if !matchesCatalogTarget(ready, target) {
		return errors.New("remote target is disconnected or changed")
	}
	runtime, err := lifecycle.ensureRuntimeLocked(ctx, target.ID, session, ready.Generation, record.Location.SSH.RemoteOS, record.Location.SSH.RemoteArch, strings.TrimSpace(piVersion))
	if err != nil {
		session.roots = make(map[string]remoteOpenedRoot)
		lifecycle.backends.UnbindTarget(target.ID)
		return err
	}
	expected := remotessh.RuntimeRootIdentity{
		CanonicalPath: record.Location.SSH.CanonicalRoot,
		Device:        record.Location.SSH.Device, Inode: record.Location.SSH.Inode,
	}
	opened, err := runtime.OpenRoot(ctx, remotessh.RuntimeRootOpenRequest{
		Generation: ready.Generation, WorkspaceID: record.ID,
		RequestedRoot: record.Location.SSH.RequestedRoot, Expected: &expected,
	})
	if err != nil {
		if session.connection.Snapshot().State != remotessh.ConnectionReady {
			session.roots = make(map[string]remoteOpenedRoot)
			lifecycle.backends.UnbindTarget(target.ID)
		}
		if errors.Is(err, remotessh.ErrHelperRootBindingChanged) {
			lifecycle.denyDriftedWorkspace(ctx, record)
		}
		return err
	}
	if err := lifecycle.requireOpen(); err != nil {
		return err
	}
	if err := lifecycle.backends.BindWorkspace(record.ID, target.ID, runtime, opened.Capability); err != nil {
		return err
	}
	lifecycle.mu.Lock()
	if lifecycle.closed {
		lifecycle.mu.Unlock()
		lifecycle.backends.UnbindTarget(target.ID)
		return errors.New("remote workspace lifecycle is closed")
	}
	session.roots[record.ID] = remoteOpenedRoot{generation: ready.Generation, capability: opened.Capability, identity: opened.Identity}
	lifecycle.mu.Unlock()
	return nil
}

func (lifecycle *RemoteWorkspaceLifecycle) AcquireTask(ctx context.Context, threadID, workspaceID string) (*remotessh.RuntimeLease, error) {
	threadID, workspaceID = strings.TrimSpace(threadID), strings.TrimSpace(workspaceID)
	if !validRemoteThreadID(threadID) {
		return nil, errors.New("remote task thread identity is invalid")
	}
	record, _, session, err := lifecycle.approvedWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	session.mu.Lock()
	root, ok := session.roots[record.ID]
	runtime := session.runtime
	if !ok || runtime == nil || root.generation == 0 || runtime.ValidateGeneration(root.generation) != nil {
		session.mu.Unlock()
		return nil, ErrRemoteRepositoryStale
	}
	lifecycle.mu.Lock()
	if lifecycle.closed {
		lifecycle.mu.Unlock()
		session.mu.Unlock()
		return nil, errors.New("remote workspace lifecycle is closed")
	}
	if _, exists := lifecycle.tasks[threadID]; exists {
		lifecycle.mu.Unlock()
		session.mu.Unlock()
		return nil, errors.New("remote task is already active")
	}
	lease, err := runtime.AcquireTask(ctx, remotessh.RuntimeLeaseRequest{Root: root.capability, OwnerID: remoteTaskOwner(threadID)})
	if err == nil {
		err = lifecycle.backends.BindTask(threadID, record.ID, lease)
	}
	if err != nil {
		if lease != nil {
			lease.Release()
		}
		lifecycle.mu.Unlock()
		session.mu.Unlock()
		return nil, err
	}
	lifecycle.tasks[threadID] = remoteLifecycleTask{workspaceID: record.ID, lease: lease}
	lifecycle.mu.Unlock()
	session.mu.Unlock()
	go lifecycle.watchTask(threadID, lease)
	return lease, nil
}

func (lifecycle *RemoteWorkspaceLifecycle) taskBackend(threadID string) (*remotessh.RuntimeLeaseSupervisor, *remotessh.RuntimeLease, workspace.Record, error) {
	threadID = strings.TrimSpace(threadID)
	lifecycle.mu.Lock()
	task, ok := lifecycle.tasks[threadID]
	lifecycle.mu.Unlock()
	if !ok || task.lease == nil || task.lease.Context().Err() != nil {
		return nil, nil, workspace.Record{}, ErrRemoteRepositoryStale
	}
	record, err := lifecycle.catalog.ResolveID(task.workspaceID)
	if err != nil || record.Location.Kind != workspace.KindSSH || record.Trust != "approve" {
		return nil, nil, workspace.Record{}, ErrRemoteContextChanged
	}
	lifecycle.mu.Lock()
	session := lifecycle.targets[record.Location.SSH.TargetID]
	lifecycle.mu.Unlock()
	if session == nil {
		return nil, nil, workspace.Record{}, ErrRemoteRepositoryStale
	}
	session.mu.Lock()
	runtime := session.runtime
	generation := task.lease.Generation()
	valid := runtime != nil && generation != 0 && runtime.ValidateGeneration(generation) == nil
	session.mu.Unlock()
	if !valid {
		return nil, nil, workspace.Record{}, ErrRemoteRepositoryStale
	}
	return runtime, task.lease, record, nil
}

func (lifecycle *RemoteWorkspaceLifecycle) StopTask(threadID string) error {
	threadID = strings.TrimSpace(threadID)
	lifecycle.mu.Lock()
	task, ok := lifecycle.tasks[threadID]
	if ok {
		delete(lifecycle.tasks, threadID)
	}
	lifecycle.mu.Unlock()
	if !ok {
		return nil
	}
	err := lifecycle.backends.UnbindTask(threadID, task.lease)
	task.lease.Release()
	return err
}

func (lifecycle *RemoteWorkspaceLifecycle) RemoveTarget(ctx context.Context, targetID string) error {
	if err := lifecycle.requireOpen(); err != nil {
		return err
	}
	targetID = strings.TrimSpace(targetID)
	if _, err := lifecycle.catalog.ResolveTarget(targetID); err != nil {
		return err
	}
	lifecycle.mu.Lock()
	session := lifecycle.targets[targetID]
	lifecycle.mu.Unlock()
	if session != nil {
		session.mu.Lock()
		defer session.mu.Unlock()
	}
	removeErr := lifecycle.remoteCatalog.RemoveTarget(ctx, targetID)
	if _, err := lifecycle.catalog.ResolveTarget(targetID); err == nil {
		return removeErr
	}
	lifecycle.mu.Lock()
	if token := lifecycle.pendingByTarget[targetID]; token != "" {
		delete(lifecycle.pendingRoots, token)
		delete(lifecycle.pendingByTarget, targetID)
	}
	delete(lifecycle.targets, targetID)
	lifecycle.mu.Unlock()
	if session != nil {
		session.connection.Close()
		session.generation = 0
		session.runtime = nil
		session.artifact = remotessh.HelperArtifact{}
		session.roots = make(map[string]remoteOpenedRoot)
	}
	return removeErr
}

func (lifecycle *RemoteWorkspaceLifecycle) DisconnectTarget(ctx context.Context, targetID string) error {
	targetID = strings.TrimSpace(targetID)
	lifecycle.mu.Lock()
	session := lifecycle.targets[targetID]
	var pending remotePendingRoot
	var hasPending bool
	if token := lifecycle.pendingByTarget[targetID]; token != "" {
		pending, hasPending = lifecycle.pendingRoots[token]
		delete(lifecycle.pendingRoots, token)
		delete(lifecycle.pendingByTarget, targetID)
	}
	lifecycle.mu.Unlock()
	if hasPending && pending.lease != nil {
		pending.lease.Release()
	}
	provisional := false
	err := lifecycle.runtimes.RevokeAndRemoveTarget(ctx, targetID)
	if session != nil {
		// Runtime revoke cancels leases and performs bounded helper shutdown before
		// revoking the shared connection generation. A target without a runtime
		// still needs its in-flight connection/bootstrap context cancelled.
		session.connection.Disconnect()
		session.mu.Lock()
		provisional = session.provisional
		session.generation = 0
		session.runtime = nil
		session.artifact = remotessh.HelperArtifact{}
		session.roots = make(map[string]remoteOpenedRoot)
		session.mu.Unlock()
	}
	lifecycle.backends.UnbindTarget(targetID)
	if provisional {
		err = errors.Join(err, lifecycle.RemoveTarget(ctx, targetID))
	}
	return err
}

func (lifecycle *RemoteWorkspaceLifecycle) Close(ctx context.Context) error {
	lifecycle.mu.Lock()
	if lifecycle.closed {
		lifecycle.mu.Unlock()
		return nil
	}
	lifecycle.closed = true
	tasks := lifecycle.tasks
	lifecycle.tasks = make(map[string]remoteLifecycleTask)
	targets := lifecycle.targets
	attempts := lifecycle.connectAttempts
	lifecycle.targets = make(map[string]*remoteLifecycleTarget)
	lifecycle.connectAttempts = make(map[*remotessh.ConnectionSupervisor]struct{})
	lifecycle.pendingRoots = make(map[string]remotePendingRoot)
	lifecycle.pendingByTarget = make(map[string]string)
	lifecycle.mu.Unlock()
	for connection := range attempts {
		connection.Close()
	}
	for threadID, task := range tasks {
		_ = lifecycle.backends.UnbindTask(threadID, task.lease)
		task.lease.Release()
	}
	var result error
	for targetID, session := range targets {
		result = errors.Join(result, lifecycle.runtimes.RemoveTarget(ctx, targetID))
		session.connection.Close()
		lifecycle.backends.UnbindTarget(targetID)
	}
	return result
}

func (lifecycle *RemoteWorkspaceLifecycle) ensureRuntimeLocked(ctx context.Context, targetID string, session *remoteLifecycleTarget, generation uint64, remoteOS, remoteArch, piVersion string) (*remotessh.RuntimeLeaseSupervisor, error) {
	artifact, content, err := lifecycle.artifacts.Select(remoteOS, remoteArch, piVersion)
	if err != nil {
		return nil, err
	}
	if _, err := session.installer.Install(ctx, generation, artifact, content); err != nil {
		return nil, err
	}
	if session.runtime != nil {
		if session.artifact.SHA256 != artifact.SHA256 {
			return nil, errors.New("remote target helper platform changed")
		}
		return session.runtime, nil
	}
	factory, err := remotessh.NewInstalledHelperGenerationFactory(session.installer, artifact)
	if err != nil {
		return nil, err
	}
	runtime, err := remotessh.NewRuntimeLeaseSupervisor(session.connection, factory)
	if err != nil {
		return nil, err
	}
	if err := lifecycle.requireOpen(); err != nil {
		return nil, err
	}
	if err := lifecycle.runtimes.Register(targetID, runtime); err != nil {
		return nil, err
	}
	lifecycle.mu.Lock()
	if lifecycle.closed {
		lifecycle.mu.Unlock()
		_ = lifecycle.runtimes.RemoveTarget(ctx, targetID)
		return nil, errors.New("remote workspace lifecycle is closed")
	}
	session.runtime, session.artifact = runtime, artifact
	lifecycle.mu.Unlock()
	return runtime, nil
}

func (lifecycle *RemoteWorkspaceLifecycle) requireOpen() error {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	if lifecycle.closed {
		return errors.New("remote workspace lifecycle is closed")
	}
	return nil
}

func (lifecycle *RemoteWorkspaceLifecycle) target(target workspace.TargetRecord) (*remoteLifecycleTarget, error) {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	if lifecycle.closed {
		return nil, errors.New("remote workspace lifecycle is closed")
	}
	if session := lifecycle.targets[target.ID]; session != nil {
		return session, nil
	}
	sshTarget, err := remotessh.NewTarget(target.HostAlias)
	if err != nil {
		return nil, err
	}
	connection, err := remotessh.NewConnectionSupervisor(lifecycle.locator, sshTarget)
	if err != nil {
		return nil, err
	}
	session, err := lifecycle.sessionForConnection(connection)
	if err != nil {
		return nil, err
	}
	lifecycle.targets[target.ID] = session
	return session, nil
}

func (lifecycle *RemoteWorkspaceLifecycle) sessionForConnection(connection *remotessh.ConnectionSupervisor) (*remoteLifecycleTarget, error) {
	installer, err := remotessh.NewHelperInstaller(lifecycle.locator, connection)
	if err != nil {
		return nil, err
	}
	return &remoteLifecycleTarget{connection: connection, installer: installer, roots: make(map[string]remoteOpenedRoot)}, nil
}

func (lifecycle *RemoteWorkspaceLifecycle) approvedWorkspace(workspaceID string) (workspace.Record, workspace.TargetRecord, *remoteLifecycleTarget, error) {
	record, err := lifecycle.catalog.ResolveID(strings.TrimSpace(workspaceID))
	if err != nil {
		return workspace.Record{}, workspace.TargetRecord{}, nil, err
	}
	if record.Trust != "approve" || record.Location.Kind != workspace.KindSSH {
		return workspace.Record{}, workspace.TargetRecord{}, nil, ErrRemoteContextChanged
	}
	target, err := lifecycle.catalog.ResolveTarget(record.Location.SSH.TargetID)
	if err != nil || target.HostKey != record.Location.SSH.HostKeyBinding {
		return workspace.Record{}, workspace.TargetRecord{}, nil, ErrRemoteContextChanged
	}
	session, err := lifecycle.target(target)
	return record, target, session, err
}

func (lifecycle *RemoteWorkspaceLifecycle) denyDriftedWorkspace(ctx context.Context, record workspace.Record) {
	ssh := record.Location.SSH
	_, _ = lifecycle.remoteCatalog.AddSSHWorkspace(ctx, workspace.SSHWorkspaceRegistration{
		Name: record.Name, TargetID: ssh.TargetID,
		RequestedRoot: ssh.RequestedRoot, CanonicalRoot: ssh.CanonicalRoot,
		Device: ssh.Device, Inode: ssh.Inode, RemoteOS: ssh.RemoteOS, RemoteArch: ssh.RemoteArch, Trust: "deny",
	})
}

func (lifecycle *RemoteWorkspaceLifecycle) watchTask(threadID string, lease *remotessh.RuntimeLease) {
	<-lease.Context().Done()
	lifecycle.mu.Lock()
	current, ok := lifecycle.tasks[threadID]
	if ok && current.lease == lease {
		delete(lifecycle.tasks, threadID)
	}
	lifecycle.mu.Unlock()
	if ok && current.lease == lease {
		_ = lifecycle.backends.UnbindTask(threadID, lease)
	}
}

func remoteTrustToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func validRemoteTrustToken(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validRemoteDisplayName(value string) bool {
	if value == "" || len([]rune(value)) > 100 || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validRemoteThreadID(value string) bool {
	if value == "" || len(value) > 256 || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character == 0 || unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func remoteTaskOwner(threadID string) string {
	digest := sha256.Sum256([]byte(threadID))
	return "task-" + hex.EncodeToString(digest[:16])
}

func remotePendingRootOwner(token string) string {
	digest := sha256.Sum256([]byte(token))
	return "root-candidate-" + hex.EncodeToString(digest[:16])
}

func matchesCatalogTarget(snapshot remotessh.ConnectionSnapshot, target workspace.TargetRecord) bool {
	return snapshot.State == remotessh.ConnectionReady && snapshot.Generation != 0 &&
		snapshot.Binding.ConfigFingerprint == target.HostKey.ConfigFingerprint &&
		snapshot.Binding.HostKey.Algorithm == target.HostKey.Algorithm &&
		snapshot.Binding.HostKey.SHA256Hash == target.HostKey.SHA256
}
