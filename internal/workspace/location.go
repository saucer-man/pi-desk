package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Kind string

const (
	KindLocal Kind = "local"
	KindSSH   Kind = "ssh"
)

const (
	maxTargetNameRunes = 100
	maxHostAliasBytes  = 512
	maxRemotePathBytes = 4096
)

var (
	ErrTargetIdentityChanged = errors.New("SSH target identity changed")
	sha256FingerprintPattern = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)
	hexDigestPattern         = regexp.MustCompile(`^[a-f0-9]{64}$`)
	hostKeyAlgorithmPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+@:-]{0,127}$`)
)

type LocalLocation struct {
	CanonicalPath string `json:"canonicalPath"`
}

type HostKeyBinding struct {
	Algorithm         string `json:"algorithm"`
	SHA256            string `json:"sha256"`
	ConfigFingerprint string `json:"configFingerprint"`
}

type SSHLocation struct {
	TargetID       string         `json:"targetId"`
	RequestedRoot  string         `json:"requestedRoot"`
	CanonicalRoot  string         `json:"canonicalRoot"`
	Device         uint64         `json:"device"`
	Inode          uint64         `json:"inode"`
	RemoteOS       string         `json:"remoteOS"`
	RemoteArch     string         `json:"remoteArch"`
	HostKeyBinding HostKeyBinding `json:"hostKeyBinding"`
}

// Location is a value-form discriminated union. Only the member selected by
// Kind is serialized to state; the other member must remain zero.
type Location struct {
	Kind  Kind
	Local LocalLocation
	SSH   SSHLocation
}

type TargetRecord struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	HostAlias       string         `json:"hostAlias"`
	HostKey         HostKeyBinding `json:"hostKey"`
	AddedAt         time.Time      `json:"addedAt"`
	LastConnectedAt time.Time      `json:"lastConnectedAt"`
}

type stateLocation struct {
	Kind  Kind           `json:"kind"`
	Local *LocalLocation `json:"local,omitempty"`
	SSH   *SSHLocation   `json:"ssh,omitempty"`
}

type stateWorkspaceRecord struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Path         string        `json:"path,omitempty"` // v1-v5 migration input only
	Location     stateLocation `json:"location,omitempty"`
	Trust        string        `json:"trust"`
	AddedAt      time.Time     `json:"addedAt"`
	LastOpenedAt time.Time     `json:"lastOpenedAt"`
}

func stateRecordFromRecord(record Record) (stateWorkspaceRecord, error) {
	if err := validateRecord(record); err != nil {
		return stateWorkspaceRecord{}, err
	}
	location := stateLocation{Kind: record.Location.Kind}
	switch record.Location.Kind {
	case KindLocal:
		local := record.Location.Local
		location.Local = &local
	case KindSSH:
		ssh := record.Location.SSH
		location.SSH = &ssh
	}
	return stateWorkspaceRecord{
		ID: record.ID, Name: record.Name, Location: location, Trust: record.Trust,
		AddedAt: record.AddedAt, LastOpenedAt: record.LastOpenedAt,
	}, nil
}

func (record stateWorkspaceRecord) toRecord(version int) (Record, error) {
	result := Record{
		ID: record.ID, Name: record.Name, Trust: record.Trust,
		AddedAt: record.AddedAt, LastOpenedAt: record.LastOpenedAt,
	}
	if version < 6 {
		if strings.TrimSpace(record.Path) == "" {
			return Record{}, errors.New("legacy local workspace path is required")
		}
		result.Path = record.Path
		result.Location = Location{Kind: KindLocal, Local: LocalLocation{CanonicalPath: record.Path}}
		return result, nil
	}
	result.Location.Kind = record.Location.Kind
	switch record.Location.Kind {
	case KindLocal:
		if record.Location.Local == nil || record.Location.SSH != nil {
			return Record{}, errors.New("invalid local workspace location union")
		}
		result.Location.Local = *record.Location.Local
		result.Path = record.Location.Local.CanonicalPath
	case KindSSH:
		if record.Location.SSH == nil || record.Location.Local != nil {
			return Record{}, errors.New("invalid SSH workspace location union")
		}
		result.Location.SSH = *record.Location.SSH
	default:
		return Record{}, errors.New("workspace location kind is invalid")
	}
	return result, validateRecord(result)
}

func validateRecord(record Record) error {
	if !validIdentity("workspace", record.ID) {
		return errors.New("workspace identity is invalid")
	}
	if !validDisplayName(record.Name) {
		return errors.New("workspace name is invalid")
	}
	if record.Trust != "approve" && record.Trust != "deny" {
		return errors.New("workspace trust must be approve or deny")
	}
	switch record.Location.Kind {
	case KindLocal:
		if record.Location.Local.CanonicalPath == "" || !filepath.IsAbs(record.Location.Local.CanonicalPath) || filepath.Clean(record.Location.Local.CanonicalPath) != record.Location.Local.CanonicalPath || record.Path != record.Location.Local.CanonicalPath || record.Location.SSH != (SSHLocation{}) {
			return errors.New("local workspace location is invalid")
		}
	case KindSSH:
		if record.Path != "" || record.Location.Local != (LocalLocation{}) {
			return errors.New("SSH workspace cannot expose a local path")
		}
		if err := validateSSHLocation(record.Location.SSH); err != nil {
			return err
		}
	default:
		return errors.New("workspace location kind is invalid")
	}
	return nil
}

func validateTarget(record TargetRecord) error {
	if !validIdentity("target", record.ID) {
		return errors.New("target identity is invalid")
	}
	if !validDisplayName(record.Name) {
		return errors.New("target name is invalid")
	}
	if err := validateHostAlias(record.HostAlias); err != nil {
		return err
	}
	return validateHostKeyBinding(record.HostKey)
}

func validateSSHLocation(location SSHLocation) error {
	if !validIdentity("target", location.TargetID) {
		return errors.New("SSH workspace target identity is invalid")
	}
	if err := validatePOSIXAbsolutePath(location.RequestedRoot); err != nil {
		return err
	}
	if err := validatePOSIXAbsolutePath(location.CanonicalRoot); err != nil {
		return err
	}
	if location.Device == 0 || location.Inode == 0 {
		return errors.New("SSH workspace root identity is invalid")
	}
	if (location.RemoteOS != "linux" && location.RemoteOS != "darwin") || (location.RemoteArch != "amd64" && location.RemoteArch != "arm64") {
		return errors.New("SSH workspace platform is unsupported")
	}
	return validateHostKeyBinding(location.HostKeyBinding)
}

func validateHostKeyBinding(binding HostKeyBinding) error {
	if !hostKeyAlgorithmPattern.MatchString(binding.Algorithm) {
		return errors.New("host-key algorithm is invalid")
	}
	if !sha256FingerprintPattern.MatchString(binding.SHA256) || !hexDigestPattern.MatchString(binding.ConfigFingerprint) {
		return errors.New("host-key binding is invalid")
	}
	return nil
}

func validateHostAlias(value string) error {
	if value == "" || len(value) > maxHostAliasBytes || !utf8.ValidString(value) || strings.HasPrefix(value, "-") || strings.ContainsAny(value, "*?") || strings.ContainsFunc(value, invalidIdentityRune) {
		return errors.New("SSH host alias is invalid")
	}
	return nil
}

func validatePOSIXAbsolutePath(value string) error {
	if value == "" || len(value) > maxRemotePathBytes || !utf8.ValidString(value) || !strings.HasPrefix(value, "/") || path.Clean(value) != value {
		return errors.New("remote workspace path is invalid")
	}
	if strings.ContainsFunc(value, func(character rune) bool { return character == 0 || unicode.IsControl(character) }) {
		return errors.New("remote workspace path is invalid")
	}
	return nil
}

func validDisplayName(value string) bool {
	return strings.TrimSpace(value) != "" && len([]rune(value)) <= maxTargetNameRunes && !strings.ContainsFunc(value, unicode.IsControl)
}

func targetAliasKey(value string) string {
	return strings.ToLower(value)
}

func invalidIdentityRune(character rune) bool {
	return unicode.IsControl(character) || unicode.IsSpace(character)
}

// NewWorkspaceID mints an opaque catalog identity for a root that has passed
// explicit SSH Connect consent but is not trusted or persisted yet.
func NewWorkspaceID() (string, error) {
	return newIdentity("workspace")
}

func newIdentity(prefix string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate %s identity: %w", prefix, err)
	}
	return prefix + "-" + hex.EncodeToString(value[:]), nil
}

func validIdentity(prefix, value string) bool {
	if !strings.HasPrefix(value, prefix+"-") || len(value) != len(prefix)+1+32 {
		return false
	}
	digest := value[len(prefix)+1:]
	_, err := hex.DecodeString(digest)
	return err == nil && strings.ToLower(digest) == digest
}
