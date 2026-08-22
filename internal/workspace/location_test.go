package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testTargetRegistration() TargetRegistration {
	return TargetRegistration{
		Name:              "Build host",
		HostAlias:         "build-prod",
		ConfigFingerprint: strings.Repeat("a", 64),
		HostKeyAlgorithm:  "ssh-ed25519",
		HostKeySHA256:     "SHA256:" + strings.Repeat("A", 43),
	}
}

func testSSHWorkspaceRegistration(targetID string) SSHWorkspaceRegistration {
	return SSHWorkspaceRegistration{
		Name: "repository", TargetID: targetID, RequestedRoot: "/srv/repository", CanonicalRoot: "/srv/repository",
		Device: 10, Inode: 20, RemoteOS: "linux", RemoteArch: "amd64", Trust: "approve",
	}
}

func TestCatalogMigratesVersionFiveToRandomLocationIdentity(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	statePath := filepath.Join(root, "state.json")
	legacyID := "workspace-0123456789abcdef01234567"
	legacy := `{
  "version": 5,
  "workspaces": [{"id":"` + legacyID + `","name":"project","path":` + quotedJSON(project) + `,"trust":"approve","addedAt":"2026-08-10T12:00:00Z","lastOpenedAt":"2026-08-10T12:00:00Z"}],
  "desktop": {"activeThreadId":"thread-1","threads":[{"id":"thread-1","title":"Audit","workspacePath":` + quotedJSON(project) + `,"trust":"approve","status":"idle"}]}
}`
	if err := os.WriteFile(statePath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := NewCatalog(statePath)
	records, err := catalog.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ID == legacyID || !validIdentity("workspace", records[0].ID) || records[0].Location.Kind != KindLocal || records[0].Location.Local.CanonicalPath != project || records[0].Path != project {
		t.Fatalf("unexpected migrated workspace: %#v", records)
	}
	desktop, err := catalog.Desktop()
	if err != nil {
		t.Fatal(err)
	}
	if len(desktop.Threads) != 1 || desktop.Threads[0].WorkspaceID != records[0].ID {
		t.Fatalf("thread was not bound to migrated WorkspaceID: %#v", desktop)
	}
	persisted, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(persisted, &state); err != nil {
		t.Fatal(err)
	}
	if state["version"] != float64(6) {
		t.Fatalf("migration did not persist version six: %s", persisted)
	}
	workspaceState := state["workspaces"].([]any)[0].(map[string]any)
	if _, legacyPath := workspaceState["path"]; legacyPath || workspaceState["location"].(map[string]any)["kind"] != "local" {
		t.Fatalf("migration retained legacy path shape: %s", persisted)
	}
	reloaded, err := NewCatalog(statePath).List()
	if err != nil || len(reloaded) != 1 || reloaded[0].ID != records[0].ID {
		t.Fatalf("migrated identity was not stable: %#v err=%v", reloaded, err)
	}
}

func TestSSHWorkspaceRegistrationAcceptsHostMintedIdentity(t *testing.T) {
	catalog := NewCatalog(filepath.Join(t.TempDir(), "state.json"))
	target, err := catalog.RegisterTarget(testTargetRegistration())
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := "workspace-0123456789abcdef0123456789abcdef"
	record, err := catalog.AddSSHWorkspace(SSHWorkspaceRegistration{
		WorkspaceID: workspaceID, Name: "Remote", TargetID: target.ID,
		RequestedRoot: "/srv/repository", CanonicalRoot: "/srv/repository",
		Device: 7, Inode: 11, RemoteOS: "linux", RemoteArch: "amd64", Trust: "approve",
	})
	if err != nil || record.ID != workspaceID {
		t.Fatalf("record=%#v err=%v", record, err)
	}
	duplicate, err := catalog.AddSSHWorkspace(SSHWorkspaceRegistration{
		WorkspaceID: "workspace-ffffffffffffffffffffffffffffffff", Name: "Remote", TargetID: target.ID,
		RequestedRoot: "/srv/repository", CanonicalRoot: "/srv/repository",
		Device: 7, Inode: 11, RemoteOS: "linux", RemoteArch: "amd64", Trust: "approve",
	})
	if err != nil || duplicate.ID != workspaceID {
		t.Fatalf("duplicate root was not idempotently reused: record=%#v err=%v", duplicate, err)
	}
}

func TestCatalogPersistsHiddenSSHTargetAndWorkspaceIdentity(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	clock := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	catalog := NewCatalog(statePath)
	catalog.now = func() time.Time { return clock }
	target, err := catalog.RegisterTarget(testTargetRegistration())
	if err != nil {
		t.Fatal(err)
	}
	if !validIdentity("target", target.ID) || target.HostAlias != "build-prod" || target.AddedAt != clock {
		t.Fatalf("unexpected target: %#v", target)
	}
	workspaceRecord, err := catalog.AddSSHWorkspace(testSSHWorkspaceRegistration(target.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !validIdentity("workspace", workspaceRecord.ID) || workspaceRecord.Path != "" || workspaceRecord.Location.Kind != KindSSH || workspaceRecord.Location.SSH.TargetID != target.ID || workspaceRecord.Location.SSH.HostKeyBinding != target.HostKey {
		t.Fatalf("unexpected SSH workspace: %#v", workspaceRecord)
	}
	clock = clock.Add(time.Hour)
	duplicate, err := catalog.AddSSHWorkspace(testSSHWorkspaceRegistration(target.ID))
	if err != nil || duplicate.ID != workspaceRecord.ID || duplicate.LastOpenedAt != clock {
		t.Fatalf("duplicate root identity was not reused: %#v err=%v", duplicate, err)
	}

	reloaded := NewCatalog(statePath)
	targets, err := reloaded.ListTargets()
	if err != nil || len(targets) != 1 || targets[0] != target {
		t.Fatalf("unexpected reloaded targets: %#v err=%v", targets, err)
	}
	records, err := reloaded.List()
	if err != nil || len(records) != 1 || records[0] != duplicate {
		t.Fatalf("unexpected reloaded workspaces: %#v err=%v", records, err)
	}
	persisted, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"password", "privateKey", "proxyCommand", "stderr", "remoteContent"} {
		if strings.Contains(string(persisted), forbidden) {
			t.Fatalf("state contains forbidden field %q: %s", forbidden, persisted)
		}
	}
}

func TestTargetAliasMatchingIsCaseInsensitive(t *testing.T) {
	catalog := NewCatalog(filepath.Join(t.TempDir(), "state.json"))
	first, err := catalog.RegisterTarget(testTargetRegistration())
	if err != nil {
		t.Fatal(err)
	}
	matching := testTargetRegistration()
	matching.HostAlias = "BUILD-PROD"
	matching.Name = "Renamed build host"
	second, err := catalog.RegisterTarget(matching)
	if err != nil || second.ID != first.ID || second.Name != matching.Name {
		t.Fatalf("case-insensitive alias was duplicated: first=%#v second=%#v err=%v", first, second, err)
	}
	if targets, _ := catalog.ListTargets(); len(targets) != 1 {
		t.Fatalf("case-insensitive alias created %d targets", len(targets))
	}
}

func TestTargetIdentityDriftDoesNotMutateState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	catalog := NewCatalog(statePath)
	original, err := catalog.RegisterTarget(testTargetRegistration())
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	drifted := testTargetRegistration()
	drifted.ConfigFingerprint = strings.Repeat("b", 64)
	if _, err := catalog.RegisterTarget(drifted); !errors.Is(err, ErrTargetIdentityChanged) {
		t.Fatalf("identity drift error = %v", err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("identity drift mutated state\nbefore=%s\nafter=%s", before, after)
	}
	targets, _ := catalog.ListTargets()
	if len(targets) != 1 || targets[0] != original {
		t.Fatalf("identity drift changed in-memory target: %#v", targets)
	}
}

func TestRemoteCatalogRemovalCallbackRunsBeforePersistence(t *testing.T) {
	catalog := NewCatalog(filepath.Join(t.TempDir(), "state.json"))
	target, err := catalog.RegisterTarget(testTargetRegistration())
	if err != nil {
		t.Fatal(err)
	}
	workspaceRecord, err := catalog.AddSSHWorkspace(testSSHWorkspaceRegistration(target.ID))
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(catalog.path)
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop before persistence")
	called := false
	if err := catalog.RemoveAfter(workspaceRecord.ID, func(Record) error { called = true; return stop }); !errors.Is(err, stop) || !called {
		t.Fatalf("remove callback called=%v err=%v", called, err)
	}
	after, _ := os.ReadFile(catalog.path)
	if string(after) != string(before) {
		t.Fatal("failed remove callback mutated state")
	}
	if records, _ := catalog.List(); len(records) != 1 {
		t.Fatalf("failed remove callback changed records: %#v", records)
	}
}

func TestRemoveSSHTargetRequiresWorkspaceRemovalAndUsesWorkspaceID(t *testing.T) {
	catalog := NewCatalog(filepath.Join(t.TempDir(), "state.json"))
	target, err := catalog.RegisterTarget(testTargetRegistration())
	if err != nil {
		t.Fatal(err)
	}
	workspaceRecord, err := catalog.AddSSHWorkspace(testSSHWorkspaceRegistration(target.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveDesktop(DesktopRecord{ActiveThreadID: "thread-remote", Threads: []ThreadRecord{{
		ID: "thread-remote", Title: "Remote", WorkspaceID: workspaceRecord.ID, WorkspacePath: "", Trust: "approve", Status: "idle",
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveDesktop(DesktopRecord{ActiveThreadID: "thread-remote", Threads: []ThreadRecord{{
		ID: "thread-remote", Title: "Remote renamed", WorkspacePath: "", Trust: "approve", Status: "idle",
	}}}); err == nil {
		t.Fatal("remote desktop thread without WorkspaceID was accepted")
	}
	if err := catalog.SaveDesktop(DesktopRecord{ActiveThreadID: "thread-remote", Threads: []ThreadRecord{{
		ID: "thread-remote", Title: "Remote path", WorkspaceID: workspaceRecord.ID, WorkspacePath: "local-anchor-marker", Trust: "approve", Status: "idle",
	}}}); err == nil {
		t.Fatal("remote desktop thread projected a local anchor path")
	}
	if err := catalog.RemoveTarget(target.ID); err == nil {
		t.Fatal("referenced target was removed")
	}
	if err := catalog.Remove(workspaceRecord.ID); err != nil {
		t.Fatal(err)
	}
	desktop, err := catalog.Desktop()
	if err != nil || len(desktop.Threads) != 0 || desktop.ActiveThreadID != "" {
		t.Fatalf("workspace removal left remote thread: %#v err=%v", desktop, err)
	}
	if err := catalog.RemoveTarget(target.ID); err != nil {
		t.Fatalf("remove unreferenced target: %v", err)
	}
}

func TestCatalogRejectsInvalidVersionSixLocationUnion(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	invalid := `{
  "version": 6,
  "workspaces": [{
    "id":"workspace-0123456789abcdef0123456789abcdef","name":"bad","trust":"deny",
    "location":{"kind":"local","local":{"canonicalPath":"/tmp/repo"},"ssh":{"targetId":"target-0123456789abcdef0123456789abcdef"}}
  }],
  "desktop":{"threads":[]}
}`
	if err := os.WriteFile(statePath, []byte(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCatalog(statePath).List(); err == nil {
		t.Fatal("invalid location union was accepted")
	}
}

func quotedJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
