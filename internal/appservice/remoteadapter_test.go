package appservice

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRemoteAdapterManifestPinsContentCompatibilityAndCoverage(t *testing.T) {
	manifest, err := verifyRemoteAdapterBundle("0.84.2")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Protocol != remoteAdapterProtocol || !slices.Equal(manifest.Coverage, remoteAdapterCoverage) {
		t.Fatalf("manifest=%#v", manifest)
	}
	for _, incompatible := range []string{"", "0.84.1", "0.85.0", "invalid"} {
		if _, err := verifyRemoteAdapterBundle(incompatible); err == nil {
			t.Fatalf("incompatible Pi version accepted: %q", incompatible)
		}
	}
	if validRemoteAdapterHandshake(manifest, "wrong", manifest.Coverage) || validRemoteAdapterHandshake(manifest, manifest.Protocol, manifest.Coverage[:len(manifest.Coverage)-1]) {
		t.Fatal("incomplete adapter coverage handshake was accepted")
	}
	if !validRemoteAdapterHandshake(manifest, manifest.Protocol, manifest.Coverage) {
		t.Fatal("exact adapter coverage handshake was rejected")
	}
	broker := &remoteTaskBroker{manifest: manifest, handshakeDone: make(chan struct{}), ctx: context.Background()}
	if _, err := broker.execute(context.Background(), remoteBrokerRequest{Operation: "hello", Protocol: "wrong", Coverage: manifest.Coverage}, 123); err == nil {
		t.Fatal("invalid broker handshake succeeded")
	}
	if err := broker.waitHandshake(context.Background(), 123); err == nil {
		t.Fatal("invalid broker handshake did not fail startup admission")
	}
	closed := make(chan struct{})
	close(closed)
	peerFenced := &remoteTaskBroker{ctx: context.Background(), handshakeDone: closed, launcherPID: 2147483646}
	if _, err := peerFenced.execute(context.Background(), remoteBrokerRequest{Operation: "stat", Path: "."}, 2147483647); err == nil {
		t.Fatal("non-Pi broker peer was accepted after handshake")
	}

	path := filepath.Join(t.TempDir(), "adapter.ts")
	if err := os.WriteFile(path, remoteAdapterSource, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyRemoteAdapterFile(path, manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(append([]byte(nil), remoteAdapterSource...), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyRemoteAdapterFile(path, manifest); err == nil {
		t.Fatal("modified adapter file was accepted")
	}
}
