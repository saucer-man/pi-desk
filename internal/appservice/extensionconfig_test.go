package appservice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pi-desk/internal/domain"
	"pi-desk/internal/workspace"
)

type fakePiPackageRunner struct {
	directory string
	args      []string
}

func (runner *fakePiPackageRunner) Run(_ context.Context, directory string, args ...string) (string, error) {
	runner.directory = directory
	runner.args = append([]string(nil), args...)
	return "ok", nil
}

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

func TestBundledPiDeskGoalImplementsGoalLoop(t *testing.T) {
	t.Parallel()
	content := string(bundledPiDeskGoalExtension)
	for _, expected := range []string{
		`const WIDGET_KEY = "pi-desk-goal"`,
		`const ENTRY_TYPE = "pi-desk-goal"`,
		`pi.registerCommand("goal"`,
		`name: GOAL_COMPLETE_TOOL`,
		`name: GOAL_BLOCKED_TOOL`,
		"goal_id does not match the current goal",
		`pi.on("agent_settled"`,
		`pi.sendUserMessage(buildGoalPrompt(goal, lead), { deliverAs: "followUp" })`,
		"goal.tokenBudget !== undefined && goal.tokensUsed >= goal.tokenBudget",
		"MAX_NO_PROGRESS_TURNS",
		"MAX_AUTO_TURNS",
		"persistState()",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("bundled goal extension is missing %q", expected)
		}
	}
	if strings.Contains(content, "goal_wait") {
		t.Fatal("bundled goal extension must not register a wait tool")
	}
}

func TestPiExtensionServiceInstallsUpdatesAndRemovesBundledGoal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	agent := filepath.Join(root, "agent")
	extensions := filepath.Join(agent, "extensions")
	if err := os.MkdirAll(extensions, 0o700); err != nil {
		t.Fatal(err)
	}
	service := newPiExtensionService(agent, []byte("todo source\n"))

	before, err := service.ListExtensions()
	if err != nil || before.Goal.Installed {
		t.Fatalf("unexpected pre-install goal status %#v, %v", before.Goal, err)
	}
	installed, err := service.InstallPiDeskGoal()
	if err != nil {
		t.Fatal(err)
	}
	if !installed.Installed || installed.UpdateAvailable || installed.Path == "" {
		t.Fatalf("unexpected goal install result %#v", installed)
	}
	content, err := os.ReadFile(filepath.Join(extensions, piDeskGoalExtensionName))
	if err != nil || len(content) == 0 {
		t.Fatalf("goal extension was not written: %v", err)
	}
	if err := os.WriteFile(filepath.Join(extensions, piDeskGoalExtensionName), []byte("user modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	outdated, err := service.ListExtensions()
	if err != nil || !outdated.Goal.UpdateAvailable {
		t.Fatalf("expected goal update status %#v, %v", outdated.Goal, err)
	}
	if _, err := service.InstallPiDeskGoal(); err != nil {
		t.Fatal(err)
	}
	if err := service.RemovePiDeskGoal(); err != nil {
		t.Fatal(err)
	}
	after, err := service.ListExtensions()
	if err != nil || after.Goal.Installed || after.Goal.UpdateAvailable {
		t.Fatalf("unexpected goal removal status %#v, %v", after.Goal, err)
	}
}

func TestBundledPiDeskComputerUseGuardsDesktopControl(t *testing.T) {
	t.Parallel()
	content := string(bundledPiDeskComputerUseExtension)
	for _, expected := range []string{
		`name: "computer_screenshot"`,
		`name: "computer_click"`,
		`name: "computer_type"`,
		`name: "computer_key"`,
		`name: "computer_scroll"`,
		`name: "computer_window"`,
		"ctx.ui.confirm(",
		"Desktop control was not authorized for this session",
		"signal?.aborted",
		"process.platform !== \"win32\"",
		"Get-Clipboard",
		"Set-Clipboard",
		"SetProcessDPIAware",
		"SetCursorPos",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("bundled computer use extension is missing %q", expected)
		}
	}
	if strings.Contains(content, "TypeText") {
		t.Fatal("bundled computer use extension must not type through the input method editor")
	}
}

func TestPiExtensionServiceInstallsUpdatesAndRemovesBundledComputerUse(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	agent := filepath.Join(root, "agent")
	extensions := filepath.Join(agent, "extensions")
	if err := os.MkdirAll(extensions, 0o700); err != nil {
		t.Fatal(err)
	}
	service := newPiExtensionService(agent, []byte("todo source\n"))

	before, err := service.ListExtensions()
	if err != nil || before.ComputerUse.Installed {
		t.Fatalf("unexpected pre-install computer use status %#v, %v", before.ComputerUse, err)
	}
	installed, err := service.InstallPiDeskComputerUse()
	if err != nil {
		t.Fatal(err)
	}
	if !installed.Installed || installed.UpdateAvailable || installed.Path == "" {
		t.Fatalf("unexpected computer use install result %#v", installed)
	}
	content, err := os.ReadFile(filepath.Join(extensions, piDeskComputerUseExtensionName))
	if err != nil || len(content) == 0 {
		t.Fatalf("computer use extension was not written: %v", err)
	}
	if err := os.WriteFile(filepath.Join(extensions, piDeskComputerUseExtensionName), []byte("user modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	outdated, err := service.ListExtensions()
	if err != nil || !outdated.ComputerUse.UpdateAvailable {
		t.Fatalf("expected computer use update status %#v, %v", outdated.ComputerUse, err)
	}
	if _, err := service.InstallPiDeskComputerUse(); err != nil {
		t.Fatal(err)
	}
	if err := service.RemovePiDeskComputerUse(); err != nil {
		t.Fatal(err)
	}
	after, err := service.ListExtensions()
	if err != nil || after.ComputerUse.Installed || after.ComputerUse.UpdateAvailable {
		t.Fatalf("unexpected computer use removal status %#v, %v", after.ComputerUse, err)
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

func TestPiExtensionServiceManagesGlobalAndTrustedProjectPackages(t *testing.T) {
	root := t.TempDir()
	agent := filepath.Join(root, "agent")
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(project, ".pi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(agent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agent, "settings.json"), []byte(`{"theme":"dark","packages":["npm:global"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".pi", "settings.json"), []byte(`{"packages":[{"source":"npm:project","extensions":[],"skills":[],"prompts":[],"themes":[]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog := workspace.NewCatalog(filepath.Join(root, "state.json"))
	if _, err := catalog.Add(project, "approve"); err != nil {
		t.Fatal(err)
	}
	runner := &fakePiPackageRunner{}
	service := &PiExtensionService{agentDirectory: agent, todoSource: []byte("todo"), workspaces: catalog, packageRunner: runner}

	snapshot, err := service.ListPackages(domain.ListPiPackagesRequest{WorkspacePath: project})
	if err != nil || !snapshot.ProjectEnabled || len(snapshot.Packages) != 2 {
		t.Fatalf("unexpected package snapshot %#v, %v", snapshot, err)
	}
	if !snapshot.Packages[0].Enabled || snapshot.Packages[1].Enabled {
		t.Fatalf("unexpected package states %#v", snapshot.Packages)
	}
	if err := service.SetPackageEnabled(domain.SetPiPackageEnabledRequest{PiPackageRequest: domain.PiPackageRequest{
		Source: "npm:global", Scope: domain.PiPackageScopeGlobal,
	}, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	disabled, err := listPiPackages(filepath.Join(agent, "settings.json"), domain.PiPackageScopeGlobal)
	if err != nil || len(disabled) != 1 || disabled[0].Enabled {
		t.Fatalf("global package was not disabled %#v, %v", disabled, err)
	}
	settings, _, err := readPiPackageSettings(filepath.Join(agent, "settings.json"))
	if err != nil || string(settings["theme"]) != `"dark"` {
		t.Fatalf("unknown settings were not preserved %#v, %v", settings, err)
	}
	if _, err := service.InstallPackage(domain.PiPackageRequest{Source: "npm:new", Scope: domain.PiPackageScopeProject, WorkspacePath: project}); err != nil {
		t.Fatal(err)
	}
	if runner.directory != project || strings.Join(runner.args, " ") != "install npm:new -l" {
		t.Fatalf("unexpected project package command dir=%q args=%q", runner.directory, runner.args)
	}
}

func TestPiExtensionServiceRejectsUnsafeProjectPackagePath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".pi"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := workspace.NewCatalog(filepath.Join(root, "state.json"))
	if _, err := catalog.Add(project, "approve"); err != nil {
		t.Fatal(err)
	}
	service := &PiExtensionService{agentDirectory: filepath.Join(root, "agent"), workspaces: catalog}

	snapshot, err := service.ListPackages(domain.ListPiPackagesRequest{WorkspacePath: project})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ProjectEnabled || !strings.Contains(snapshot.ProjectNotice, "real directory") {
		t.Fatalf("unsafe project package path was accepted: %#v", snapshot)
	}
}
