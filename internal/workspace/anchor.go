package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/natefinch/atomic"
)

const (
	anchorFormatVersion  = 1
	anchorMarkerName     = "workspace.json"
	maxAnchorMarkerBytes = 4 << 10
)

var (
	ErrAnchorInvalid = errors.New("SSH workspace anchor is invalid")
	ErrAnchorRebind  = errors.New("SSH workspace anchor cannot be rebound")
)

type AnchorMarker struct {
	Format      uint16 `json:"format"`
	WorkspaceID string `json:"workspaceId"`
}

func DefaultAnchorRoot() (string, error) {
	statePath, err := DefaultStatePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(statePath), "remote-anchors"), nil
}

func AnchorDirectory(root, workspaceID string) (string, error) {
	if !validIdentity("workspace", workspaceID) {
		return "", ErrAnchorInvalid
	}
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." || !filepath.IsAbs(root) {
		return "", ErrAnchorInvalid
	}
	return filepath.Join(root, workspaceID), nil
}

// EnsureSSHAnchor creates or verifies an immutable local marker. Existing
// markers are never rewritten, and any extra entry makes the anchor invalid.
func EnsureSSHAnchor(root, workspaceID string) (string, error) {
	directory, err := AnchorDirectory(root, workspaceID)
	if err != nil {
		return "", err
	}
	if err := ensurePlainDirectory(root, 0o700); err != nil {
		return "", err
	}
	if err := ensurePlainDirectory(directory, 0o700); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", fmt.Errorf("%w: read anchor directory", ErrAnchorInvalid)
	}
	for _, entry := range entries {
		if entry.Name() != anchorMarkerName || entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return "", ErrAnchorInvalid
		}
	}
	if len(entries) == 1 {
		marker, err := ReadSSHAnchor(root, directory)
		if err != nil {
			return "", err
		}
		if marker.WorkspaceID != workspaceID {
			return "", ErrAnchorRebind
		}
		return directory, nil
	}
	marker := AnchorMarker{Format: anchorFormatVersion, WorkspaceID: workspaceID}
	content, err := json.Marshal(marker)
	if err != nil {
		return "", ErrAnchorInvalid
	}
	content = append(content, '\n')
	markerPath := filepath.Join(directory, anchorMarkerName)
	if err := atomic.WriteFile(markerPath, bytes.NewReader(content)); err != nil {
		return "", fmt.Errorf("write SSH anchor marker: %w", err)
	}
	if err := os.Chmod(markerPath, 0o600); err != nil {
		return "", fmt.Errorf("protect SSH anchor marker: %w", err)
	}
	return directory, nil
}

// ReadSSHAnchor reads only a direct child of the configured anchor root and
// rejects symlinks, extra files, unknown JSON fields, and rebind attempts.
func ReadSSHAnchor(root, directory string) (AnchorMarker, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	directory = filepath.Clean(strings.TrimSpace(directory))
	if root == "" || directory == "" || !filepath.IsAbs(root) || !filepath.IsAbs(directory) {
		return AnchorMarker{}, ErrAnchorInvalid
	}
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.Contains(relative, string(filepath.Separator)) || !validIdentity("workspace", relative) {
		return AnchorMarker{}, ErrAnchorInvalid
	}
	if err := verifyPlainDirectory(root); err != nil {
		return AnchorMarker{}, err
	}
	if err := verifyPlainDirectory(directory); err != nil {
		return AnchorMarker{}, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != anchorMarkerName || entries[0].Type()&os.ModeSymlink != 0 || entries[0].IsDir() {
		return AnchorMarker{}, ErrAnchorInvalid
	}
	markerPath := filepath.Join(directory, anchorMarkerName)
	info, err := os.Lstat(markerPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxAnchorMarkerBytes {
		return AnchorMarker{}, ErrAnchorInvalid
	}
	content, err := os.ReadFile(markerPath)
	if err != nil || len(content) > maxAnchorMarkerBytes {
		return AnchorMarker{}, ErrAnchorInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var marker AnchorMarker
	if err := decoder.Decode(&marker); err != nil {
		return AnchorMarker{}, ErrAnchorInvalid
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return AnchorMarker{}, ErrAnchorInvalid
	}
	if marker.Format != anchorFormatVersion || marker.WorkspaceID != relative || !validIdentity("workspace", marker.WorkspaceID) {
		return AnchorMarker{}, ErrAnchorRebind
	}
	return marker, nil
}

func ensurePlainDirectory(directory string, mode os.FileMode) error {
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(directory, mode); err != nil {
			return fmt.Errorf("create SSH anchor directory: %w", err)
		}
		info, err = os.Lstat(directory)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrAnchorInvalid
	}
	if err := os.Chmod(directory, mode); err != nil {
		return fmt.Errorf("protect SSH anchor directory: %w", err)
	}
	return nil
}

func verifyPlainDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrAnchorInvalid
	}
	return nil
}
