package appservice

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"pi-desk/internal/remotessh"
	"pi-desk/internal/repository"
	"pi-desk/internal/workspace"
)

func newTestRemoteCatalogCoordinator(t *testing.T, catalog *workspace.Catalog, registry *remotessh.RuntimeRegistry) *RemoteCatalogCoordinator {
	t.Helper()
	backends, err := NewRemoteBackendCoordinator(catalog, NewRepositoryService(catalog, repository.New()), NewTerminalService(catalog))
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewRemoteCatalogCoordinator(catalog, registry, backends)
	if err != nil {
		t.Fatal(err)
	}
	return coordinator
}

func TestRemoteCatalogCoordinatorRoutesSecurityMutationsThroughRegistry(t *testing.T) {
	catalog := workspace.NewCatalog(filepath.Join(t.TempDir(), "state.json"))
	registry := remotessh.NewRuntimeRegistry()
	coordinator := newTestRemoteCatalogCoordinator(t, catalog, registry)
	targetRegistration := workspace.TargetRegistration{
		Name: "Build host", HostAlias: "build-prod", ConfigFingerprint: strings.Repeat("a", 64),
		HostKeyAlgorithm: "ssh-ed25519", HostKeySHA256: "SHA256:" + strings.Repeat("A", 43),
	}
	target, err := catalog.RegisterTarget(targetRegistration)
	if err != nil {
		t.Fatal(err)
	}
	registration := workspace.SSHWorkspaceRegistration{
		Name: "Remote", TargetID: target.ID, RequestedRoot: "/srv/repository", CanonicalRoot: "/srv/repository",
		Device: 7, Inode: 11, RemoteOS: "linux", RemoteArch: "amd64", Trust: "approve",
	}
	record, err := coordinator.AddSSHWorkspace(context.Background(), registration)
	if err != nil {
		t.Fatal(err)
	}
	registration.Trust = "deny"
	if _, err := coordinator.AddSSHWorkspace(context.Background(), registration); err != nil {
		t.Fatal(err)
	}
	records, _ := catalog.List()
	if len(records) != 1 || records[0].Trust != "deny" {
		t.Fatalf("records = %#v", records)
	}
	if err := coordinator.RemoveWorkspace(context.Background(), record.ID); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.RemoveTarget(context.Background(), target.ID); err != nil {
		t.Fatal(err)
	}
	if targets, _ := catalog.ListTargets(); len(targets) != 0 {
		t.Fatalf("targets = %#v", targets)
	}
}
