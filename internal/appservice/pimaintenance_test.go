package appservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	"pi-desk/internal/domain"
	"pi-desk/internal/piruntime"
)

type fakePiMaintainer struct {
	action     domain.PiMaintenanceAction
	invocation piruntime.Invocation
	output     string
	err        error
}

func (maintainer *fakePiMaintainer) Run(_ context.Context, action domain.PiMaintenanceAction) (piruntime.Invocation, string, error) {
	maintainer.action = action
	return maintainer.invocation, maintainer.output, maintainer.err
}

func TestPiMaintenanceRunsFixedActionAndRefreshesRuntime(t *testing.T) {
	maintainer := &fakePiMaintainer{invocation: piruntime.Invocation{PiPath: "C:/tools/pi.exe"}, output: "updated"}
	prepared := false
	released := false
	service := newPiMaintenanceService(maintainer, fakeProber{status: domain.PiRuntimeStatus{State: domain.RuntimeReady, Version: "0.90.0"}}, func() (func(), error) {
		prepared = true
		return func() { released = true }, nil
	})

	result, err := service.MaintainPi(domain.PiMaintenanceRequest{Action: domain.PiUpdateSelf})
	if err != nil {
		t.Fatal(err)
	}
	if !prepared || !released || maintainer.action != domain.PiUpdateSelf || result.Output != "updated" || result.Command != "C:/tools/pi.exe" || result.Runtime.Version != "0.90.0" {
		t.Fatalf("unexpected maintenance result: action=%q result=%#v", maintainer.action, result)
	}
}

func TestPiMaintenanceRefusesToRunWhenSessionsCannotBeStopped(t *testing.T) {
	maintainer := &fakePiMaintainer{}
	stopErr := errors.New("unable to stop Pi sessions")
	service := newPiMaintenanceService(maintainer, fakeProber{}, func() (func(), error) { return nil, stopErr })
	if _, err := service.MaintainPi(domain.PiMaintenanceRequest{Action: domain.PiUpdateSelf}); !errors.Is(err, stopErr) {
		t.Fatalf("expected session shutdown error, got %v", err)
	}
	if maintainer.action != "" {
		t.Fatalf("maintainer should not run: %#v", maintainer)
	}
}

func TestPiMaintenanceReturnsCommandOutputWithFailure(t *testing.T) {
	maintainer := &fakePiMaintainer{output: "registry unavailable", err: errors.New("exit status 1")}
	service := newPiMaintenanceService(maintainer, fakeProber{}, nil)
	result, err := service.MaintainPi(domain.PiMaintenanceRequest{Action: domain.PiUpdateSelf})
	if err == nil || result.Output != "registry unavailable" || err.Error() != "exit status 1: registry unavailable" {
		t.Fatalf("unexpected failure: result=%#v err=%v", result, err)
	}
}

func TestPiMaintenanceBoundsErrorMessage(t *testing.T) {
	maintainer := &fakePiMaintainer{output: strings.Repeat("x", maxMaintenanceErrorOutput+100), err: errors.New("failed")}
	service := newPiMaintenanceService(maintainer, fakeProber{}, nil)
	_, err := service.MaintainPi(domain.PiMaintenanceRequest{Action: domain.PiUpdateSelf})
	if err == nil || len(err.Error()) > maxMaintenanceErrorOutput+100 || !strings.Contains(err.Error(), "truncated by Pi Desk") {
		t.Fatalf("maintenance error was not bounded: len=%d err=%v", len(err.Error()), err)
	}
}
