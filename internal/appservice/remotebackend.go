package appservice

import (
	"errors"
	"strings"
	"sync"

	"pi-desk/internal/remotessh"
	"pi-desk/internal/repository"
	"pi-desk/internal/workspace"
)

type remoteWorkspaceBinding struct {
	targetID   string
	generation uint64
	runtime    *remotessh.RuntimeLeaseSupervisor
}

type remoteTaskBinding struct {
	workspaceID string
	generation  uint64
	lease       *remotessh.RuntimeLease
}

// RemoteBackendCoordinator is the host-owned join point between one
// verified SSH root generation and the Repository/Terminal services. It does
// not connect targets, open roots, acquire/release task leases, or start Pi.
type RemoteBackendCoordinator struct {
	catalog    repositoryWorkspaceResolver
	repository *RepositoryService
	terminal   *TerminalService

	mu         sync.Mutex
	workspaces map[string]remoteWorkspaceBinding
	tasks      map[string]remoteTaskBinding
}

func NewRemoteBackendCoordinator(catalog *workspace.Catalog, repositoryService *RepositoryService, terminalService *TerminalService) (*RemoteBackendCoordinator, error) {
	if catalog == nil || repositoryService == nil || terminalService == nil {
		return nil, errors.New("remote backend coordinator dependencies are required")
	}
	return &RemoteBackendCoordinator{
		catalog: catalog, repository: repositoryService, terminal: terminalService,
		workspaces: make(map[string]remoteWorkspaceBinding), tasks: make(map[string]remoteTaskBinding),
	}, nil
}

func (coordinator *RemoteBackendCoordinator) BindWorkspace(workspaceID, targetID string, runtime *remotessh.RuntimeLeaseSupervisor, root *remotessh.RuntimeRootCapability) error {
	backend, err := repository.NewRemoteBackend(runtime, root)
	if err != nil {
		return err
	}
	return coordinator.bindWorkspaceBackend(workspaceID, targetID, runtime, backend)
}

func (coordinator *RemoteBackendCoordinator) bindWorkspaceBackend(workspaceID, targetID string, runtime *remotessh.RuntimeLeaseSupervisor, backend remoteRepositoryBackend) error {
	workspaceID, targetID = strings.TrimSpace(workspaceID), strings.TrimSpace(targetID)
	if runtime == nil || backend == nil || backend.WorkspaceID() != workspaceID || backend.Generation() == 0 {
		return errors.New("remote workspace backend binding is invalid")
	}
	record, err := coordinator.catalog.ResolveID(workspaceID)
	if err != nil {
		return err
	}
	if record.Trust != "approve" || record.Location.Kind != workspace.KindSSH || record.Location.SSH.TargetID != targetID {
		return errors.New("remote workspace backend requires the approved catalog identity")
	}
	if err := backend.ValidateBinding(); err != nil {
		return ErrRemoteRepositoryStale
	}

	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if current, exists := coordinator.workspaces[workspaceID]; exists {
		if current.targetID == targetID && current.generation == backend.Generation() && current.runtime == runtime {
			return nil
		}
		if current.generation >= backend.Generation() {
			return ErrRemoteRepositoryStale
		}
		coordinator.unbindWorkspaceLocked(workspaceID, current.generation)
	}
	if err := coordinator.repository.bindRemoteWorkspace(workspaceID, backend); err != nil {
		return err
	}
	coordinator.workspaces[workspaceID] = remoteWorkspaceBinding{targetID: targetID, generation: backend.Generation(), runtime: runtime}
	return nil
}

func (coordinator *RemoteBackendCoordinator) BindTask(threadID, workspaceID string, lease *remotessh.RuntimeLease) error {
	threadID, workspaceID = strings.TrimSpace(threadID), strings.TrimSpace(workspaceID)
	if threadID == "" || lease == nil || lease.Kind() != remotessh.RuntimeTaskLease || lease.WorkspaceID() != workspaceID {
		return errors.New("remote task backend binding is invalid")
	}
	record, err := coordinator.catalog.ResolveID(workspaceID)
	if err != nil || record.Trust != "approve" || record.Location.Kind != workspace.KindSSH {
		return errors.New("remote task workspace is no longer approved")
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	binding, ok := coordinator.workspaces[workspaceID]
	if !ok || lease.Generation() != binding.generation {
		return ErrRemoteRepositoryStale
	}
	if current, exists := coordinator.tasks[threadID]; exists && (current.workspaceID != workspaceID || current.generation != binding.generation || current.lease != lease) {
		return errors.New("remote task is already bound to another workspace generation")
	}
	if err := coordinator.terminal.bindRemoteThread(threadID, binding.runtime, lease, record.Location.SSH.CanonicalRoot); err != nil {
		return err
	}
	coordinator.tasks[threadID] = remoteTaskBinding{workspaceID: workspaceID, generation: binding.generation, lease: lease}
	return nil
}

func (coordinator *RemoteBackendCoordinator) UnbindTask(threadID string, lease *remotessh.RuntimeLease) error {
	threadID = strings.TrimSpace(threadID)
	coordinator.mu.Lock()
	current, ok := coordinator.tasks[threadID]
	if !ok || lease == nil || current.lease != lease {
		coordinator.mu.Unlock()
		return nil
	}
	delete(coordinator.tasks, threadID)
	err := coordinator.terminal.unbindRemoteThread(threadID)
	coordinator.mu.Unlock()
	return err
}

func (coordinator *RemoteBackendCoordinator) UnbindTarget(targetID string) {
	targetID = strings.TrimSpace(targetID)
	coordinator.mu.Lock()
	for workspaceID, binding := range coordinator.workspaces {
		if binding.targetID == targetID {
			coordinator.unbindWorkspaceLocked(workspaceID, binding.generation)
		}
	}
	coordinator.mu.Unlock()
}

func (coordinator *RemoteBackendCoordinator) unbindWorkspaceLocked(workspaceID string, generation uint64) {
	binding, ok := coordinator.workspaces[workspaceID]
	if !ok || generation != 0 && generation != binding.generation {
		return
	}
	for threadID, task := range coordinator.tasks {
		if task.workspaceID == workspaceID && task.generation == binding.generation {
			_ = coordinator.terminal.unbindRemoteThread(threadID)
			delete(coordinator.tasks, threadID)
		}
	}
	coordinator.repository.unbindRemoteWorkspace(workspaceID, binding.generation)
	delete(coordinator.workspaces, workspaceID)
}
