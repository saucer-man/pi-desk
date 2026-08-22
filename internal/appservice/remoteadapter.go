package appservice

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strings"

	"golang.org/x/mod/semver"
)

const remoteAdapterProtocol = "pi-desk.remote-tools.v1"

var remoteAdapterCoverage = []string{"bash", "edit", "find", "grep", "ls", "read", "user_bash", "write"}

//go:embed resources/pi-desk-remote.manifest.json
var remoteAdapterManifestSource []byte

type remoteAdapterManifest struct {
	Format          uint16 `json:"format"`
	Protocol        string `json:"protocol"`
	Size            int    `json:"size"`
	SHA256          string `json:"sha256"`
	PiCompatibility struct {
		MinInclusive string `json:"minInclusive"`
		MaxExclusive string `json:"maxExclusive"`
	} `json:"piCompatibility"`
	Coverage []string `json:"coverage"`
}

func verifyRemoteAdapterBundle(piVersion string) (remoteAdapterManifest, error) {
	if len(remoteAdapterManifestSource) == 0 || len(remoteAdapterManifestSource) > 4<<10 {
		return remoteAdapterManifest{}, errors.New("remote adapter manifest is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(remoteAdapterManifestSource))
	decoder.DisallowUnknownFields()
	var manifest remoteAdapterManifest
	if err := decoder.Decode(&manifest); err != nil {
		return remoteAdapterManifest{}, errors.New("remote adapter manifest is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return remoteAdapterManifest{}, errors.New("remote adapter manifest is invalid")
	}
	if manifest.Format != 1 || manifest.Protocol != remoteAdapterProtocol || manifest.Size != len(remoteAdapterSource) || !validAdapterDigest(manifest.SHA256) || !slices.Equal(manifest.Coverage, remoteAdapterCoverage) {
		return remoteAdapterManifest{}, errors.New("remote adapter bundle identity is invalid")
	}
	digest := sha256.Sum256(remoteAdapterSource)
	if hex.EncodeToString(digest[:]) != manifest.SHA256 {
		return remoteAdapterManifest{}, errors.New("remote adapter bundle hash is invalid")
	}
	version := "v" + strings.TrimPrefix(strings.TrimSpace(piVersion), "v")
	minimum := "v" + strings.TrimPrefix(strings.TrimSpace(manifest.PiCompatibility.MinInclusive), "v")
	maximum := "v" + strings.TrimPrefix(strings.TrimSpace(manifest.PiCompatibility.MaxExclusive), "v")
	if !semver.IsValid(version) || !semver.IsValid(minimum) || !semver.IsValid(maximum) || semver.Compare(minimum, maximum) >= 0 || semver.Compare(version, minimum) < 0 || semver.Compare(version, maximum) >= 0 {
		return remoteAdapterManifest{}, errors.New("installed Pi version is incompatible with the remote adapter")
	}
	return manifest, nil
}

func validRemoteAdapterHandshake(manifest remoteAdapterManifest, protocol string, coverage []string) bool {
	return protocol == manifest.Protocol && slices.Equal(coverage, manifest.Coverage)
}

func verifyRemoteAdapterFile(path string, manifest remoteAdapterManifest) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != int64(manifest.Size) {
		return errors.New("remote adapter file identity is invalid")
	}
	content, err := os.ReadFile(path)
	if err != nil || len(content) != manifest.Size {
		return errors.New("remote adapter file is invalid")
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != manifest.SHA256 {
		return errors.New("remote adapter file hash is invalid")
	}
	return nil
}

func validAdapterDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
