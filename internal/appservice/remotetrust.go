package appservice

import (
	"context"
	"errors"

	"pi-desk/internal/remotessh"
	"pi-desk/internal/workspace"
)

// RemoteCatalogCoordinator is the host-only mutation path for SSH
// catalog records. It revokes live capabilities before unbinding backends and
// persisting deny, workspace removal, or target removal.
type RemoteCatalogCoordinator struct {
	catalog  *workspace.Catalog
	runtimes *remotessh.RuntimeRegistry
	backends *RemoteBackendCoordinator
}

func NewRemoteCatalogCoordinator(catalog *workspace.Catalog, runtimes *remotessh.RuntimeRegistry, backends *RemoteBackendCoordinator) (*RemoteCatalogCoordinator, error) {
	if catalog == nil || runtimes == nil || backends == nil {
		return nil, errors.New("remote catalog coordinator dependencies are required")
	}
	return &RemoteCatalogCoordinator{catalog: catalog, runtimes: runtimes, backends: backends}, nil
}

func (coordinator *RemoteCatalogCoordinator) AddSSHWorkspace(ctx context.Context, registration workspace.SSHWorkspaceRegistration) (workspace.Record, error) {
	return coordinator.catalog.AddSSHWorkspaceAfter(registration, func(targetID string) error {
		coordinator.revokeTarget(ctx, targetID, false)
		return nil
	})
}

func (coordinator *RemoteCatalogCoordinator) RemoveWorkspace(ctx context.Context, workspaceID string) error {
	return coordinator.catalog.RemoveAfter(workspaceID, func(record workspace.Record) error {
		if record.Location.Kind == workspace.KindSSH {
			coordinator.revokeTarget(ctx, record.Location.SSH.TargetID, false)
		}
		return nil
	})
}

func (coordinator *RemoteCatalogCoordinator) RemoveTarget(ctx context.Context, targetID string) error {
	return coordinator.catalog.RemoveTargetAfter(targetID, func() error {
		coordinator.revokeTarget(ctx, targetID, true)
		return nil
	})
}

func (coordinator *RemoteCatalogCoordinator) revokeTarget(ctx context.Context, targetID string, remove bool) {
	if remove {
		_ = coordinator.runtimes.RemoveTarget(ctx, targetID)
	} else {
		_ = coordinator.runtimes.RevokeTarget(ctx, targetID)
	}
	// Runtime revoke cancels leases first; only then may stale UI backends be
	// detached. A bounded helper shutdown error cannot preserve bindings or
	// roll back an already validated catalog mutation.
	coordinator.backends.UnbindTarget(targetID)
}
