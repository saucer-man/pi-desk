package appservice

import (
	"os"
	"path/filepath"
	"testing"

	"pi-desk/internal/domain"
	"pi-desk/internal/workspace"
)

func TestPromptTemplateServiceListsNativeGlobalAndProjectDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	agentDirectory := filepath.Join(root, "agent")
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(agentDirectory, "prompts"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".pi", "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDirectory, "prompts", "review.md"), []byte("---\ndescription: Review changes\nargument-hint: <path>\n---\nReview $1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".pi", "prompts", "release.md"), []byte("Create a release summary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := workspace.NewCatalog(filepath.Join(root, "state.json"))
	if _, err := catalog.Add(project, "approve"); err != nil {
		t.Fatal(err)
	}

	service := newPromptTemplateService(agentDirectory, catalog)
	snapshot, err := service.ListPromptTemplates(domain.ListPromptTemplatesRequest{WorkspacePath: project})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.ProjectEnabled || len(snapshot.Templates) != 2 {
		t.Fatalf("expected global and project templates, got %#v", snapshot)
	}
	if snapshot.Templates[0].Name != "review" || snapshot.Templates[0].ArgumentHint != "<path>" {
		t.Fatalf("unexpected global template %#v", snapshot.Templates[0])
	}
	if snapshot.Templates[1].Name != "release" || snapshot.Templates[1].Description != "Create a release summary" {
		t.Fatalf("unexpected project template %#v", snapshot.Templates[1])
	}
}

func TestPromptTemplateServiceRejectsUntrustedProjectResources(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	catalog := workspace.NewCatalog(filepath.Join(root, "state.json"))
	if _, err := catalog.Add(project, "deny"); err != nil {
		t.Fatal(err)
	}
	service := newPromptTemplateService(filepath.Join(root, "agent"), catalog)

	snapshot, err := service.ListPromptTemplates(domain.ListPromptTemplatesRequest{WorkspacePath: project})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ProjectEnabled || snapshot.ProjectNotice == "" {
		t.Fatalf("expected disabled project prompts, got %#v", snapshot)
	}
	_, err = service.UpsertPromptTemplate(domain.UpsertPromptTemplateRequest{
		Scope: domain.PromptTemplateScopeProject, WorkspacePath: project, Name: "review", Content: "Review\n",
	})
	if err == nil {
		t.Fatal("expected untrusted project to reject writes")
	}
}

func TestPromptTemplateServiceWritesRenamesReadsAndDeletesTemplate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	service := newPromptTemplateService(filepath.Join(root, "agent"), nil)
	created, err := service.UpsertPromptTemplate(domain.UpsertPromptTemplateRequest{
		Scope: domain.PromptTemplateScopeGlobal, Name: "review", Content: "---\ndescription: Review code\n---\nReview $@\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Description != "Review code" {
		t.Fatalf("unexpected created template %#v", created)
	}
	renamed, err := service.UpsertPromptTemplate(domain.UpsertPromptTemplateRequest{
		Scope: domain.PromptTemplateScopeGlobal, OriginalName: "review", Name: "audit", Content: "Audit the repository\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "audit" || renamed.Description != "Audit the repository" {
		t.Fatalf("unexpected renamed template %#v", renamed)
	}
	loaded, err := service.GetPromptTemplate(domain.PromptTemplateRequest{Scope: domain.PromptTemplateScopeGlobal, Name: "audit"})
	if err != nil || loaded.Content != "Audit the repository\n" {
		t.Fatalf("unexpected loaded template %#v, %v", loaded, err)
	}
	if err := service.DeletePromptTemplate(domain.PromptTemplateRequest{Scope: domain.PromptTemplateScopeGlobal, Name: "audit"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetPromptTemplate(domain.PromptTemplateRequest{Scope: domain.PromptTemplateScopeGlobal, Name: "audit"}); err == nil {
		t.Fatal("expected deleted prompt to be unavailable")
	}
}

func TestPromptTemplateServiceRejectsUnsafeTemplateName(t *testing.T) {
	t.Parallel()
	service := newPromptTemplateService(filepath.Join(t.TempDir(), "agent"), nil)
	_, err := service.UpsertPromptTemplate(domain.UpsertPromptTemplateRequest{
		Scope: domain.PromptTemplateScopeGlobal, Name: "../outside", Content: "never write outside\n",
	})
	if err == nil {
		t.Fatal("expected path traversal name to be rejected")
	}
}
