package appservice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pi-desk/internal/domain"
)

func TestManagedSkillServiceUsesPiGlobalSkillRules(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	agentDirectory := filepath.Join(root, ".pi", "agent")
	sharedDirectory := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(filepath.Join(agentDirectory, "skills", "nested", "review"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sharedDirectory, "shared-review"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDirectory, "skills", "root.md"), []byte("---\nname: root-skill\ndescription: A root skill\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDirectory, "skills", "nested", "review", "SKILL.md"), []byte("---\nname: code-review\ndescription: Review code safely\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sharedDirectory, "ignored.md"), []byte("---\nname: ignored\ndescription: Ignore root markdown\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sharedDirectory, "shared-review", "SKILL.md"), []byte("---\nname: shared-review\ndescription: Shared review skill\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := newManagedSkillService(agentDirectory, nil)

	snapshot, err := service.ListManagedSkills(domain.ListManagedSkillsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.GlobalDirectories) != 2 || len(snapshot.Skills) != 3 {
		t.Fatalf("unexpected skill snapshot %#v", snapshot)
	}
	if snapshot.Skills[0].RootDirectory != filepath.Join(agentDirectory, "skills") {
		t.Fatalf("Pi skill root was not preserved %#v", snapshot.Skills[0])
	}
	shared := snapshot.Skills[2]
	if shared.Name != "shared-review" || shared.RootDirectory != sharedDirectory {
		t.Fatalf("shared skill was not discovered %#v", shared)
	}
	loaded, err := service.GetManagedSkill(domain.ManagedSkillRequest{
		Scope: domain.SkillScopeGlobal, RootDirectory: shared.RootDirectory, RelativePath: shared.RelativePath,
	})
	if err != nil || !strings.Contains(loaded.Content, "Shared review skill") {
		t.Fatalf("shared skill was not loaded from its real root %#v, %v", loaded, err)
	}
}

func TestManagedSkillServiceCreatesUpdatesAndDeletesPiSkill(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	service := newManagedSkillService(filepath.Join(root, "agent"), nil)
	created, err := service.CreateManagedSkill(domain.CreateManagedSkillRequest{
		Scope: domain.SkillScopeGlobal, Name: "code-review", Description: "Review source code before merging",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Kind != "directory" || created.RelativePath != filepath.Join("code-review", "SKILL.md") {
		t.Fatalf("unexpected created skill %#v", created)
	}
	updated, err := service.UpdateManagedSkill(domain.UpdateManagedSkillRequest{
		Scope: domain.SkillScopeGlobal, RelativePath: created.RelativePath,
		Content: "---\nname: code-review\ndescription: Review changed code\n---\n\n# Code review\n",
	})
	if err != nil || updated.Description != "Review changed code" {
		t.Fatalf("unexpected updated skill %#v, %v", updated, err)
	}
	loaded, err := service.GetManagedSkill(domain.ManagedSkillRequest{Scope: domain.SkillScopeGlobal, RelativePath: created.RelativePath})
	if err != nil || !strings.Contains(loaded.Content, "# Code review") {
		t.Fatalf("unexpected loaded skill %#v, %v", loaded, err)
	}
	if err := service.DeleteManagedSkill(domain.ManagedSkillRequest{Scope: domain.SkillScopeGlobal, RelativePath: created.RelativePath}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetManagedSkill(domain.ManagedSkillRequest{Scope: domain.SkillScopeGlobal, RelativePath: created.RelativePath}); err == nil {
		t.Fatal("expected deleted skill to be unavailable")
	}
}

func TestManagedSkillServiceRejectsUnsafeSkillPathAndUnknownRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	service := newManagedSkillService(filepath.Join(root, ".pi", "agent"), nil)
	if _, err := service.GetManagedSkill(domain.ManagedSkillRequest{Scope: domain.SkillScopeGlobal, RelativePath: "..\\outside\\SKILL.md"}); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
	if _, err := service.GetManagedSkill(domain.ManagedSkillRequest{
		Scope: domain.SkillScopeGlobal, RootDirectory: filepath.Join(root, "outside"), RelativePath: "review\\SKILL.md",
	}); err == nil {
		t.Fatal("expected an unknown global root to be rejected")
	}
}
