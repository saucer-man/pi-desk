package remotessh

import (
	"context"
	"errors"
	"strings"
	"testing"

	"pi-desk/internal/remotehelper"
	"pi-desk/internal/remoteprotocol"
)

func TestValidateHelperResponseRejectsWrongEnvelopeAndStrictPayload(t *testing.T) {
	payload, err := remoteprotocol.EncodePayload(remotehelper.PingResponse{OK: true})
	if err != nil {
		t.Fatal(err)
	}
	valid := remoteprotocol.Frame{Envelope: remoteprotocol.Envelope{
		Version:    remoteprotocol.Version,
		Kind:       remoteprotocol.KindResponse,
		ID:         "ping",
		Generation: 7,
		Payload:    payload,
	}}
	var response remotehelper.PingResponse
	if err := validateHelperResponse(valid, "ping", 7, &response); err != nil || !response.OK {
		t.Fatalf("valid response rejected: response=%#v err=%v", response, err)
	}

	wrongGeneration := valid
	wrongGeneration.Envelope.Generation = 8
	if err := validateHelperResponse(wrongGeneration, "ping", 7, &response); !errors.Is(err, ErrHelperProtocolMismatch) {
		t.Fatalf("wrong generation error = %v", err)
	}
	withBlob := valid
	withBlob.Blob = []byte("unexpected")
	if err := validateHelperResponse(withBlob, "ping", 7, &response); !errors.Is(err, ErrHelperProtocolMismatch) {
		t.Fatalf("unexpected blob error = %v", err)
	}
	invalidPayload := valid
	invalidPayload.Envelope.Payload = []byte(`{"ok":true,"extra":1}`)
	if err := validateHelperResponse(invalidPayload, "ping", 7, &response); !errors.Is(err, ErrHelperProtocolMismatch) {
		t.Fatalf("unknown payload field error = %v", err)
	}
}

func TestHelperProcessErrorIsRedactedAndClassified(t *testing.T) {
	secret := "private-user@private-host"
	stderr := &boundedOutput{limit: maxConnectionProbeStderrBytes}
	_, _ = stderr.Write([]byte(secret + ": Permission denied"))
	err := helperProcessError(context.Background(), stderr, errors.New("exit status 126"))
	if !errors.Is(err, ErrHelperArtifactUnsupported) || strings.Contains(err.Error(), secret) {
		t.Fatalf("permission failure = %v", err)
	}

	overflow := &boundedOutput{limit: 1, overflow: true}
	err = helperProcessError(context.Background(), overflow, errors.New("exit status 255"))
	var lifecycleErr *ConnectionSupervisorError
	if !errors.As(err, &lifecycleErr) || lifecycleErr.Failure.Code != FailureOutputLimit || lifecycleErr.Failure.Reason != ReasonOutputLimit {
		t.Fatalf("overflow failure = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = helperProcessError(ctx, &boundedOutput{limit: 1}, context.Canceled)
	if !errors.Is(err, ErrConnectionGenerationRevoked) {
		t.Fatalf("cancelled helper failure = %v", err)
	}
}

func TestSameCapabilitiesIsOrderIndependentButExact(t *testing.T) {
	if !sameCapabilities([]string{"b", "a"}, []string{"a", "b"}) {
		t.Fatal("same capability set was rejected")
	}
	if sameCapabilities([]string{"a", "a", "b"}, []string{"a", "b"}) || sameCapabilities([]string{"a"}, []string{"a", "b"}) {
		t.Fatal("non-exact capability set was accepted")
	}
}

func TestNewHelperInstallerRequiresSupervisorLocator(t *testing.T) {
	target, err := NewTarget("build-prod")
	if err != nil {
		t.Fatal(err)
	}
	first := newLocator(&fakeCommandRunner{paths: map[string]string{"ssh": "/usr/bin/ssh"}}, "linux", "")
	second := newLocator(&fakeCommandRunner{paths: map[string]string{"ssh": "/other/ssh"}}, "linux", "")
	supervisor, err := NewConnectionSupervisor(first, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewHelperInstaller(second, supervisor); err == nil {
		t.Fatal("helper installer accepted a different OpenSSH locator")
	}
}
