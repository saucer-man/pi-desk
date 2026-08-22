package remotessh

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveSSHRemoteSetup(t *testing.T) {
	hostAlias := strings.TrimSpace(os.Getenv("PI_DESK_SSH_LIVE_TARGET"))
	rootPath := strings.TrimSpace(os.Getenv("PI_DESK_SSH_LIVE_DIRECTORY"))
	if hostAlias == "" || rootPath == "" || os.Getenv("PI_DESK_SSH_LIVE_HELPER") == "" || os.Getenv("PI_DESK_SSH_LIVE_HELPER_OS") == "" || os.Getenv("PI_DESK_SSH_LIVE_HELPER_ARCH") == "" || os.Getenv("PI_DESK_SSH_LIVE_HELPER_BUILD") == "" {
		t.Skip("set live SSH target, directory, and helper variables to run remote setup")
	}
	if !validLiveDirectory(rootPath) {
		t.Fatal("PI_DESK_SSH_LIVE_DIRECTORY must be a normalized absolute POSIX path")
	}

	artifact, content := loadLiveHelperArtifact(t)
	target, err := NewTarget(hostAlias)
	if err != nil {
		t.Fatal(err)
	}
	locator := newLiveLocator(t)
	supervisor, err := NewConnectionSupervisor(locator, target)
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ready, err := supervisor.Connect(ctx)
	if err != nil {
		t.Fatalf("remote setup connect failed: %v", err)
	}
	platform, err := supervisor.ProbePlatform(ctx, ready.Generation)
	if err != nil {
		t.Fatalf("remote setup platform probe failed: %v", err)
	}
	if platform.OS != artifact.OS || platform.Arch != artifact.Architecture {
		t.Fatalf("remote setup platform=%#v artifact=%#v", platform, artifact)
	}

	installer, err := NewHelperInstaller(locator, supervisor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := installer.Install(ctx, ready.Generation, artifact, content); err != nil {
		t.Fatalf("remote setup helper install failed: %v", err)
	}
	factory, err := NewInstalledHelperGenerationFactory(installer, artifact)
	if err != nil {
		t.Fatal(err)
	}
	runtimeSupervisor, err := NewRuntimeLeaseSupervisor(supervisor, factory)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeSupervisor.Close(context.Background())
	opened, err := runtimeSupervisor.OpenRoot(ctx, RuntimeRootOpenRequest{
		Generation: ready.Generation, WorkspaceID: "workspace-0123456789abcdef0123456789abcdef", RequestedRoot: rootPath,
	})
	if err != nil {
		t.Fatalf("remote setup root.open failed: %v", err)
	}
	if opened.Identity.CanonicalPath != rootPath || opened.Identity.Device == 0 || opened.Identity.Inode == 0 {
		t.Fatalf("unexpected remote setup root identity: %#v", opened.Identity)
	}
}
