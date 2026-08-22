package remotessh

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/mod/semver"

	"pi-desk/internal/remoteprotocol"
)

const (
	helperManifestVersion  = 1
	maxHelperManifestBytes = 64 << 10
	maxHelperArtifactBytes = 64 << 20
	maxBuildIdentityBytes  = 128
)

var (
	ErrHelperManifestInvalid     = errors.New("remote helper manifest is invalid")
	ErrHelperArtifactUnsupported = errors.New("remote helper artifact is unsupported")
	ErrHelperArtifactIntegrity   = errors.New("remote helper artifact integrity check failed")
	ErrHelperPiIncompatible      = errors.New("remote helper artifact is incompatible with this Pi version")
)

// HelperArtifact describes one immutable, locally packaged helper binary.
// SHA256 is the exact binary digest, while BuildIdentity is the fixed build
// value verified by the helper hello response after it starts.
type HelperArtifact struct {
	ProtocolVersion uint16 `json:"protocolVersion"`
	OS              string `json:"os"`
	Architecture    string `json:"architecture"`
	Size            int64  `json:"size"`
	SHA256          string `json:"sha256"`
	BuildIdentity   string `json:"buildIdentity"`
	PiVersionMin    string `json:"piVersionMin"`
	PiVersionMax    string `json:"piVersionMax"`
}

// HelperManifest is the immutable release description bundled alongside the
// four helper binaries. It contains no target alias, host identity, remote
// path, credential, or user-controlled URL.
type HelperManifest struct {
	Version   uint16           `json:"version"`
	Artifacts []HelperArtifact `json:"artifacts"`
}

// ParseHelperManifest accepts a bounded, strict JSON release manifest. The
// manifest must contain exactly the supported POSIX remote targets so a
// release never has a platform-dependent fallback artifact.
func ParseHelperManifest(content []byte) (HelperManifest, error) {
	if len(content) == 0 || len(content) > maxHelperManifestBytes || !utf8.Valid(content) {
		return HelperManifest{}, ErrHelperManifestInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var manifest HelperManifest
	if err := decoder.Decode(&manifest); err != nil {
		return HelperManifest{}, fmt.Errorf("%w: decode", ErrHelperManifestInvalid)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return HelperManifest{}, fmt.Errorf("%w: trailing data", ErrHelperManifestInvalid)
	}
	if err := manifest.Validate(); err != nil {
		return HelperManifest{}, err
	}
	return manifest, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return errors.New("JSON document has trailing data")
}

// Validate checks the release metadata without opening an artifact file.
func (manifest HelperManifest) Validate() error {
	if manifest.Version != helperManifestVersion || len(manifest.Artifacts) != 4 {
		return ErrHelperManifestInvalid
	}
	expected := map[string]struct{}{
		"linux/amd64":  {},
		"linux/arm64":  {},
		"darwin/amd64": {},
		"darwin/arm64": {},
	}
	for _, artifact := range manifest.Artifacts {
		if err := artifact.Validate(); err != nil {
			return err
		}
		key := artifact.OS + "/" + artifact.Architecture
		if _, ok := expected[key]; !ok {
			return ErrHelperManifestInvalid
		}
		delete(expected, key)
	}
	if len(expected) != 0 {
		return ErrHelperManifestInvalid
	}
	return nil
}

// Validate checks static metadata for a single artifact. The maximum is a
// package budget, not a trust decision for arbitrary caller-supplied files.
func (artifact HelperArtifact) Validate() error {
	if artifact.ProtocolVersion != remoteprotocol.Version || artifact.Size <= 0 || artifact.Size > maxHelperArtifactBytes {
		return ErrHelperManifestInvalid
	}
	if (artifact.OS != "linux" && artifact.OS != "darwin") || (artifact.Architecture != "amd64" && artifact.Architecture != "arm64") {
		return ErrHelperManifestInvalid
	}
	if len(artifact.SHA256) != sha256.Size*2 || strings.ToLower(artifact.SHA256) != artifact.SHA256 {
		return ErrHelperManifestInvalid
	}
	if _, err := hex.DecodeString(artifact.SHA256); err != nil {
		return ErrHelperManifestInvalid
	}
	if err := validateBuildIdentity(artifact.BuildIdentity); err != nil {
		return err
	}
	minVersion, err := normalizePiVersion(artifact.PiVersionMin)
	if err != nil {
		return err
	}
	maxVersion, err := normalizePiVersion(artifact.PiVersionMax)
	if err != nil || semver.Compare(minVersion, maxVersion) >= 0 {
		return ErrHelperManifestInvalid
	}
	return nil
}

// SelectHelperArtifact chooses an exact POSIX helper for a remote platform and
// a local Pi version. No architecture, version, or protocol fallback exists.
func (manifest HelperManifest) SelectHelperArtifact(goos, architecture, piVersion string) (HelperArtifact, error) {
	if err := manifest.Validate(); err != nil {
		return HelperArtifact{}, err
	}
	version, err := normalizePiVersion(piVersion)
	if err != nil {
		return HelperArtifact{}, ErrHelperPiIncompatible
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.OS != goos || artifact.Architecture != architecture {
			continue
		}
		minVersion, _ := normalizePiVersion(artifact.PiVersionMin)
		maxVersion, _ := normalizePiVersion(artifact.PiVersionMax)
		if semver.Compare(version, minVersion) < 0 || semver.Compare(version, maxVersion) >= 0 {
			return HelperArtifact{}, ErrHelperPiIncompatible
		}
		return artifact, nil
	}
	return HelperArtifact{}, ErrHelperArtifactUnsupported
}

// VerifyContent proves that the supplied local binary is the exact artifact
// selected from the manifest before any SSH/SFTP process is started.
func (artifact HelperArtifact) VerifyContent(content []byte) error {
	if err := artifact.Validate(); err != nil {
		return err
	}
	if int64(len(content)) != artifact.Size {
		return ErrHelperArtifactIntegrity
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != artifact.SHA256 {
		return ErrHelperArtifactIntegrity
	}
	return nil
}

func validateBuildIdentity(value string) error {
	if value == "" || len(value) > maxBuildIdentityBytes || !utf8.ValidString(value) {
		return ErrHelperManifestInvalid
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return ErrHelperManifestInvalid
		}
	}
	return nil
}

func normalizePiVersion(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "v") {
		value = "v" + value
	}
	if !semver.IsValid(value) {
		return "", ErrHelperManifestInvalid
	}
	return value, nil
}
