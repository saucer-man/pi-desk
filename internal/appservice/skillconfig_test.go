package appservice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pi-desk/internal/domain"
	"pi-desk/internal/workspace"
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

func TestManagedSkillServiceDeletingRootSkillPreservesOtherSkills(t *testing.T) {
	t.Parallel()
	agentDirectory := filepath.Join(t.TempDir(), "agent")
	skillDirectory := filepath.Join(agentDirectory, "skills")
	sibling := filepath.Join(skillDirectory, "other", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(sibling), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(skillDirectory, "SKILL.md"), sibling} {
		if err := os.WriteFile(path, []byte("---\nname: review\ndescription: Review code\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	service := newManagedSkillService(agentDirectory, nil)
	if err := service.DeleteManagedSkill(domain.ManagedSkillRequest{Scope: domain.SkillScopeGlobal, RelativePath: "SKILL.md"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("deleting a root skill removed another skill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(skillDirectory, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("root skill was not deleted: %v", err)
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

func TestProjectSharedSkillsFollowRepoBoundaryAndStayReadOnly(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	project := filepath.Join(repo, "subdir")
	for _, directory := range []string{root, repo, project} {
		path := filepath.Join(directory, ".agents", "skills", filepath.Base(directory), "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("---\nname: "+filepath.Base(directory)+"\ndescription: Shared project skill\n---\nContent"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	service := newManagedSkillService(filepath.Join(root, "agent"), fakeWorkspaceResolver{record: workspace.Record{Path: project, Trust: "approve"}})
	snapshot, err := service.ListManagedSkills(domain.ListManagedSkillsRequest{WorkspacePath: project})
	if err != nil || len(snapshot.Skills) != 2 {
		t.Fatalf("project discovery: %#v %v", snapshot, err)
	}
	for _, skill := range snapshot.Skills {
		request := domain.ManagedSkillRequest{Scope: domain.SkillScopeProject, WorkspacePath: project, RootDirectory: skill.RootDirectory, RelativePath: skill.RelativePath}
		loaded, err := service.GetManagedSkill(request)
		if err != nil || !loaded.ReadOnly || !skill.ReadOnly {
			t.Fatalf("shared skill cannot be inspected read-only: %#v %v", loaded, err)
		}
		if _, err := service.UpdateManagedSkill(domain.UpdateManagedSkillRequest{Scope: request.Scope, WorkspacePath: project, RootDirectory: request.RootDirectory, RelativePath: request.RelativePath, Content: "changed"}); err == nil {
			t.Fatal("shared skill was writable")
		}
		if err := service.DeleteManagedSkill(request); err == nil {
			t.Fatal("shared skill was deletable")
		}
		content, _ := os.ReadFile(skill.Path)
		if !strings.Contains(string(content), "Content") {
			t.Fatal("shared skill changed")
		}
	}
	service.workspaces = fakeWorkspaceResolver{record: workspace.Record{Path: project, Trust: "deny"}}
	snapshot, err = service.ListManagedSkills(domain.ListManagedSkillsRequest{WorkspacePath: project})
	if err != nil || snapshot.ProjectEnabled || len(snapshot.Skills) != 0 {
		t.Fatalf("untrusted project skills leaked: %#v %v", snapshot, err)
	}
}
