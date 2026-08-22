package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSSHAnchorRoundTripAndImmutableBinding(t *testing.T) {
	root := filepath.Join(t.TempDir(), "remote-anchors")
	workspaceID, err := newIdentity("workspace")
	if err != nil {
		t.Fatal(err)
	}
	directory, err := EnsureSSHAnchor(root, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if directory != filepath.Join(root, workspaceID) {
		t.Fatalf("anchor directory = %q", directory)
	}
	marker, err := ReadSSHAnchor(root, directory)
	if err != nil || marker.Format != anchorFormatVersion || marker.WorkspaceID != workspaceID {
		t.Fatalf("anchor marker = %#v, %v", marker, err)
	}
	if repeated, err := EnsureSSHAnchor(root, workspaceID); err != nil || repeated != directory {
		t.Fatalf("idempotent anchor ensure = %q, %v", repeated, err)
	}
	markerInfo, err := os.Stat(filepath.Join(directory, anchorMarkerName))
	if err != nil || (runtime.GOOS != "windows" && markerInfo.Mode().Perm()&0o022 != 0) {
		t.Fatalf("anchor marker permissions = %#o, %v", markerInfo.Mode().Perm(), err)
	}
}

func TestSSHAnchorRejectsRebindExtraEntriesAndSymlinks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "remote-anchors")
	workspaceID, _ := newIdentity("workspace")
	directory, err := EnsureSSHAnchor(root, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	otherID, _ := newIdentity("workspace")
	if err := os.WriteFile(filepath.Join(directory, anchorMarkerName), []byte(`{"format":1,"workspaceId":"`+otherID+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSSHAnchor(root, directory); !errors.Is(err, ErrAnchorRebind) {
		t.Fatalf("rebound marker error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "unexpected"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureSSHAnchor(root, workspaceID); !errors.Is(err, ErrAnchorInvalid) {
		t.Fatalf("extra anchor entry error = %v", err)
	}

	if err := os.Symlink(directory, filepath.Join(root, otherID)); err == nil {
		if _, err := ReadSSHAnchor(root, filepath.Join(root, otherID)); !errors.Is(err, ErrAnchorInvalid) {
			t.Fatalf("symlink anchor error = %v", err)
		}
	}
}

func TestSSHAnchorRejectsOutsideAndUnknownMarkerFields(t *testing.T) {
	root := filepath.Join(t.TempDir(), "remote-anchors")
	workspaceID, _ := newIdentity("workspace")
	directory, err := EnsureSSHAnchor(root, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSSHAnchor(root, filepath.Dir(root)); !errors.Is(err, ErrAnchorInvalid) {
		t.Fatalf("outside anchor error = %v", err)
	}
	content := `{"format":1,"workspaceId":"` + workspaceID + `","target":"secret"}`
	if err := os.WriteFile(filepath.Join(directory, anchorMarkerName), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSSHAnchor(root, directory); !errors.Is(err, ErrAnchorInvalid) {
		t.Fatalf("unknown marker field error = %v", err)
	}
}
