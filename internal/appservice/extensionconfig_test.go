package appservice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pi-desk/internal/domain"
)

func TestBundledPiDeskTodoResetsStateBeforeEachUserTurn(t *testing.T) {
	t.Parallel()
	content := string(bundledPiDeskTodoExtension)
	for _, expected := range []string{
		`const WIDGET_KEY = "pi-desk-todo"`,
		`pi.on("before_agent_start"`,
		"clearPreviousTurn(ctx)",
		"persistState()",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("bundled todo extension is missing %q", expected)
		}
	}
}

func TestBundledPiDeskTodoAlwaysProjectsNumericIDOrder(t *testing.T) {
	t.Parallel()
	content := string(bundledPiDeskTodoExtension)
	for _, expected := range []string{
		`function orderedTodos(): Todo[]`,
		`return [...todos].sort((left, right) => left.id - right.id)`,
		`todos = orderedTodos()`,
		`todos: ordered`,
		`text = ordered.length`,
		`? ordered.map((todo)`,
		`Always present todo items in numeric ID order (#1, #2, #3...) and never regroup completed items separately.`,
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("bundled todo extension does not enforce numeric order: missing %q", expected)
		}
	}
	if strings.Contains(content, `filter((todo) => !todo.done).map`) || strings.Contains(content, `filter((todo) => todo.done).map`) {
		t.Fatal("bundled todo extension must not group pending and completed items")
	}
}

func TestPiExtensionServiceListsGlobalConfiguredAndPackageExtensions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	agent := filepath.Join(root, "agent")
	extensions := filepath.Join(agent, "extensions")
	if err := os.MkdirAll(filepath.Join(extensions, "directory-extension"), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		filepath.Join(extensions, "local.ts"):                        "export default () => {}",
		filepath.Join(extensions, "plain.js"):                        "export default () => {}",
		filepath.Join(extensions, "ignored.txt"):                     "ignored",
		filepath.Join(extensions, "directory-extension", "index.ts"): "export default () => {}",
		filepath.Join(agent, "explicit.ts"):                          "export default () => {}",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	settings := `{
  "extensions": ["explicit.ts"],
  "packages": ["npm:context-mode", {"source":"npm:pi-subagents","extensions":["extensions/*.ts"]}]
}`
	if err := os.WriteFile(filepath.Join(agent, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}

	service := newPiExtensionService(agent, []byte("todo source\n"))
	snapshot, err := service.ListExtensions()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.GlobalDirectory != extensions || snapshot.SettingsPath != filepath.Join(agent, "settings.json") {
		t.Fatalf("unexpected snapshot paths %#v", snapshot)
	}
	if len(snapshot.Extensions) != 6 {
		t.Fatalf("expected 6 discovered entries, got %#v", snapshot.Extensions)
	}
	origins := map[domain.PiExtensionOrigin]int{}
	for _, extension := range snapshot.Extensions {
		origins[extension.Origin]++
	}
	if origins[domain.PiExtensionOriginGlobal] != 3 || origins[domain.PiExtensionOriginSettings] != 1 || origins[domain.PiExtensionOriginPackage] != 2 {
		t.Fatalf("unexpected origins %#v", origins)
	}
	if snapshot.Todo.Installed || snapshot.Todo.UpdateAvailable {
		t.Fatalf("todo should not be installed %#v", snapshot.Todo)
	}
}

func TestPiExtensionServiceInstallsUpdatesAndRemovesBundledTodo(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	agent := filepath.Join(root, "agent")
	extensions := filepath.Join(agent, "extensions")
	if err := os.MkdirAll(extensions, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(extensions, legacyTodoExtensionName)
	if err := os.WriteFile(legacyPath, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := newPiExtensionService(agent, []byte("current todo source\n"))

	before, err := service.ListExtensions()
	if err != nil || !before.Todo.LegacyInstalled || before.Todo.Installed {
		t.Fatalf("unexpected pre-install status %#v, %v", before.Todo, err)
	}
	installed, err := service.InstallPiDeskTodo()
	if err != nil {
		t.Fatal(err)
	}
	if !installed.ReplacedLegacy || !installed.Todo.Installed || installed.Todo.UpdateAvailable || installed.Todo.LegacyInstalled {
		t.Fatalf("unexpected install result %#v", installed)
	}
	content, err := os.ReadFile(filepath.Join(extensions, piDeskTodoExtensionName))
	if err != nil || string(content) != "current todo source\n" {
		t.Fatalf("unexpected installed source %q, %v", content, err)
	}
	if _, err := os.Stat(legacyPath + ".disabled-by-pi-desk"); err != nil {
		t.Fatalf("legacy backup was not retained: %v", err)
	}

	if err := os.WriteFile(filepath.Join(extensions, piDeskTodoExtensionName), []byte("user modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	outdated, err := service.ListExtensions()
	if err != nil || !outdated.Todo.UpdateAvailable {
		t.Fatalf("expected update status %#v, %v", outdated.Todo, err)
	}
	if _, err := service.InstallPiDeskTodo(); err != nil {
		t.Fatal(err)
	}
	if err := service.RemovePiDeskTodo(); err != nil {
		t.Fatal(err)
	}
	after, err := service.ListExtensions()
	if err != nil || after.Todo.Installed {
		t.Fatalf("unexpected removal status %#v, %v", after.Todo, err)
	}
}

func TestPiExtensionServiceRejectsInvalidSettings(t *testing.T) {
	t.Parallel()
	agent := filepath.Join(t.TempDir(), "agent")
	if err := os.MkdirAll(agent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agent, "settings.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := newPiExtensionService(agent, []byte("todo")).ListExtensions()
	if err == nil || !strings.Contains(err.Error(), "parse Pi settings") {
		t.Fatalf("expected settings parse error, got %v", err)
	}
}
