package appservice

import (
	"context"
	"os"
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

func TestLiveRemoteWorkspaceServiceSetup(t *testing.T) {
	hostAlias := strings.TrimSpace(os.Getenv("PI_DESK_SSH_LIVE_TARGET"))
	rootPath := strings.TrimSpace(os.Getenv("PI_DESK_SSH_LIVE_DIRECTORY"))
	remoteOS := strings.TrimSpace(os.Getenv("PI_DESK_SSH_LIVE_HELPER_OS"))
	remoteArch := strings.TrimSpace(os.Getenv("PI_DESK_SSH_LIVE_HELPER_ARCH"))
	if hostAlias == "" || rootPath == "" || remoteOS == "" || remoteArch == "" {
		t.Skip("set live SSH target, directory, and helper platform variables")
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
	service, err := NewRemoteWorkspaceService(catalog, lifecycle, piruntime.NewLocator())
	if err != nil {
		t.Fatal(err)
	}

	targetID, err := service.ConnectRemoteTarget(domain.ConnectRemoteTargetRequest{Name: "Live service", HostAlias: hostAlias})
	if err != nil {
		t.Fatalf("service connect target=%q err=%v", targetID, err)
	}
	candidate, err := service.PrepareRemoteRoot(domain.PrepareRemoteRootRequest{
		TargetID: targetID, Name: "Live service root", RequestedRoot: rootPath,
	})
	if err != nil {
		t.Fatalf("service prepare root=%#v err=%v", candidate, err)
	}
	if candidate.CanonicalRoot != rootPath || candidate.HostAlias != hostAlias || candidate.HostKeySHA256 == "" {
		t.Fatalf("unexpected service root candidate: %#v", candidate)
	}
	workspaceRecord, err := service.DecideRemoteRoot(domain.DecideRemoteRootRequest{Token: candidate.Token, Trust: "approve"})
	if err != nil || workspaceRecord.ID == "" {
		t.Fatalf("service approve workspace=%#v err=%v", workspaceRecord, err)
	}
	if err := service.DisconnectRemoteTarget(domain.RemoteTargetRequest{TargetID: targetID}); err != nil {
		t.Fatalf("service disconnect target: %v", err)
	}
	resumed, err := service.ResumeRemoteWorkspace(domain.ResumeRemoteWorkspaceRequest{WorkspaceID: workspaceRecord.ID})
	if err != nil || resumed.ID != workspaceRecord.ID {
		t.Fatalf("service resume workspace=%#v err=%v", resumed, err)
	}
	retry, err := service.PrepareRemoteRoot(domain.PrepareRemoteRootRequest{
		TargetID: targetID, Name: "Live service retry", RequestedRoot: rootPath,
	})
	if err != nil || retry.CanonicalRoot != rootPath {
		t.Fatalf("service retry root=%#v err=%v", retry, err)
	}
}
