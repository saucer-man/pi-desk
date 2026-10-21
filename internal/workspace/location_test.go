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

func TestTargetIdentityDriftIsAdopted(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	catalog := NewCatalog(statePath)
	original, err := catalog.RegisterTarget(testTargetRegistration())
	if err != nil {
		t.Fatal(err)
	}
	first, err := catalog.AddSSHWorkspace(testSSHWorkspaceRegistration(original.ID))
	if err != nil {
		t.Fatal(err)
	}
	registration := testSSHWorkspaceRegistration(original.ID)
	registration.RequestedRoot, registration.CanonicalRoot, registration.Inode = "/srv/other", "/srv/other", 21
	second, err := catalog.AddSSHWorkspace(registration)
	if err != nil {
		t.Fatal(err)
	}
	drifted := testTargetRegistration()
	drifted.ConfigFingerprint = strings.Repeat("b", 64)
	drifted.HostKeySHA256 = "SHA256:" + strings.Repeat("B", 43)
	updated, err := catalog.RegisterTarget(drifted)
	if err != nil {
		t.Fatalf("identity drift error = %v", err)
	}
	if updated.ID != original.ID || updated.HostKey.ConfigFingerprint != drifted.ConfigFingerprint {
		t.Fatalf("adopted target = %#v", updated)
	}
	persisted, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(persisted), drifted.ConfigFingerprint) {
		t.Fatalf("adopted binding was not persisted: %s", persisted)
	}
	if strings.Contains(string(persisted), original.HostKey.ConfigFingerprint) || strings.Contains(string(persisted), original.HostKey.SHA256) {
		t.Fatal("target refresh persisted stale workspace bindings")
	}
	targets, _ := catalog.ListTargets()
	if len(targets) != 1 || targets[0].HostKey.ConfigFingerprint != drifted.ConfigFingerprint {
		t.Fatalf("identity drift changed in-memory target: %#v", targets)
	}
	for _, current := range []*Catalog{catalog, NewCatalog(statePath)} {
		for _, originalWorkspace := range []Record{first, second} {
			record, err := current.ResolveID(originalWorkspace.ID)
			originalWorkspace.Location.SSH.HostKeyBinding = updated.HostKey
			if err != nil || record != originalWorkspace {
				t.Fatalf("target refresh did not preserve its workspace: record=%#v err=%v", record, err)
			}
		}
	}
}

func TestCatalogLoadsUnboundSSHWorkspaces(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	catalog := NewCatalog(statePath)
	target, err := catalog.RegisterTarget(testTargetRegistration())
	if err != nil {
		t.Fatal(err)
	}
	remote, err := catalog.AddSSHWorkspace(testSSHWorkspaceRegistration(target.ID))
	if err != nil {
		t.Fatal(err)
	}
	local, err := catalog.Add(t.TempDir(), "approve")
	if err != nil {
		t.Fatal(err)
	}
	desktop := DesktopRecord{ActiveThreadID: "thread-remote", Threads: []ThreadRecord{{
		ID: "thread-remote", Title: "Remote", WorkspaceID: remote.ID, Trust: "approve", Status: "idle",
	}}}
	if err := catalog.SaveDesktop(desktop); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state stateFile
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	drifted := target
	drifted.HostKey.ConfigFingerprint = strings.Repeat("b", 64)
	for _, test := range []struct {
		name    string
		targets []TargetRecord
	}{
		{"stale binding", []TargetRecord{drifted}},
		{"missing target", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			state.Targets = test.targets
			data, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(statePath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				reloaded := NewCatalog(statePath)
				for _, expected := range []Record{local, remote} {
					actual, err := reloaded.ResolveID(expected.ID)
					if err != nil || actual != expected {
						t.Fatalf("loading changed workspace identity: record=%#v err=%v", actual, err)
					}
				}
				restored, err := reloaded.Desktop()
				if err != nil || restored.ActiveThreadID != desktop.ActiveThreadID || len(restored.Threads) != 1 || restored.Threads[0] != desktop.Threads[0] {
					t.Fatalf("loading changed desktop threads: desktop=%#v err=%v", restored, err)
				}
				if len(test.targets) == 0 {
					if _, err := reloaded.ResolveTarget(target.ID); err == nil {
						t.Fatal("missing target was silently replaced")
					}
				}
			}
			after, err := os.ReadFile(statePath)
			if err != nil || string(after) != string(data) {
				t.Fatalf("loading rewrote saved bindings: %v", err)
			}
		})
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
