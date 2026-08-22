package remotessh

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"pi-desk/internal/remoteprotocol"
)

func helperArtifactForTest(goos, architecture string, content []byte) HelperArtifact {
	digest := sha256.Sum256(content)
	return HelperArtifact{
		ProtocolVersion: remoteprotocol.Version,
		OS:              goos,
		Architecture:    architecture,
		Size:            int64(len(content)),
		SHA256:          hex.EncodeToString(digest[:]),
		BuildIdentity:   "test-build-20260819",
		PiVersionMin:    "0.84.2",
		PiVersionMax:    "0.85.0",
	}
}

func helperManifestForTest(content []byte) HelperManifest {
	return HelperManifest{
		Version: helperManifestVersion,
		Artifacts: []HelperArtifact{
			helperArtifactForTest("linux", "amd64", content),
			helperArtifactForTest("linux", "arm64", content),
			helperArtifactForTest("darwin", "amd64", content),
			helperArtifactForTest("darwin", "arm64", content),
		},
	}
}

func TestHelperManifestRoundTripAndSelection(t *testing.T) {
	content := []byte("remote helper test binary")
	manifest := helperManifestForTest(content)
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseHelperManifest(encoded)
	if err != nil {
		t.Fatalf("ParseHelperManifest returned error: %v", err)
	}
	artifact, err := parsed.SelectHelperArtifact("linux", "arm64", "0.84.2")
	if err != nil {
		t.Fatalf("SelectHelperArtifact returned error: %v", err)
	}
	if artifact.OS != "linux" || artifact.Architecture != "arm64" {
		t.Fatalf("unexpected artifact: %#v", artifact)
	}
	if err := artifact.VerifyContent(content); err != nil {
		t.Fatalf("VerifyContent returned error: %v", err)
	}
}

func TestHelperManifestRejectsInvalidMetadata(t *testing.T) {
	content := []byte("remote helper test binary")
	tests := []struct {
		name   string
		mutate func(*HelperManifest)
		want   error
	}{
		{
			name:   "missing target",
			mutate: func(manifest *HelperManifest) { manifest.Artifacts = manifest.Artifacts[:3] },
			want:   ErrHelperManifestInvalid,
		},
		{
			name: "duplicate target",
			mutate: func(manifest *HelperManifest) {
				manifest.Artifacts[3].OS, manifest.Artifacts[3].Architecture = "linux", "amd64"
			},
			want: ErrHelperManifestInvalid,
		},
		{
			name:   "wrong protocol",
			mutate: func(manifest *HelperManifest) { manifest.Artifacts[0].ProtocolVersion++ },
			want:   ErrHelperManifestInvalid,
		},
		{
			name:   "unsafe identity",
			mutate: func(manifest *HelperManifest) { manifest.Artifacts[0].BuildIdentity = "build id" },
			want:   ErrHelperManifestInvalid,
		},
		{
			name: "invalid compatibility range",
			mutate: func(manifest *HelperManifest) {
				manifest.Artifacts[0].PiVersionMax = manifest.Artifacts[0].PiVersionMin
			},
			want: ErrHelperManifestInvalid,
		},
		{
			name:   "oversized artifact",
			mutate: func(manifest *HelperManifest) { manifest.Artifacts[0].Size = maxHelperArtifactBytes + 1 },
			want:   ErrHelperManifestInvalid,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := helperManifestForTest(content)
			test.mutate(&candidate)
			if err := candidate.Validate(); !errors.Is(err, test.want) {
				t.Fatalf("Validate error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParseHelperManifestRejectsUnknownAndTrailingJSON(t *testing.T) {
	content := []byte("remote helper test binary")
	encoded, err := json.Marshal(helperManifestForTest(content))
	if err != nil {
		t.Fatal(err)
	}
	unknown := append([]byte(nil), encoded[:len(encoded)-1]...)
	unknown = append(unknown, []byte(`,"unexpected":true}`)...)
	if _, err := ParseHelperManifest(unknown); !errors.Is(err, ErrHelperManifestInvalid) {
		t.Fatalf("unknown field error = %v", err)
	}
	if _, err := ParseHelperManifest(append(encoded, []byte(" {}")...)); !errors.Is(err, ErrHelperManifestInvalid) {
		t.Fatalf("trailing JSON error = %v", err)
	}
}

func TestHelperArtifactSelectionAndIntegrityFailClosed(t *testing.T) {
	content := []byte("remote helper test binary")
	manifest := helperManifestForTest(content)
	if _, err := manifest.SelectHelperArtifact("windows", "amd64", "0.84.2"); !errors.Is(err, ErrHelperArtifactUnsupported) {
		t.Fatalf("unsupported target error = %v", err)
	}
	if _, err := manifest.SelectHelperArtifact("linux", "amd64", "0.85.0"); !errors.Is(err, ErrHelperPiIncompatible) {
		t.Fatalf("incompatible Pi error = %v", err)
	}
	if _, err := manifest.SelectHelperArtifact("linux", "amd64", "not-a-version"); !errors.Is(err, ErrHelperPiIncompatible) {
		t.Fatalf("malformed Pi version error = %v", err)
	}
	artifact := manifest.Artifacts[0]
	if err := artifact.VerifyContent([]byte("modified helper test binary")); !errors.Is(err, ErrHelperArtifactIntegrity) {
		t.Fatalf("modified content error = %v", err)
	}
}

func FuzzParseHelperManifest(f *testing.F) {
	content := []byte("remote helper test binary")
	seed, err := json.Marshal(helperManifestForTest(content))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte(`{"version":1,"artifacts":[]}`))
	f.Fuzz(func(t *testing.T, value []byte) {
		manifest, err := ParseHelperManifest(value)
		if err == nil {
			if err := manifest.Validate(); err != nil {
				t.Fatalf("parsed invalid manifest: %#v, %v", manifest, err)
			}
		}
	})
}
