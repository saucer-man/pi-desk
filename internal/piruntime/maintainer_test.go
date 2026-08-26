package piruntime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"pi-desk/internal/domain"
)

type maintenanceCall struct {
	name string
	args []string
}

type maintenanceRunner struct {
	paths  map[string]string
	calls  []maintenanceCall
	output string
	err    error
}

func (runner *maintenanceRunner) LookPath(name string) (string, error) {
	if path := runner.paths[name]; path != "" {
		return path, nil
	}
	return "", errors.New("not found")
}

func (runner *maintenanceRunner) CombinedOutput(_ context.Context, name string, args ...string) ([]byte, error) {
	runner.calls = append(runner.calls, maintenanceCall{name: name, args: append([]string(nil), args...)})
	return []byte(runner.output), runner.err
}

func TestMaintainerUsesFixedPiUpdateArguments(t *testing.T) {
	runner := &maintenanceRunner{paths: map[string]string{"pi": "test-pi", "pi.exe": "test-pi"}, output: "updated\n"}
	maintainer := NewMaintainer(newLocator(runner))

	invocation, output, err := maintainer.Run(context.Background(), domain.PiUpdateSelf)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.PiPath != "test-pi" || output != "updated" {
		t.Fatalf("unexpected result: %#v %q", invocation, output)
	}
	if len(runner.calls) != 2 || runner.calls[1].name != "test-pi" {
		t.Fatalf("unexpected calls: %#v", runner.calls)
	}
	if got := runner.calls[1].args; len(got) != 2 || got[0] != "update" || got[1] != "--self" {
		t.Fatalf("unexpected update arguments: %#v", got)
	}
}

func TestMaintainerRejectsRemovedPackageAndCatalogActions(t *testing.T) {
	runner := &maintenanceRunner{paths: map[string]string{"pi": "test-pi", "pi.exe": "test-pi"}}
	maintainer := NewMaintainer(newLocator(runner))
	for _, action := range []domain.PiMaintenanceAction{"update-all", "update-extensions", "update-models"} {
		if _, _, err := maintainer.Run(context.Background(), action); err == nil || !strings.Contains(err.Error(), "unsupported Pi maintenance action") {
			t.Fatalf("action %q was not rejected: %v", action, err)
		}
	}
	for _, call := range runner.calls {
		if len(call.args) > 0 && call.args[0] == "update" {
			t.Fatalf("removed action executed a Pi update command: %#v", runner.calls)
		}
	}
}

func TestMaintainerInstallsOnlyWhenPiIsMissing(t *testing.T) {
	runner := &maintenanceRunner{paths: map[string]string{"npm": "test-npm", "npm.cmd": "test-npm"}}
	maintainer := NewMaintainer(newLocator(runner))
	if _, _, err := maintainer.Run(context.Background(), domain.PiInstall); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected npm install only, got %#v", runner.calls)
	}
	call := runner.calls[0]
	commandLine := strings.Join(call.args, " ")
	if !strings.Contains(commandLine, "install -g --ignore-scripts "+piPackageName) {
		t.Fatalf("unexpected npm invocation: %#v", call)
	}

	runner = &maintenanceRunner{paths: map[string]string{"pi": "test-pi", "pi.exe": "test-pi"}}
	maintainer = NewMaintainer(newLocator(runner))
	if _, _, err := maintainer.Run(context.Background(), domain.PiInstall); !errors.Is(err, ErrPiAlreadyInstalled) {
		t.Fatalf("expected installed error, got %v", err)
	}
}

func TestMaintainerRejectsPiUpdatesWhenPiIsUnavailable(t *testing.T) {
	runner := &maintenanceRunner{paths: map[string]string{}}
	maintainer := NewMaintainer(newLocator(runner))
	if _, _, err := maintainer.Run(context.Background(), domain.PiUpdateSelf); !errors.Is(err, ErrPiNotInstalled) {
		t.Fatalf("expected not installed error, got %v", err)
	}
}
