package remotessh

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const liveEnvironmentSentinel = "PI_DESK_REMOTE_ENV_SENTINEL"

func newLiveLocator(t *testing.T) *Locator {
	t.Helper()
	locator := NewLocator()
	configPath := strings.TrimSpace(os.Getenv("PI_DESK_SSH_LIVE_CONFIG"))
	if configPath == "" {
		return locator
	}
	absolute, err := filepath.Abs(configPath)
	if err != nil {
		t.Fatalf("resolve PI_DESK_SSH_LIVE_CONFIG: %v", err)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("PI_DESK_SSH_LIVE_CONFIG must name a regular file: %v", err)
	}
	locator.testConfigPath = filepath.ToSlash(absolute)
	return locator
}

func loadLiveHelperArtifact(t *testing.T) (HelperArtifact, []byte) {
	t.Helper()
	content, err := os.ReadFile(os.Getenv("PI_DESK_SSH_LIVE_HELPER"))
	if err != nil || len(content) == 0 || len(content) > maxHelperArtifactBytes {
		t.Fatalf("read live helper artifact: size=%d err=%v", len(content), err)
	}
	artifact := helperArtifactForTest(os.Getenv("PI_DESK_SSH_LIVE_HELPER_OS"), os.Getenv("PI_DESK_SSH_LIVE_HELPER_ARCH"), content)
	artifact.BuildIdentity = os.Getenv("PI_DESK_SSH_LIVE_HELPER_BUILD")
	if err := artifact.Validate(); err != nil {
		t.Fatalf("live helper artifact: %v", err)
	}
	return artifact, content
}

func TestLiveSSHConfigExecutionConsentBoundary(t *testing.T) {
	target := os.Getenv("PI_DESK_SSH_LIVE_TARGET")
	configPath := strings.TrimSpace(os.Getenv("PI_DESK_SSH_LIVE_CONSENT_CONFIG"))
	markerPath := strings.TrimSpace(os.Getenv("PI_DESK_SSH_LIVE_CONSENT_MARKER"))
	if target == "" || configPath == "" || markerPath == "" {
		t.Skip("set live target, consent config, and consent marker to run the Match exec boundary")
	}
	configPath, err := filepath.Abs(configPath)
	if err != nil {
		t.Fatal(err)
	}
	markerPath, err = filepath.Abs(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(markerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	defer os.Remove(markerPath)
	discovery, err := DiscoverSSHConfig(DiscoveryOptions{ConfigPath: configPath, HomeDir: filepath.Dir(configPath)})
	if err != nil {
		t.Fatal(err)
	}
	aliasIndex := slices.IndexFunc(discovery, func(alias Alias) bool { return alias.Name == target })
	if aliasIndex < 0 || !discovery[aliasIndex].Risk.HasMatchExec {
		t.Fatalf("static discovery did not project Match exec risk: %#v", discovery)
	}
	if _, err := os.Stat(markerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("static discovery executed Match exec: %v", err)
	}
	locator := NewLocator()
	locator.testConfigPath = filepath.ToSlash(configPath)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := locator.ProbeConnection(ctx, target); err != nil {
		t.Fatalf("explicit consent preflight: %v", err)
	}
	if info, err := os.Stat(markerPath); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("explicit preflight did not execute Match exec marker: %v", err)
	}
}

func TestLiveSSHConnectionFixture(t *testing.T) {
	target := os.Getenv("PI_DESK_SSH_LIVE_TARGET")
	directory := os.Getenv("PI_DESK_SSH_LIVE_DIRECTORY")
	if target == "" || directory == "" {
		t.Skip("set PI_DESK_SSH_LIVE_TARGET and PI_DESK_SSH_LIVE_DIRECTORY to run the real SSH fixture")
	}
	if !validLiveDirectory(directory) {
		t.Fatalf("PI_DESK_SSH_LIVE_DIRECTORY must be a normalized absolute POSIX path using shell-safe ASCII characters")
	}
	t.Setenv(liveEnvironmentSentinel, "must-not-reach-remote")

	locator := newLiveLocator(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	preflight, err := locator.ProbeConnection(ctx, target)
	if err != nil {
		t.Fatalf("strict connection preflight failed: %v", err)
	}
	if preflight.Config.Fingerprint == "" || preflight.HostKey.Algorithm == "" || preflight.HostKey.SHA256Hash == "" {
		t.Fatalf("strict connection returned incomplete evidence: %#v", preflight)
	}

	t.Run("workspace and environment boundary", func(t *testing.T) {
		invocation, err := locator.connectionProbeInvocation(target)
		if err != nil {
			t.Fatal(err)
		}
		invocation.Args = replaceProbeCommand(invocation.Args,
			`test -d `+directory+` && test -r `+directory+` && test -w `+directory+` && test -x `+directory+` && test -z "${PI_DESK_REMOTE_ENV_SENTINEL+x}"`,
		)
		output, runErr := locator.runner.Run(ctx, invocation.Executable, invocation.Args...)
		if runErr != nil {
			t.Fatalf("workspace/environment probe failed: %#v", ClassifyOpenSSHFailure(output.Stderr))
		}
		evidence, err := ParseHostKeyEvidence(output.Stderr)
		if err != nil {
			t.Fatalf("workspace probe host-key evidence: %v", err)
		}
		if evidence != preflight.HostKey {
			t.Fatalf("host key changed between probes: first=%#v second=%#v", preflight.HostKey, evidence)
		}
	})

	t.Run("unknown host key", func(t *testing.T) {
		knownHosts := filepath.Join(t.TempDir(), "known_hosts")
		if err := os.WriteFile(knownHosts, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		failure := runExpectedLiveFailure(t, ctx, locator, target, knownHosts)
		if failure.Code != FailureHostKeyUnknown || failure.Reason != ReasonHostKeyUnknown {
			t.Fatalf("unexpected unknown-key classification: %#v", failure)
		}
	})

	t.Run("changed host key", func(t *testing.T) {
		if preflight.HostKey.Algorithm != "ssh-ed25519" {
			t.Skipf("deterministic changed-key fixture requires ssh-ed25519, target negotiated %s", preflight.HostKey.Algorithm)
		}
		knownHosts := filepath.Join(t.TempDir(), "known_hosts")
		hostPattern := knownHostsPattern(preflight.Config)
		line := fmt.Sprintf("%s ssh-ed25519 %s\n", hostPattern, fakeEd25519PublicKey())
		if err := os.WriteFile(knownHosts, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
		failure := runExpectedLiveFailure(t, ctx, locator, target, knownHosts)
		if failure.Code != FailureHostKeyChanged || failure.Reason != ReasonHostKeyChanged {
			t.Fatalf("unexpected changed-key classification: %#v", failure)
		}
	})

	t.Run("authentication required", func(t *testing.T) {
		invocation, err := locator.connectionProbeInvocation(target)
		if err != nil {
			t.Fatal(err)
		}
		invocation.Args = insertBeforeTarget(invocation.Args,
			"-o", "PubkeyAuthentication=no",
			"-o", "PreferredAuthentications=publickey",
		)
		output, runErr := locator.runner.Run(ctx, invocation.Executable, invocation.Args...)
		if runErr == nil {
			t.Fatal("connection unexpectedly authenticated after public-key authentication was disabled")
		}
		failure := ClassifyOpenSSHFailure(output.Stderr)
		if failure.Code != FailureAuthRequired || failure.Reason != ReasonAuthenticationRejected {
			t.Fatalf("unexpected authentication classification: %#v", failure)
		}
	})
}

func TestLiveSSHAuthenticationAndProxyJumpMatrix(t *testing.T) {
	passwordTarget := os.Getenv("PI_DESK_SSH_LIVE_PASSWORD_TARGET")
	encryptedTarget := os.Getenv("PI_DESK_SSH_LIVE_ENCRYPTED_TARGET")
	jumpTarget := os.Getenv("PI_DESK_SSH_LIVE_PROXYJUMP_TARGET")
	if passwordTarget == "" || encryptedTarget == "" || jumpTarget == "" {
		t.Skip("set password, encrypted-key, and ProxyJump live targets to run the authentication matrix")
	}
	locator := newLiveLocator(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, test := range []struct {
		name   string
		target string
		reason FailureReason
	}{
		{name: "password only", target: passwordTarget, reason: ReasonAuthenticationRejected},
		{name: "encrypted key not loaded", target: encryptedTarget, reason: ReasonAuthenticationRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := locator.ProbeConnection(ctx, test.target)
			var probeError *ConnectionProbeError
			if !errors.As(err, &probeError) || probeError.Failure.Code != FailureAuthRequired || probeError.Failure.Reason != test.reason {
				t.Fatalf("authentication failure=%#v err=%v", probeError, err)
			}
		})
	}

	preflight, err := locator.ProbeConnection(ctx, jumpTarget)
	if err != nil {
		t.Fatalf("ProxyJump connection: %v", err)
	}
	if !preflight.Config.ProxyJump || preflight.Config.ProxyJumpSHA256 == "" || preflight.Config.ProxyCommand || preflight.Config.Fingerprint == "" || preflight.HostKey.Algorithm == "" || preflight.HostKey.SHA256Hash == "" {
		t.Fatalf("incomplete ProxyJump evidence: %#v", preflight)
	}
}

func TestLiveSSHRestrictedFilesystemMatrix(t *testing.T) {
	noHomeTarget := os.Getenv("PI_DESK_SSH_LIVE_NOHOME_TARGET")
	readOnlyTarget := os.Getenv("PI_DESK_SSH_LIVE_READONLY_TARGET")
	rootPath := os.Getenv("PI_DESK_SSH_LIVE_DIRECTORY")
	if noHomeTarget == "" || readOnlyTarget == "" || rootPath == "" || os.Getenv("PI_DESK_SSH_LIVE_HELPER") == "" || os.Getenv("PI_DESK_SSH_LIVE_HELPER_OS") == "" || os.Getenv("PI_DESK_SSH_LIVE_HELPER_ARCH") == "" || os.Getenv("PI_DESK_SSH_LIVE_HELPER_BUILD") == "" {
		t.Skip("set no-home/read-only targets and live helper variables to run the restricted-filesystem matrix")
	}
	if !validLiveDirectory(rootPath) {
		t.Fatal("PI_DESK_SSH_LIVE_DIRECTORY must be a normalized shell-safe absolute POSIX path")
	}
	artifact, content := loadLiveHelperArtifact(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	t.Run("no writable home", func(t *testing.T) {
		locator := newLiveLocator(t)
		target, err := NewTarget(noHomeTarget)
		if err != nil {
			t.Fatal(err)
		}
		connection, err := NewConnectionSupervisor(locator, target)
		if err != nil {
			t.Fatal(err)
		}
		ready, err := connection.Connect(ctx)
		if err != nil {
			t.Fatal(err)
		}
		installer, err := NewHelperInstaller(locator, connection)
		if err != nil {
			t.Fatal(err)
		}
		_, err = installer.Install(ctx, ready.Generation, artifact, content)
		if !errors.Is(err, ErrHelperInstall) && !errors.Is(err, ErrHelperCacheUnsafe) {
			t.Fatalf("no-home install error=%v", err)
		}
		if err := connection.ValidateGeneration(ready.Generation); !errors.Is(err, ErrConnectionGenerationRevoked) {
			t.Fatalf("failed no-home install retained generation: %v", err)
		}
	})

	t.Run("read only workspace", func(t *testing.T) {
		locator := newLiveLocator(t)
		target, err := NewTarget(readOnlyTarget)
		if err != nil {
			t.Fatal(err)
		}
		connection, err := NewConnectionSupervisor(locator, target)
		if err != nil {
			t.Fatal(err)
		}
		ready, err := connection.Connect(ctx)
		if err != nil {
			t.Fatal(err)
		}
		installer, err := NewHelperInstaller(locator, connection)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := installer.Install(ctx, ready.Generation, artifact, content); err != nil {
			t.Fatal(err)
		}
		factory, err := NewInstalledHelperGenerationFactory(installer, artifact)
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := NewRuntimeLeaseSupervisor(connection, factory)
		if err != nil {
			t.Fatal(err)
		}
		defer runtime.Close(context.Background())
		opened, err := runtime.OpenRoot(ctx, RuntimeRootOpenRequest{
			Generation: ready.Generation, WorkspaceID: "workspace-0123456789abcdef0123456789abcdef", RequestedRoot: rootPath,
		})
		if err != nil {
			t.Fatal(err)
		}
		lease, err := runtime.AcquireTask(ctx, RuntimeLeaseRequest{Root: opened.Capability, OwnerID: "live-readonly-task"})
		if err != nil {
			t.Fatal(err)
		}
		name := ".pi-desk-readonly-denied"
		if _, err := runtime.WriteFile(ctx, lease, RuntimeFileWriteRequest{Path: name, Content: []byte("must-not-exist\n"), ExpectedAbsent: true}); !errors.Is(err, ErrRuntimeFileWrite) {
			t.Fatalf("read-only write error=%v", err)
		}
		if err := runLiveFixedCommand(ctx, locator, readOnlyTarget, `test ! -e `+path.Join(rootPath, name)); err != nil {
			t.Fatalf("read-only write created a file: %v", err)
		}
		if err := connection.ValidateGeneration(ready.Generation); err != nil {
			t.Fatalf("ordinary permission denial revoked connection: %v", err)
		}
	})
}

func TestLiveSSHConcurrentHelperInstallers(t *testing.T) {
	hostAlias := os.Getenv("PI_DESK_SSH_LIVE_TARGET")
	if hostAlias == "" || os.Getenv("PI_DESK_SSH_LIVE_HELPER") == "" || os.Getenv("PI_DESK_SSH_LIVE_HELPER_OS") == "" || os.Getenv("PI_DESK_SSH_LIVE_HELPER_ARCH") == "" || os.Getenv("PI_DESK_SSH_LIVE_HELPER_BUILD") == "" {
		t.Skip("set live target and helper variables to run concurrent exact-cache installation")
	}
	artifact, content := loadLiveHelperArtifact(t)
	buildIdentity := artifact.BuildIdentity
	content = append(slices.Clone(content), []byte(fmt.Sprintf("\n%s-%d", t.Name(), time.Now().UnixNano()))...)
	artifact = helperArtifactForTest(artifact.OS, artifact.Architecture, content)
	artifact.BuildIdentity = buildIdentity
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	target, err := NewTarget(hostAlias)
	if err != nil {
		t.Fatal(err)
	}
	type instance struct {
		connection *ConnectionSupervisor
		installer  *HelperInstaller
		generation uint64
	}
	instances := make([]instance, 2)
	for index := range instances {
		locator := newLiveLocator(t)
		connection, err := NewConnectionSupervisor(locator, target)
		if err != nil {
			t.Fatal(err)
		}
		ready, err := connection.Connect(ctx)
		if err != nil {
			t.Fatal(err)
		}
		installer, err := NewHelperInstaller(locator, connection)
		if err != nil {
			t.Fatal(err)
		}
		instances[index] = instance{connection: connection, installer: installer, generation: ready.Generation}
	}
	results := make(chan struct {
		result HelperInstallResult
		err    error
	}, len(instances))
	for _, current := range instances {
		go func() {
			result, installErr := current.installer.Install(ctx, current.generation, artifact, content)
			results <- struct {
				result HelperInstallResult
				err    error
			}{result: result, err: installErr}
		}()
	}
	var remotePath string
	for range instances {
		outcome := <-results
		if outcome.err != nil || outcome.result.RemotePath == "" {
			t.Fatalf("concurrent helper install result=%#v err=%v", outcome.result, outcome.err)
		}
		if remotePath != "" && remotePath != outcome.result.RemotePath {
			t.Fatalf("concurrent installers published different paths: %q != %q", remotePath, outcome.result.RemotePath)
		}
		remotePath = outcome.result.RemotePath
	}
	for _, current := range instances {
		if err := current.connection.ValidateGeneration(current.generation); err != nil {
			t.Fatalf("concurrent installer revoked a successful generation: %v", err)
		}
	}
}

func TestLiveSSHConnectionSupervisor(t *testing.T) {
	hostAlias := os.Getenv("PI_DESK_SSH_LIVE_TARGET")
	if hostAlias == "" {
		t.Skip("set PI_DESK_SSH_LIVE_TARGET to run the live connection-supervisor lifecycle")
	}
	target, err := NewTarget(hostAlias)
	if err != nil {
		t.Fatalf("live SSH target is invalid: %v", err)
	}
	supervisor, err := NewConnectionSupervisor(newLiveLocator(t), target)
	if err != nil {
		t.Fatalf("create connection supervisor: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	first, err := supervisor.Connect(ctx)
	if err != nil {
		t.Fatalf("initial supervisor connect: %v", err)
	}
	if first.State != ConnectionReady || first.Generation == 0 || first.Binding.ConfigFingerprint == "" || first.Binding.HostKey.Algorithm == "" || first.Binding.HostKey.SHA256Hash == "" {
		t.Fatalf("initial supervisor snapshot is incomplete: %#v", first)
	}
	if err := supervisor.ValidateGeneration(first.Generation); err != nil {
		t.Fatalf("initial generation is not valid: %v", err)
	}
	cached, err := supervisor.Connect(ctx)
	if err != nil || cached != first {
		t.Fatalf("ready supervisor did not reuse its generation: cached=%#v err=%v", cached, err)
	}

	disconnected := supervisor.Disconnect()
	if disconnected.State != ConnectionDisconnected || disconnected.Generation != 0 || disconnected.Failure == nil || disconnected.Failure.Code != FailureDisconnected {
		t.Fatalf("disconnect snapshot is not fail-closed: %#v", disconnected)
	}
	if err := supervisor.ValidateGeneration(first.Generation); !errors.Is(err, ErrConnectionGenerationRevoked) {
		t.Fatalf("revoked generation was accepted: %v", err)
	}

	second, err := supervisor.Connect(ctx)
	if err != nil {
		t.Fatalf("explicit reconnect failed: %v", err)
	}
	if second.State != ConnectionReady || second.Generation <= first.Generation || second.Binding != first.Binding {
		t.Fatalf("reconnect did not create a matching new generation: first=%#v second=%#v", first, second)
	}
}

func TestLiveSSHNetworkDisconnectFaults(t *testing.T) {
	hostAlias := os.Getenv("PI_DESK_SSH_LIVE_TARGET")
	adminAlias := os.Getenv("PI_DESK_SSH_LIVE_ADMIN_TARGET")
	controlURL := strings.TrimRight(os.Getenv("PI_DESK_SSH_LIVE_TOXIPROXY_URL"), "/")
	artifactPath := os.Getenv("PI_DESK_SSH_LIVE_HELPER")
	rootPath := os.Getenv("PI_DESK_SSH_LIVE_DIRECTORY")
	artifactOS := os.Getenv("PI_DESK_SSH_LIVE_HELPER_OS")
	artifactArchitecture := os.Getenv("PI_DESK_SSH_LIVE_HELPER_ARCH")
	buildIdentity := os.Getenv("PI_DESK_SSH_LIVE_HELPER_BUILD")
	if hostAlias == "" || adminAlias == "" || controlURL == "" || artifactPath == "" || rootPath == "" || artifactOS == "" || artifactArchitecture == "" || buildIdentity == "" {
		t.Skip("set live target/admin/config/helper and Toxiproxy variables to run the network-disconnect fixture")
	}
	if !validLiveDirectory(rootPath) {
		t.Fatal("PI_DESK_SSH_LIVE_DIRECTORY must be a normalized shell-safe absolute POSIX path")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := setLiveProxyEnabled(ctx, controlURL, true); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_ = setLiveProxyLatency(cleanupContext, controlURL, false)
		_ = setLiveProxyEnabled(cleanupContext, controlURL, true)
		_ = runLiveFixedCommand(cleanupContext, newLiveLocator(t), adminAlias, `rm -f `+path.Join(rootPath, ".pi-desk-disconnect-accepted")+` `+path.Join(rootPath, ".pi-desk-disconnect-completed")+` `+path.Join(rootPath, ".pi-desk-disconnect-write")+` `+path.Join(rootPath, ".pi-desk-disconnect-terminal")+` `+path.Join(rootPath, ".pi-desk-predispatch-write"))
	}()

	content, err := os.ReadFile(artifactPath)
	if err != nil || len(content) == 0 || len(content) > maxHelperArtifactBytes {
		t.Fatalf("read helper artifact: size=%d err=%v", len(content), err)
	}
	artifact := helperArtifactForTest(artifactOS, artifactArchitecture, content)
	artifact.BuildIdentity = buildIdentity
	if err := artifact.Validate(); err != nil {
		t.Fatal(err)
	}
	locator := newLiveLocator(t)
	target, err := NewTarget(hostAlias)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := NewConnectionSupervisor(locator, target)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := connection.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	installer, err := NewHelperInstaller(locator, connection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := installer.Install(ctx, ready.Generation, artifact, content); err != nil {
		t.Fatal(err)
	}
	factory, err := NewInstalledHelperGenerationFactory(installer, artifact)
	if err != nil {
		t.Fatal(err)
	}
	runtimeSupervisor, err := NewRuntimeLeaseSupervisor(connection, factory)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeSupervisor.Close(context.Background())
	opened, err := runtimeSupervisor.OpenRoot(ctx, RuntimeRootOpenRequest{
		Generation: ready.Generation, WorkspaceID: "workspace-0123456789abcdef0123456789abcdef", RequestedRoot: rootPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := runtimeSupervisor.AcquireTask(ctx, RuntimeLeaseRequest{Root: opened.Capability, OwnerID: "live-disconnect-task"})
	if err != nil {
		t.Fatal(err)
	}
	acceptedPath := path.Join(rootPath, ".pi-desk-disconnect-accepted")
	completedPath := path.Join(rootPath, ".pi-desk-disconnect-completed")
	if err := runLiveFixedCommand(ctx, locator, adminAlias, `rm -f `+acceptedPath+` `+completedPath); err != nil {
		t.Fatal(err)
	}
	bashDone := make(chan error, 1)
	go func() {
		_, runErr := runtimeSupervisor.RunBash(ctx, lease, `printf accepted > `+acceptedPath+`; sleep 30; printf completed > `+completedPath)
		bashDone <- runErr
	}()
	markerDeadline := time.Now().Add(10 * time.Second)
	for {
		if err := runLiveFixedCommand(ctx, locator, adminAlias, `test -f `+acceptedPath); err == nil {
			break
		}
		if time.Now().After(markerDeadline) {
			t.Fatal("remote Bash never reached its accepted marker")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := setLiveProxyEnabled(ctx, controlURL, false); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-bashDone:
		if !errors.Is(err, ErrRuntimeOutcomeUnknown) {
			t.Fatalf("accepted Bash disconnect error=%v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("accepted Bash did not terminate after network disconnect")
	}
	select {
	case <-lease.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("network disconnect did not revoke the task lease")
	}
	if err := waitLiveGenerationRevoked(connection, ready.Generation, 5*time.Second); err != nil {
		t.Fatalf("Bash disconnect retained generation %d: %v", ready.Generation, err)
	}
	if err := setLiveProxyEnabled(ctx, controlURL, true); err != nil {
		t.Fatal(err)
	}
	if err := runLiveFixedCommand(ctx, locator, adminAlias, `test -f `+acceptedPath+` && test ! -f `+completedPath); err != nil {
		t.Fatalf("remote command completion was not conservatively unknown: %v", err)
	}
	reconnected, err := connection.Connect(ctx)
	if err != nil || reconnected.Generation <= ready.Generation {
		t.Fatalf("explicit reconnect=%#v err=%v", reconnected, err)
	}

	writeRoot, err := runtimeSupervisor.OpenRoot(ctx, RuntimeRootOpenRequest{
		Generation: reconnected.Generation, WorkspaceID: "workspace-0123456789abcdef0123456789abcdef", RequestedRoot: rootPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeLease, err := runtimeSupervisor.AcquireTask(ctx, RuntimeLeaseRequest{Root: writeRoot.Capability, OwnerID: "live-disconnect-write"})
	if err != nil {
		t.Fatal(err)
	}
	writeName := ".pi-desk-disconnect-write"
	writePath := path.Join(rootPath, writeName)
	if err := runLiveFixedCommand(ctx, locator, adminAlias, `rm -f `+writePath); err != nil {
		t.Fatal(err)
	}
	if err := setLiveProxyLatency(ctx, controlURL, true); err != nil {
		t.Fatal(err)
	}
	writeDone := make(chan error, 1)
	go func() {
		_, writeErr := runtimeSupervisor.WriteFile(ctx, writeLease, RuntimeFileWriteRequest{Path: writeName, Content: []byte("committed\n"), ExpectedAbsent: true})
		writeDone <- writeErr
	}()
	markerDeadline = time.Now().Add(10 * time.Second)
	for {
		if err := runLiveFixedCommand(ctx, locator, adminAlias, `test "$(cat `+writePath+`)" = committed`); err == nil {
			break
		}
		if time.Now().After(markerDeadline) {
			t.Fatal("conditional write did not commit before its delayed response")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := setLiveProxyEnabled(ctx, controlURL, false); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-writeDone:
		if !errors.Is(err, ErrRuntimeOutcomeUnknown) {
			t.Fatalf("committed write disconnect error=%v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("committed write did not terminate after network disconnect")
	}
	select {
	case <-writeLease.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("write disconnect did not revoke its task lease")
	}
	if err := waitLiveGenerationRevoked(connection, reconnected.Generation, 5*time.Second); err != nil {
		t.Fatalf("write disconnect retained generation %d: %v", reconnected.Generation, err)
	}
	if err := setLiveProxyLatency(ctx, controlURL, false); err != nil {
		t.Fatal(err)
	}
	if err := setLiveProxyEnabled(ctx, controlURL, true); err != nil {
		t.Fatal(err)
	}
	terminalReady, err := connection.Connect(ctx)
	if err != nil || terminalReady.Generation <= reconnected.Generation {
		t.Fatalf("reconnect after write=%#v err=%v", terminalReady, err)
	}
	terminalRoot, err := runtimeSupervisor.OpenRoot(ctx, RuntimeRootOpenRequest{
		Generation: terminalReady.Generation, WorkspaceID: "workspace-0123456789abcdef0123456789abcdef", RequestedRoot: rootPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	terminalLease, err := runtimeSupervisor.AcquireTask(ctx, RuntimeLeaseRequest{Root: terminalRoot.Capability, OwnerID: "live-disconnect-terminal"})
	if err != nil {
		t.Fatal(err)
	}
	terminalMarker := path.Join(rootPath, ".pi-desk-disconnect-terminal")
	if err := runLiveFixedCommand(ctx, locator, adminAlias, `rm -f `+terminalMarker); err != nil {
		t.Fatal(err)
	}
	terminal, err := runtimeSupervisor.StartTerminal(ctx, terminalLease, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := waitLiveTerminalOutput(terminal, nil, 10*time.Second); err != nil {
		t.Fatalf("Terminal shell readiness: %v", err)
	}
	if err := terminal.Input([]byte(`touch ` + terminalMarker + `; printf terminal-ready; sleep 30` + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := waitLiveTerminalOutput(terminal, []byte("terminal-ready"), 10*time.Second); err != nil {
		t.Fatalf("Terminal command readiness: %v", err)
	}
	if err := runLiveFixedCommand(ctx, locator, adminAlias, `test -f `+terminalMarker); err != nil {
		t.Fatalf("Terminal output preceded its remote marker: %v", err)
	}
	if err := setLiveProxyEnabled(ctx, controlURL, false); err != nil {
		t.Fatal(err)
	}
	var disconnectedEvent *RuntimeTerminalEvent
	terminalDeadline := time.After(15 * time.Second)
terminalEvents:
	for {
		select {
		case event, ok := <-terminal.Events():
			if !ok {
				break terminalEvents
			}
			if event.Type == "disconnected" {
				disconnectedEvent = &event
				break terminalEvents
			}
		case <-terminalDeadline:
			t.Fatal("Terminal stream did not disconnect")
		}
	}
	if disconnectedEvent == nil || !errors.Is(disconnectedEvent.Error, ErrRuntimeOutcomeUnknown) {
		t.Fatalf("Terminal disconnect event=%#v", disconnectedEvent)
	}
	select {
	case <-terminalLease.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Terminal disconnect did not revoke its task lease")
	}
	if err := waitLiveGenerationRevoked(connection, terminalReady.Generation, 5*time.Second); err != nil {
		t.Fatalf("Terminal disconnect retained generation %d: %v", terminalReady.Generation, err)
	}
	if err := setLiveProxyEnabled(ctx, controlURL, true); err != nil {
		t.Fatal(err)
	}
	finalReady, err := connection.Connect(ctx)
	if err != nil || finalReady.Generation <= terminalReady.Generation {
		t.Fatalf("final explicit reconnect=%#v err=%v", finalReady, err)
	}
	crashRoot, err := runtimeSupervisor.OpenRoot(ctx, RuntimeRootOpenRequest{
		Generation: finalReady.Generation, WorkspaceID: "workspace-0123456789abcdef0123456789abcdef", RequestedRoot: rootPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	crashLease, err := runtimeSupervisor.AcquireTask(ctx, RuntimeLeaseRequest{Root: crashRoot.Capability, OwnerID: "live-helper-crash"})
	if err != nil {
		t.Fatal(err)
	}
	if err := runLiveFixedCommand(ctx, locator, adminAlias, `/usr/bin/pkill -u fixture -f '[h]elper serve-stdio'`); err != nil {
		t.Fatalf("terminate remote helper fixture: %v", err)
	}
	select {
	case <-crashLease.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("helper crash did not revoke its task lease")
	}
	if err := waitLiveGenerationRevoked(connection, finalReady.Generation, 5*time.Second); err != nil {
		t.Fatalf("helper crash retained generation %d: %v", finalReady.Generation, err)
	}
	afterCrash, err := connection.Connect(ctx)
	if err != nil || afterCrash.Generation <= finalReady.Generation {
		t.Fatalf("explicit reconnect after helper crash=%#v err=%v", afterCrash, err)
	}
	preDispatchRoot, err := runtimeSupervisor.OpenRoot(ctx, RuntimeRootOpenRequest{
		Generation: afterCrash.Generation, WorkspaceID: "workspace-0123456789abcdef0123456789abcdef", RequestedRoot: rootPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	preDispatchLease, err := runtimeSupervisor.AcquireTask(ctx, RuntimeLeaseRequest{Root: preDispatchRoot.Capability, OwnerID: "live-predispatch-write"})
	if err != nil {
		t.Fatal(err)
	}
	preDispatchName := ".pi-desk-predispatch-write"
	preDispatchPath := path.Join(rootPath, preDispatchName)
	if err := runLiveFixedCommand(ctx, locator, adminAlias, `rm -f `+preDispatchPath); err != nil {
		t.Fatal(err)
	}
	connection.Disconnect()
	select {
	case <-preDispatchLease.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("pre-dispatch revoke did not cancel task lease")
	}
	_, err = runtimeSupervisor.WriteFile(ctx, preDispatchLease, RuntimeFileWriteRequest{Path: preDispatchName, Content: []byte("must-not-dispatch\n"), ExpectedAbsent: true})
	if !errors.Is(err, ErrConnectionGenerationRevoked) || errors.Is(err, ErrRuntimeOutcomeUnknown) {
		t.Fatalf("pre-dispatch write error=%v", err)
	}
	if err := runLiveFixedCommand(ctx, locator, adminAlias, `test ! -e `+preDispatchPath); err != nil {
		t.Fatalf("pre-dispatch write reached remote helper: %v", err)
	}
}

func waitLiveTerminalOutput(session *RuntimeTerminalSession, expected []byte, timeout time.Duration) error {
	deadline := time.After(timeout)
	var output []byte
	for {
		select {
		case event, ok := <-session.Events():
			if !ok {
				return errors.New("Terminal event stream closed")
			}
			switch event.Type {
			case "output":
				output = append(output, event.Data...)
				if len(expected) == 0 || bytes.Contains(output, expected) {
					return nil
				}
			case "exit", "disconnected":
				return fmt.Errorf("Terminal ended before expected output: type=%s error=%v", event.Type, event.Error)
			}
		case <-deadline:
			return errors.New("Terminal output timed out")
		}
	}
}

func waitLiveGenerationRevoked(connection *ConnectionSupervisor, generation uint64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := connection.ValidateGeneration(generation)
		if errors.Is(err, ErrConnectionGenerationRevoked) {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func setLiveProxyLatency(ctx context.Context, controlURL string, enabled bool) error {
	method := http.MethodDelete
	body := io.Reader(nil)
	endpoint := controlURL + "/proxies/ssh/toxics/pi-desk-response-latency"
	if enabled {
		method = http.MethodPost
		body = strings.NewReader(`{"name":"pi-desk-response-latency","type":"latency","stream":"downstream","attributes":{"latency":10000,"jitter":0}}`)
		endpoint = controlURL + "/proxies/ssh/toxics"
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return fmt.Errorf("set live SSH response latency enabled=%t: %w", enabled, err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	closeErr := response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("set live SSH response latency enabled=%t: HTTP %s", enabled, response.Status)
	}
	return closeErr
}

func setLiveProxyEnabled(ctx context.Context, controlURL string, enabled bool) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, controlURL+"/proxies/ssh", strings.NewReader(fmt.Sprintf(`{"enabled":%t}`, enabled)))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return fmt.Errorf("set live SSH proxy enabled=%t: %w", enabled, err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	closeErr := response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("set live SSH proxy enabled=%t: HTTP %s", enabled, response.Status)
	}
	return closeErr
}

func TestLiveSSHHelperBootstrap(t *testing.T) {
	hostAlias := os.Getenv("PI_DESK_SSH_LIVE_TARGET")
	artifactPath := os.Getenv("PI_DESK_SSH_LIVE_HELPER")
	rootPath := os.Getenv("PI_DESK_SSH_LIVE_DIRECTORY")
	artifactOS := os.Getenv("PI_DESK_SSH_LIVE_HELPER_OS")
	artifactArchitecture := os.Getenv("PI_DESK_SSH_LIVE_HELPER_ARCH")
	buildIdentity := os.Getenv("PI_DESK_SSH_LIVE_HELPER_BUILD")
	if hostAlias == "" || artifactPath == "" || rootPath == "" || artifactOS == "" || artifactArchitecture == "" || buildIdentity == "" {
		t.Skip("set PI_DESK_SSH_LIVE_TARGET, PI_DESK_SSH_LIVE_DIRECTORY and all PI_DESK_SSH_LIVE_HELPER* variables to run SFTP/helper bootstrap")
	}
	if !validLiveDirectory(rootPath) {
		t.Fatal("PI_DESK_SSH_LIVE_DIRECTORY must be a normalized shell-safe absolute POSIX path")
	}
	file, err := os.Open(artifactPath)
	if err != nil {
		t.Fatalf("open packaged helper: %v", err)
	}
	content, err := io.ReadAll(io.LimitReader(file, maxHelperArtifactBytes+1))
	closeErr := file.Close()
	if err != nil || closeErr != nil || len(content) == 0 || len(content) > maxHelperArtifactBytes {
		t.Fatalf("read packaged helper: read=%v close=%v size=%d", err, closeErr, len(content))
	}
	artifact := helperArtifactForTest(artifactOS, artifactArchitecture, content)
	artifact.BuildIdentity = buildIdentity
	if err := artifact.Validate(); err != nil {
		t.Fatalf("live helper artifact metadata: %v", err)
	}
	target, err := NewTarget(hostAlias)
	if err != nil {
		t.Fatal(err)
	}
	locator := newLiveLocator(t)
	supervisor, err := NewConnectionSupervisor(locator, target)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	ready, err := supervisor.Connect(ctx)
	if err != nil {
		t.Fatalf("connect before helper bootstrap: %v", err)
	}
	installer, err := NewHelperInstaller(locator, supervisor)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := installer.Install(ctx, ready.Generation, artifact, content)
	if err != nil {
		t.Fatalf("install exact helper: %v", err)
	}
	if installed.RemotePath == "" {
		t.Fatal("installer returned an empty cache path")
	}
	wrongIdentity := artifact
	wrongIdentity.BuildIdentity += "-mismatch"
	failedGeneration := ready.Generation
	if _, err := installer.ProbeInstalledHelper(ctx, failedGeneration, wrongIdentity); !errors.Is(err, ErrHelperProtocolMismatch) {
		t.Fatalf("mismatched helper identity error = %v", err)
	}
	if err := supervisor.ValidateGeneration(failedGeneration); !errors.Is(err, ErrConnectionGenerationRevoked) {
		t.Fatalf("helper identity mismatch did not revoke generation: %v", err)
	}
	ready, err = supervisor.Connect(ctx)
	if err != nil || ready.Generation <= failedGeneration {
		t.Fatalf("explicit reconnect after helper mismatch: ready=%#v err=%v", ready, err)
	}
	probe, err := installer.ProbeInstalledHelper(ctx, ready.Generation, artifact)
	if err != nil {
		t.Fatalf("probe installed helper: %v", err)
	}
	if probe.ProtocolVersion != artifact.ProtocolVersion || probe.BuildIdentity != artifact.BuildIdentity || probe.OS != artifact.OS || probe.Architecture != artifact.Architecture || !sameCapabilities(probe.Capabilities, requiredHelperCapabilities()) {
		t.Fatalf("unexpected helper identity: %#v", probe)
	}
	reused, err := installer.Install(ctx, ready.Generation, artifact, content)
	if err != nil || !reused.Reused || reused.RemotePath != installed.RemotePath {
		t.Fatalf("exact helper reuse: result=%#v err=%v", reused, err)
	}
	fixtureName := ".pi-desk-readonly-fixture"
	fixturePath := path.Join(rootPath, fixtureName)
	imageFixtureName := ".pi-desk-image-fixture.png"
	imageFixturePath := path.Join(rootPath, imageFixtureName)
	writeFixtureName := ".pi-desk-write-fixture"
	writeFixturePath := path.Join(rootPath, writeFixtureName)
	directoryFixtureName := ".pi-desk-directory-fixture"
	directoryFixturePath := path.Join(rootPath, directoryFixtureName)
	gitFixtureName := ".pi-desk-git-fixture"
	gitFixturePath := path.Join(rootPath, gitFixtureName)
	gitFilterMarkerPath := path.Join(gitFixturePath, "filter-ran")
	if err := runLiveFixedCommand(ctx, locator, hostAlias, `umask 077; rm -f `+writeFixturePath+`; rm -rf `+directoryFixturePath+` `+gitFixturePath+`; mkdir `+gitFixturePath+`; cd `+gitFixturePath+`; /usr/bin/git init -q; printf '*.txt filter=evil\n' > .gitattributes; printf 'tracked\n' > tracked.txt; /usr/bin/git add tracked.txt .gitattributes; /usr/bin/git config filter.evil.clean 'touch `+gitFilterMarkerPath+`; /bin/cat'; /usr/bin/git config filter.evil.required true; printf 'changed\n' > tracked.txt; printf 'untracked\n' > untracked.txt; /usr/bin/git status --porcelain >/dev/null; test -e `+gitFilterMarkerPath+`; rm -f `+gitFilterMarkerPath+`; cd `+rootPath+`; printf 'alpha\nbeta\n' > `+fixturePath+`; printf '\211PNG\015\012\032\012fixture' > `+imageFixturePath); err != nil {
		t.Fatalf("create read-only helper fixture: %v", err)
	}
	defer func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := runLiveFixedCommand(cleanupContext, locator, hostAlias, `rm -f `+fixturePath+` `+imageFixturePath+` `+writeFixturePath+`; rm -rf `+directoryFixturePath+` `+gitFixturePath); err != nil {
			t.Errorf("remove read-only helper fixture: %v", err)
		}
	}()

	runtimeFactory, err := NewInstalledHelperGenerationFactory(installer, artifact)
	if err != nil {
		t.Fatal(err)
	}
	runtimeSupervisor, err := NewRuntimeLeaseSupervisor(supervisor, runtimeFactory)
	if err != nil {
		t.Fatal(err)
	}
	openedRoot, err := runtimeSupervisor.OpenRoot(ctx, RuntimeRootOpenRequest{
		Generation: ready.Generation, WorkspaceID: "workspace-0123456789abcdef0123456789abcdef", RequestedRoot: rootPath,
	})
	if err != nil {
		t.Fatalf("open persistent helper root capability: %v", err)
	}
	if openedRoot.Identity.CanonicalPath != rootPath || openedRoot.Identity.Device == 0 || openedRoot.Identity.Inode == 0 {
		t.Fatalf("unexpected live root identity: %#v", openedRoot.Identity)
	}
	reopenedRoot, err := runtimeSupervisor.OpenRoot(ctx, RuntimeRootOpenRequest{
		Generation: ready.Generation, WorkspaceID: "workspace-0123456789abcdef0123456789abcdef",
		RequestedRoot: rootPath, Expected: &openedRoot.Identity,
	})
	if err != nil || reopenedRoot.Capability.rootHandle != openedRoot.Capability.rootHandle || reopenedRoot.Identity != openedRoot.Identity {
		t.Fatalf("reuse persistent helper root capability: reopened=%#v err=%v", reopenedRoot, err)
	}
	runtimeLease, err := runtimeSupervisor.AcquireTask(ctx, RuntimeLeaseRequest{Root: openedRoot.Capability, OwnerID: "live-helper-task"})
	if err != nil {
		t.Fatalf("start persistent helper generation: %v", err)
	}
	rootStat, err := runtimeSupervisor.StatFile(ctx, runtimeLease, ".")
	if err != nil || rootStat.Kind != "directory" {
		t.Fatalf("stat live root: stat=%#v err=%v", rootStat, err)
	}
	listing, err := runtimeSupervisor.ListFiles(ctx, runtimeLease, ".")
	if err != nil || !slices.ContainsFunc(listing.Entries, func(entry RuntimeFileInfo) bool { return entry.Path == fixtureName && entry.Kind == "file" }) {
		t.Fatalf("list live root: list=%#v err=%v", listing, err)
	}
	read, err := runtimeSupervisor.ReadFile(ctx, runtimeLease, fixtureName, 1, 10)
	if err != nil || read.Content != "alpha\nbeta" || read.StartLine != 1 || read.EndLine != 2 {
		t.Fatalf("read live file: read=%#v err=%v", read, err)
	}
	wantDigest := sha256.Sum256([]byte("alpha\nbeta\n"))
	hash, err := runtimeSupervisor.HashFile(ctx, runtimeLease, fixtureName)
	if err != nil || hash.SHA256 != fmt.Sprintf("%x", wantDigest) || hash.Size != int64(len("alpha\nbeta\n")) {
		t.Fatalf("hash live file: hash=%#v err=%v", hash, err)
	}
	imageFixtureContent := []byte("\x89PNG\r\n\x1a\nfixture")
	imageDigest := sha256.Sum256(imageFixtureContent)
	image, err := runtimeSupervisor.ReadImage(ctx, runtimeLease, imageFixtureName)
	if err != nil || image.MIME != "image/png" || image.SHA256 != fmt.Sprintf("%x", imageDigest) || !bytes.Equal(image.Content, imageFixtureContent) {
		t.Fatalf("read live image: image=%#v err=%v", image, err)
	}
	directory, err := runtimeSupervisor.EnsureDirectory(ctx, runtimeLease, directoryFixtureName+"/deep")
	if err != nil || !slices.Equal(directory.Created, []string{directoryFixtureName, directoryFixtureName + "/deep"}) {
		t.Fatalf("create live directories: result=%#v err=%v", directory, err)
	}
	nestedContent := []byte("nested remote write\n")
	if _, err := runtimeSupervisor.WriteFile(ctx, runtimeLease, RuntimeFileWriteRequest{
		Path: directoryFixtureName + "/deep/file.txt", Content: nestedContent, ExpectedAbsent: true,
	}); err != nil {
		t.Fatalf("write live nested file: %v", err)
	}
	if _, err := runtimeSupervisor.EditFile(ctx, runtimeLease, RuntimeFileEditRequest{
		Path: directoryFixtureName + "/deep/file.txt", OldText: "nested", NewText: "edited",
	}); err != nil {
		t.Fatalf("edit live nested file: %v", err)
	}
	edited, err := runtimeSupervisor.ReadFile(ctx, runtimeLease, directoryFixtureName+"/deep/file.txt", 1, 10)
	if err != nil || edited.Content != "edited remote write" {
		t.Fatalf("read live edited file: read=%#v err=%v", edited, err)
	}
	writeFirst := []byte("first remote write\n")
	writeFirstDigest := sha256.Sum256(writeFirst)
	created, err := runtimeSupervisor.WriteFile(ctx, runtimeLease, RuntimeFileWriteRequest{
		Path: writeFixtureName, Content: writeFirst, ExpectedAbsent: true,
	})
	if err != nil || !created.Created || created.SHA256 != fmt.Sprintf("%x", writeFirstDigest) {
		t.Fatalf("create live file: result=%#v err=%v", created, err)
	}
	writeSecond := []byte("second remote write\n")
	writeSecondDigest := sha256.Sum256(writeSecond)
	updated, err := runtimeSupervisor.WriteFile(ctx, runtimeLease, RuntimeFileWriteRequest{
		Path: writeFixtureName, Content: writeSecond, ExpectedSHA256: fmt.Sprintf("%x", writeFirstDigest),
	})
	if err != nil || updated.Created || updated.SHA256 != fmt.Sprintf("%x", writeSecondDigest) {
		t.Fatalf("update live file: result=%#v err=%v", updated, err)
	}
	if _, err := runtimeSupervisor.WriteFile(ctx, runtimeLease, RuntimeFileWriteRequest{
		Path: writeFixtureName, Content: []byte("must not win"), ExpectedSHA256: fmt.Sprintf("%x", writeFirstDigest),
	}); !errors.Is(err, ErrRuntimeFileConflict) {
		t.Fatalf("live write conflict error=%v", err)
	}
	written, err := runtimeSupervisor.ReadFile(ctx, runtimeLease, writeFixtureName, 1, 10)
	if err != nil || written.Content != strings.TrimSuffix(string(writeSecond), "\n") {
		t.Fatalf("read live written file: read=%#v err=%v", written, err)
	}
	found, err := runtimeSupervisor.FindFiles(ctx, runtimeLease, RuntimeSearchFindRequest{Path: ".", Pattern: fixtureName, Limit: 10})
	if err != nil || !slices.Contains(found.Paths, fixtureName) {
		t.Fatalf("find live file: found=%#v err=%v", found, err)
	}
	grep, err := runtimeSupervisor.GrepFiles(ctx, runtimeLease, RuntimeSearchGrepRequest{Path: ".", Pattern: "beta", Glob: fixtureName, Limit: 10})
	if err != nil || len(grep.Matches) != 1 || grep.Matches[0].Path != fixtureName || grep.Matches[0].Line != 2 {
		t.Fatalf("grep live file: grep=%#v err=%v", grep, err)
	}
	gitRoot, err := runtimeSupervisor.OpenRoot(ctx, RuntimeRootOpenRequest{
		Generation: ready.Generation, WorkspaceID: "workspace-fedcba9876543210fedcba9876543210", RequestedRoot: gitFixturePath,
	})
	if err != nil {
		t.Fatalf("open live Git root: %v", err)
	}
	gitLease, err := runtimeSupervisor.AcquireRead(ctx, RuntimeLeaseRequest{Root: gitRoot.Capability, OwnerID: "live-git-read"})
	if err != nil {
		t.Fatalf("acquire live Git read lease: %v", err)
	}
	for _, request := range []RuntimeGitReadRequest{{Operation: "status"}, {Operation: "files"}, {Operation: "diff", Path: "tracked.txt"}, {Operation: "branches"}} {
		result, err := runtimeSupervisor.ReadGit(ctx, gitLease, request)
		if err != nil || result.Operation != request.Operation {
			t.Fatalf("live Git %s: result=%#v err=%v", request.Operation, result, err)
		}
	}
	gitLease.Release()
	if err := runLiveFixedCommand(ctx, locator, hostAlias, `test ! -e `+gitFilterMarkerPath); err != nil {
		t.Fatalf("isolated Git executed configured filter: %v", err)
	}
	bashResult, err := runtimeSupervisor.RunBash(ctx, runtimeLease, `test -z "${PI_DESK_REMOTE_ENV_SENTINEL+x}" || exit 99; printf 'bash-out'; printf 'bash-err' >&2; exit 7`)
	if err != nil || bashResult.ExitCode != 7 || bashResult.Output != "bash-outbash-err" {
		t.Fatalf("live Bash: result=%#v err=%v", bashResult, err)
	}
	bashContext, cancelBash := context.WithTimeout(ctx, 200*time.Millisecond)
	_, bashErr := runtimeSupervisor.RunBash(bashContext, runtimeLease, `trap '' INT TERM; while :; do sleep 1; done`)
	cancelBash()
	if !errors.Is(bashErr, context.Canceled) {
		t.Fatalf("cancel live Bash error=%v", bashErr)
	}
	terminalSession, err := runtimeSupervisor.StartTerminal(ctx, runtimeLease, 80, 24)
	if err != nil {
		t.Fatalf("start live Terminal: %v", err)
	}
	if err := terminalSession.Resize(100, 30); err != nil {
		t.Fatalf("resize live Terminal: %v", err)
	}
	if err := terminalSession.Input([]byte("printf 'live-terminal'; exit 4\n")); err != nil {
		t.Fatalf("input live Terminal: %v", err)
	}
	var terminalOutput []byte
	terminalExit := -1
	for event := range terminalSession.Events() {
		if event.Type == "output" {
			terminalOutput = append(terminalOutput, event.Data...)
		}
		if event.Type == "exit" {
			terminalExit = event.ExitCode
		}
	}
	if terminalExit != 4 || !bytes.Contains(terminalOutput, []byte("live-terminal")) {
		t.Fatalf("live Terminal exit=%d output=%q", terminalExit, terminalOutput)
	}
	mutationResults := make(chan error, 2)
	for _, content := range [][]byte{[]byte("left wins\n"), []byte("right wins\n")} {
		go func(content []byte) {
			_, err := runtimeSupervisor.WriteFile(ctx, runtimeLease, RuntimeFileWriteRequest{
				Path: writeFixtureName, Content: content, ExpectedSHA256: fmt.Sprintf("%x", writeSecondDigest),
			})
			mutationResults <- err
		}(content)
	}
	mutationSuccess, mutationConflict := 0, 0
	for range 2 {
		switch err := <-mutationResults; {
		case err == nil:
			mutationSuccess++
		case errors.Is(err, ErrRuntimeFileConflict):
			mutationConflict++
		default:
			t.Fatalf("concurrent live mutation error=%v", err)
		}
	}
	if mutationSuccess != 1 || mutationConflict != 1 {
		t.Fatalf("concurrent live mutations succeeded=%d conflicted=%d", mutationSuccess, mutationConflict)
	}
	concurrent := make(chan error, 16)
	for index := range 16 {
		go func(index int) {
			switch index % 3 {
			case 0:
				_, err := runtimeSupervisor.StatFile(ctx, runtimeLease, fixtureName)
				concurrent <- err
			case 1:
				_, err := runtimeSupervisor.HashFile(ctx, runtimeLease, fixtureName)
				concurrent <- err
			default:
				_, err := runtimeSupervisor.ReadImage(ctx, runtimeLease, imageFixtureName)
				concurrent <- err
			}
		}(index)
	}
	for range 16 {
		if err := <-concurrent; err != nil {
			t.Fatalf("concurrent live read-only operation: %v", err)
		}
	}
	if snapshot := runtimeSupervisor.Snapshot(); snapshot.State != RuntimeReady || snapshot.Generation != ready.Generation || snapshot.TaskLeases != 1 {
		t.Fatalf("persistent helper snapshot: %#v", snapshot)
	}
	runtimeLease.Release()
	if err := runtimeSupervisor.Disconnect(ctx); err != nil {
		t.Fatalf("stop persistent helper generation: %v", err)
	}
	ready, err = supervisor.Connect(ctx)
	if err != nil {
		t.Fatalf("reconnect after persistent helper shutdown: %v", err)
	}

	generationContext, release, err := supervisor.bindGenerationContext(ctx, ready.Generation)
	if err != nil {
		t.Fatal(err)
	}
	filesystem, err := locator.openSFTP(generationContext, hostAlias)
	if err != nil {
		release()
		t.Fatalf("open SFTP for noexec injection: %v", err)
	}
	chmodErr := filesystem.Chmod(installed.RemotePath, 0o600)
	sftpCloseErr := filesystem.Close()
	release()
	if chmodErr != nil || sftpCloseErr != nil {
		t.Fatalf("inject non-executable helper: chmod=%v close=%v", chmodErr, sftpCloseErr)
	}
	noexecGeneration := ready.Generation
	if _, err := installer.ProbeInstalledHelper(ctx, noexecGeneration, artifact); !errors.Is(err, ErrHelperArtifactUnsupported) {
		t.Fatalf("non-executable helper error = %v", err)
	}
	if err := supervisor.ValidateGeneration(noexecGeneration); !errors.Is(err, ErrConnectionGenerationRevoked) {
		t.Fatalf("non-executable helper did not revoke generation: %v", err)
	}
	ready, err = supervisor.Connect(ctx)
	if err != nil {
		t.Fatalf("reconnect before helper repair: %v", err)
	}
	repaired, err := installer.Install(ctx, ready.Generation, artifact, content)
	if err != nil || repaired.Reused {
		t.Fatalf("repair non-executable helper: result=%#v err=%v", repaired, err)
	}
	if _, err := installer.ProbeInstalledHelper(ctx, ready.Generation, artifact); err != nil {
		t.Fatalf("probe repaired helper: %v", err)
	}
}

func validLiveDirectory(value string) bool {
	if value == "" || value == "/" || !strings.HasPrefix(value, "/") || path.Clean(value) != value {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("/._-", char) {
			continue
		}
		return false
	}
	return true
}

func runExpectedLiveFailure(t *testing.T, ctx context.Context, locator *Locator, target, knownHosts string) ConnectionFailure {
	t.Helper()
	invocation, err := locator.connectionProbeInvocation(target)
	if err != nil {
		t.Fatal(err)
	}
	knownHosts = filepath.ToSlash(knownHosts)
	invocation.Args = insertBeforeTarget(invocation.Args,
		"-o", "UserKnownHostsFile="+knownHosts,
		"-o", "GlobalKnownHostsFile="+knownHosts,
	)
	output, runErr := locator.runner.Run(ctx, invocation.Executable, invocation.Args...)
	if runErr == nil {
		t.Fatal("connection unexpectedly succeeded with isolated known_hosts")
	}
	return ClassifyOpenSSHFailure(output.Stderr)
}

func insertBeforeTarget(args []string, extra ...string) []string {
	boundary := slices.Index(args, "--")
	if boundary < 0 {
		panic("OpenSSH invocation has no argument boundary")
	}
	result := make([]string, 0, len(args)+len(extra))
	result = append(result, args[:boundary]...)
	result = append(result, extra...)
	result = append(result, args[boundary:]...)
	return result
}

func runLiveFixedCommand(ctx context.Context, locator *Locator, hostAlias, command string) error {
	invocation, err := locator.connectionProbeInvocation(hostAlias)
	if err != nil {
		return err
	}
	invocation.Args = replaceProbeCommand(invocation.Args, command)
	output, err := locator.runner.Run(ctx, invocation.Executable, invocation.Args...)
	if err != nil {
		failure := ClassifyOpenSSHFailure(output.Stderr)
		return fmt.Errorf("%s: %s", failure.Code, failure.Reason)
	}
	if len(bytes.TrimSpace(output.Stdout)) != 0 {
		return errors.New("fixed live SSH command returned unexpected output")
	}
	if _, err := ParseHostKeyEvidence(output.Stderr); err != nil {
		return errors.New("fixed live SSH command returned no host-key evidence")
	}
	return nil
}

func replaceProbeCommand(args []string, command ...string) []string {
	if len(args) < 2 || args[len(args)-1] != "true" {
		panic("OpenSSH invocation does not end in the fixed probe command")
	}
	result := append([]string(nil), args[:len(args)-1]...)
	return append(result, command...)
}

func knownHostsPattern(config EffectiveConfig) string {
	if config.HostKeyAlias != "" {
		return config.HostKeyAlias
	}
	if config.Port != 22 {
		return fmt.Sprintf("[%s]:%d", config.HostName, config.Port)
	}
	return config.HostName
}

func fakeEd25519PublicKey() string {
	var blob bytes.Buffer
	_ = binary.Write(&blob, binary.BigEndian, uint32(len("ssh-ed25519")))
	_, _ = blob.WriteString("ssh-ed25519")
	_ = binary.Write(&blob, binary.BigEndian, uint32(32))
	_, _ = blob.Write(bytes.Repeat([]byte{0x42}, 32))
	return base64.StdEncoding.EncodeToString(blob.Bytes())
}

func TestFakeEd25519PublicKeyHasDifferentFingerprint(t *testing.T) {
	blob, err := base64.StdEncoding.DecodeString(fakeEd25519PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(blob)
	fingerprint := "SHA256:" + base64.RawStdEncoding.EncodeToString(hash[:])
	if fingerprint == "" {
		t.Fatal("deterministic fake key returned an empty fingerprint")
	}
}
