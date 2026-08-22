package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"pi-desk/internal/remoteprotocol"
	"pi-desk/internal/remotessh"
	terminalruntime "pi-desk/internal/terminal"
)

func TestLiveRemoteRepositoryAndTerminalBackends(t *testing.T) {
	host := os.Getenv("PI_DESK_SSH_LIVE_TARGET")
	rootPath := os.Getenv("PI_DESK_SSH_LIVE_DIRECTORY")
	artifactPath := os.Getenv("PI_DESK_SSH_LIVE_HELPER")
	artifactOS := os.Getenv("PI_DESK_SSH_LIVE_HELPER_OS")
	artifactArch := os.Getenv("PI_DESK_SSH_LIVE_HELPER_ARCH")
	buildIdentity := os.Getenv("PI_DESK_SSH_LIVE_HELPER_BUILD")
	if host == "" || rootPath == "" || artifactPath == "" || artifactOS == "" || artifactArch == "" || buildIdentity == "" {
		t.Skip("set PI_DESK_SSH_LIVE_TARGET, PI_DESK_SSH_LIVE_DIRECTORY and PI_DESK_SSH_LIVE_HELPER* to run the remote Repository fixture")
	}
	content, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	artifact := remotessh.HelperArtifact{
		ProtocolVersion: remoteprotocol.Version, OS: artifactOS, Architecture: artifactArch,
		Size: int64(len(content)), SHA256: hex.EncodeToString(digest[:]), BuildIdentity: buildIdentity,
		PiVersionMin: "0.84.2", PiVersionMax: "0.85.0",
	}
	if err := artifact.Validate(); err != nil {
		t.Fatal(err)
	}
	target, err := remotessh.NewTarget(host)
	if err != nil {
		t.Fatal(err)
	}
	locator := remotessh.NewLocator()
	connection, err := remotessh.NewConnectionSupervisor(locator, target)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ready, err := connection.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	installer, err := remotessh.NewHelperInstaller(locator, connection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := installer.Install(ctx, ready.Generation, artifact, content); err != nil {
		t.Fatal(err)
	}
	factory, err := remotessh.NewInstalledHelperGenerationFactory(installer, artifact)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := remotessh.NewRuntimeLeaseSupervisor(connection, factory)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	parent, err := runtime.OpenRoot(ctx, remotessh.RuntimeRootOpenRequest{
		Generation: ready.Generation, WorkspaceID: "workspace-0123456789abcdef0123456789abcdef", RequestedRoot: rootPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := runtime.AcquireTask(ctx, remotessh.RuntimeLeaseRequest{Root: parent.Capability, OwnerID: "repository-live-setup"})
	if err != nil {
		t.Fatal(err)
	}
	defer task.Release()
	const fixture = ".pi-desk-repository-backend-fixture"
	setup := `rm -rf -- ` + fixture + `; mkdir -- ` + fixture + `; cd -- ` + fixture + `; /usr/bin/git init -q; /usr/bin/git config user.name fixture; /usr/bin/git config user.email fixture@example.invalid; printf 'tracked\n' > tracked.txt; /usr/bin/git add tracked.txt; /usr/bin/git commit -qm initial; printf 'changed\n' > tracked.txt; printf 'untracked\n' > untracked.txt`
	if result, err := runtime.RunBash(ctx, task, setup); err != nil || result.ExitCode != 0 {
		t.Fatalf("setup: result=%#v err=%v", result, err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = runtime.RunBash(cleanupCtx, task, `rm -rf -- `+fixture)
	}()
	gitRoot, err := runtime.OpenRoot(ctx, remotessh.RuntimeRootOpenRequest{
		Generation: ready.Generation, WorkspaceID: "workspace-fedcba9876543210fedcba9876543210", RequestedRoot: path.Join(rootPath, fixture),
	})
	if err != nil {
		t.Fatal(err)
	}
	backend, err := NewRemoteBackend(runtime, gitRoot.Capability)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := backend.Snapshot(ctx)
	if err != nil || !snapshot.Git.IsRepository || len(snapshot.Files) != 2 || len(snapshot.Git.Files) != 2 {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
	diff, err := backend.Diff(ctx, "tracked.txt")
	if err != nil || !strings.Contains(diff.Working, "+changed") {
		t.Fatalf("diff=%#v err=%v", diff, err)
	}
	branches, err := backend.Branches(ctx)
	if err != nil || len(branches.Branches) == 0 || !branches.Branches[0].Current {
		t.Fatalf("branches=%#v err=%v", branches, err)
	}
	preview, err := backend.Preview(ctx, "untracked.txt")
	if err != nil || preview.Content != "untracked" || preview.Size != int64(len("untracked\n")) {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}

	terminalEvents := make(chan terminalruntime.Event, 16)
	terminalManager := terminalruntime.NewRemoteManager(ctx, func(event terminalruntime.Event) { terminalEvents <- event })
	if err := terminalManager.Bind("live-remote-terminal", runtime, task, rootPath); err != nil {
		t.Fatal(err)
	}
	started, err := terminalManager.Start(terminalruntime.StartConfig{ThreadID: "live-remote-terminal", CWD: rootPath, Columns: 80, Rows: 24})
	if err != nil || !started.Running {
		t.Fatalf("terminal start=%#v err=%v", started, err)
	}
	if err := terminalManager.Resize("live-remote-terminal", 100, 30); err != nil {
		t.Fatal(err)
	}
	if err := terminalManager.Write("live-remote-terminal", []byte("printf 'repository-terminal'; exit 6\n")); err != nil {
		t.Fatal(err)
	}
	var terminalOutput []byte
	terminalExit := -1
	for terminalExit < 0 {
		select {
		case event := <-terminalEvents:
			terminalOutput = append(terminalOutput, event.Data...)
			if event.Type == "exit" {
				terminalExit = event.ExitCode
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for remote Terminal backend")
		}
	}
	if terminalExit != 6 || !strings.Contains(string(terminalOutput), "repository-terminal") {
		t.Fatalf("terminal exit=%d output=%q", terminalExit, terminalOutput)
	}
	terminalManager.Shutdown()
}
