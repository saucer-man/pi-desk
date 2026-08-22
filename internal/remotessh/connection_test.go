package remotessh

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const validHostKeyDebug = `debug1: kex: host key algorithm: ssh-ed25519
debug1: Server host key: ssh-ed25519 SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
`

func TestValidLiveDirectory(t *testing.T) {
	for _, value := range []string{"/root/test", "/srv/workspace-1", "/home/user/.project"} {
		if !validLiveDirectory(value) {
			t.Fatalf("validLiveDirectory(%q) = false", value)
		}
	}
	for _, value := range []string{"", "/", "relative", "/root/../etc", "/root/two words", "/root/test;id", "//root/test"} {
		if validLiveDirectory(value) {
			t.Fatalf("validLiveDirectory(%q) = true", value)
		}
	}
}

func TestProbeConnectionBindsConfigAndObservedHostKey(t *testing.T) {
	runner := &fakeCommandRunner{
		paths:     map[string]string{"ssh": "/usr/bin/ssh"},
		output:    []byte(validEffectiveConfig),
		runOutput: commandOutput{Stderr: []byte(validHostKeyDebug)},
	}
	preflight, err := newLocator(runner, "linux", "").ProbeConnection(context.Background(), "build-prod")
	if err != nil {
		t.Fatalf("ProbeConnection returned an error: %v", err)
	}
	if preflight.Config.Fingerprint == "" || preflight.HostKey.Algorithm != "ssh-ed25519" || preflight.HostKey.SHA256Hash == "" {
		t.Fatalf("unexpected connection preflight: %#v", preflight)
	}
	if runner.combinedCalls != 1 || runner.runCalls != 1 || len(runner.commandArgs) < 5 || runner.commandArgs[len(runner.commandArgs)-5] != "-n" || runner.commandArgs[len(runner.commandArgs)-4] != "-v" || runner.commandArgs[len(runner.commandArgs)-1] != "true" {
		t.Fatalf("unexpected command sequence: combined=%d run=%d args=%#v", runner.combinedCalls, runner.runCalls, runner.commandArgs)
	}
}

func TestProbeConnectionReturnsRedactedStableFailure(t *testing.T) {
	secret := "secret-user@private-host"
	runner := &fakeCommandRunner{
		paths:     map[string]string{"ssh": "/usr/bin/ssh"},
		output:    []byte(validEffectiveConfig),
		runOutput: commandOutput{Stderr: []byte(secret + ": Permission denied (publickey).")},
		runErr:    errors.New("exit status 255"),
	}
	_, err := newLocator(runner, "linux", "").ProbeConnection(context.Background(), "build-prod")
	var probeErr *ConnectionProbeError
	if !errors.As(err, &probeErr) {
		t.Fatalf("ProbeConnection error = %v, want ConnectionProbeError", err)
	}
	if probeErr.Failure.Code != FailureAuthRequired || probeErr.Failure.Reason != ReasonAuthenticationRejected {
		t.Fatalf("unexpected failure classification: %#v", probeErr.Failure)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("connection error leaked stderr: %v", err)
	}
}

func TestProbeConnectionFailsClosedOnRemoteProfileOutput(t *testing.T) {
	runner := &fakeCommandRunner{
		paths:  map[string]string{"ssh": "/usr/bin/ssh"},
		output: []byte(validEffectiveConfig),
		runOutput: commandOutput{
			Stdout: []byte("unexpected profile output\n"),
			Stderr: []byte(validHostKeyDebug),
		},
	}
	_, err := newLocator(runner, "linux", "").ProbeConnection(context.Background(), "build-prod")
	var probeErr *ConnectionProbeError
	if !errors.As(err, &probeErr) || probeErr.Failure.Code != FailureConnect || probeErr.Failure.Reason != ReasonHostOutput {
		t.Fatalf("ProbeConnection error = %v, want host output failure", err)
	}
	if strings.Contains(err.Error(), "profile") {
		t.Fatalf("connection error leaked stdout: %v", err)
	}
}

func TestProbeConnectionFailsClosedOnOutputLimit(t *testing.T) {
	runner := &fakeCommandRunner{
		paths:  map[string]string{"ssh": "/usr/bin/ssh"},
		output: []byte(validEffectiveConfig),
		runErr: ErrProbeOutputTooLarge,
	}
	_, err := newLocator(runner, "linux", "").ProbeConnection(context.Background(), "build-prod")
	var probeErr *ConnectionProbeError
	if !errors.As(err, &probeErr) || probeErr.Failure.Code != FailureOutputLimit || probeErr.Failure.Reason != ReasonOutputLimit {
		t.Fatalf("ProbeConnection error = %v, want output-limit failure", err)
	}
}

func TestProbeConnectionFailsClosedWithoutHostKeyEvidence(t *testing.T) {
	runner := &fakeCommandRunner{
		paths:     map[string]string{"ssh": "/usr/bin/ssh"},
		output:    []byte(validEffectiveConfig),
		runOutput: commandOutput{Stderr: []byte("debug1: authenticated")},
	}
	_, err := newLocator(runner, "linux", "").ProbeConnection(context.Background(), "build-prod")
	var probeErr *ConnectionProbeError
	if !errors.As(err, &probeErr) || probeErr.Failure.Code != FailureConnect || probeErr.Failure.Reason != ReasonHostKeyEvidence {
		t.Fatalf("ProbeConnection error = %v, want host-key evidence failure", err)
	}
}

func TestProbeConnectionDoesNotConnectWhenEffectiveEnvironmentIsUnsafe(t *testing.T) {
	runner := &fakeCommandRunner{
		paths:  map[string]string{"ssh": "/usr/bin/ssh"},
		output: []byte(validEffectiveConfig + "setenv API_TOKEN=secret\n"),
	}
	_, err := newLocator(runner, "linux", "").ProbeConnection(context.Background(), "build-prod")
	if !errors.Is(err, ErrSSHEnvironmentUnsafe) {
		t.Fatalf("ProbeConnection error = %v, want ErrSSHEnvironmentUnsafe", err)
	}
	if runner.runCalls != 0 {
		t.Fatalf("unsafe effective config made %d connection calls", runner.runCalls)
	}
}

func TestProbeConnectionClassifiesCancellationDuringConfigPreflight(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &fakeCommandRunner{
		paths:       map[string]string{"ssh": "/usr/bin/ssh"},
		combinedErr: context.Canceled,
	}
	_, err := newLocator(runner, "linux", "").ProbeConnection(ctx, "build-prod")
	var probeErr *ConnectionProbeError
	if !errors.As(err, &probeErr) || probeErr.Failure.Code != FailureCancelled || probeErr.Failure.Reason != ReasonCancelled {
		t.Fatalf("ProbeConnection config error = %v, want cancellation", err)
	}
	if runner.runCalls != 0 {
		t.Fatalf("cancelled config preflight made %d connection calls", runner.runCalls)
	}
}

func TestProbeConnectionClassifiesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &fakeCommandRunner{
		paths:  map[string]string{"ssh": "/usr/bin/ssh"},
		output: []byte(validEffectiveConfig),
		runErr: context.Canceled,
	}
	_, err := newLocator(runner, "linux", "").ProbeConnection(ctx, "build-prod")
	var probeErr *ConnectionProbeError
	if !errors.As(err, &probeErr) || probeErr.Failure.Code != FailureCancelled || probeErr.Failure.Reason != ReasonCancelled {
		t.Fatalf("ProbeConnection error = %v, want cancellation", err)
	}
}
