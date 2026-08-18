package main

import (
	"testing"

	"pi-desk/internal/domain"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestCenteredWindowStateFitsFirstLaunchToPrimaryWorkArea(t *testing.T) {
	screens := []*application.Screen{{
		ID: "primary", IsPrimary: true,
		WorkArea: application.Rect{X: 0, Y: 0, Width: 1536, Height: 824},
	}}

	got := centeredWindowState(1440, 900, screens)
	want := domain.WindowState{X: 48, Y: 0, Width: 1440, Height: 824, Valid: true}
	if got != want {
		t.Fatalf("centered state = %#v, want %#v", got, want)
	}
}

func TestCenteredWindowStateDefersToPlatformWithoutScreenData(t *testing.T) {
	if got := centeredWindowState(1440, 900, nil); got.Valid {
		t.Fatalf("centered state without screens = %#v, want invalid platform default", got)
	}
}

func TestFirstLaunchMaximisesOnCompactHighDPILogicalWorkArea(t *testing.T) {
	screens := []*application.Screen{{
		ID: "primary", IsPrimary: true,
		WorkArea: application.Rect{X: 0, Y: 0, Width: 1536, Height: 816},
	}}
	state := centeredWindowState(defaultWindowWidth, defaultWindowHeight, screens)

	if !shouldMaximiseFirstLaunch(state, screens) {
		t.Fatalf("first launch state %#v should maximise on compact work area", state)
	}
}

func TestFirstLaunchStaysNormalOnRoomyWorkArea(t *testing.T) {
	screens := []*application.Screen{{
		ID: "primary", IsPrimary: true,
		WorkArea: application.Rect{X: 0, Y: 0, Width: 2560, Height: 1400},
	}}
	state := centeredWindowState(defaultWindowWidth, defaultWindowHeight, screens)

	if shouldMaximiseFirstLaunch(state, screens) {
		t.Fatalf("first launch state %#v should stay normal on roomy work area", state)
	}
}

func TestConstrainWindowStateRaisesTinySavedBoundsToMinimum(t *testing.T) {
	screens := []*application.Screen{{
		ID: "primary", IsPrimary: true,
		WorkArea: application.Rect{X: 0, Y: 0, Width: 1920, Height: 1040},
	}}
	state := domain.WindowState{X: 1800, Y: 900, Width: 420, Height: 300, Valid: true}

	got := constrainWindowState(state, screens)
	want := domain.WindowState{X: 940, Y: 360, Width: minimumWindowWidth, Height: minimumWindowHeight, Valid: true}
	if got != want {
		t.Fatalf("constrained tiny state = %#v, want %#v", got, want)
	}
}

func TestConstrainWindowStateKeepsWindowInsideCurrentWorkArea(t *testing.T) {
	screens := []*application.Screen{{
		ID: "primary", IsPrimary: true,
		WorkArea: application.Rect{X: 0, Y: 0, Width: 1920, Height: 1040},
	}}
	state := domain.WindowState{X: -24, Y: -38, Width: 2000, Height: 1100, Maximized: true, Valid: true}

	got := constrainWindowState(state, screens)
	want := domain.WindowState{X: 0, Y: 0, Width: 1920, Height: 1040, Maximized: true, Valid: true}
	if got != want {
		t.Fatalf("constrained state = %#v, want %#v", got, want)
	}
}

func TestConstrainWindowStatePreservesValidSecondaryMonitorPlacement(t *testing.T) {
	screens := []*application.Screen{
		{ID: "primary", IsPrimary: true, WorkArea: application.Rect{X: 0, Y: 0, Width: 1920, Height: 1040}},
		{ID: "left", WorkArea: application.Rect{X: -1280, Y: 40, Width: 1280, Height: 984}},
	}
	state := domain.WindowState{X: -1200, Y: 80, Width: 1100, Height: 800, Valid: true}

	if got := constrainWindowState(state, screens); got != state {
		t.Fatalf("valid secondary placement changed from %#v to %#v", state, got)
	}
}

func TestConstrainWindowStateCentersDetachedWindowOnPrimaryScreen(t *testing.T) {
	screens := []*application.Screen{
		{ID: "primary", IsPrimary: true, WorkArea: application.Rect{X: 0, Y: 24, Width: 1600, Height: 876}},
		{ID: "right", WorkArea: application.Rect{X: 1600, Y: 0, Width: 1920, Height: 1040}},
	}
	state := domain.WindowState{X: 5200, Y: -900, Width: 1200, Height: 700, Valid: true}

	got := constrainWindowState(state, screens)
	if got.X != 200 || got.Y != 112 || got.Width != 1200 || got.Height != 700 {
		t.Fatalf("detached state = %#v, want centered primary placement", got)
	}
}

func TestConstrainWindowStateLeavesInvalidStateUntouched(t *testing.T) {
	state := domain.WindowState{X: -5000, Y: -5000, Width: 0, Height: 0}
	screens := []*application.Screen{{ID: "primary", IsPrimary: true, WorkArea: application.Rect{Width: 1920, Height: 1040}}}

	if got := constrainWindowState(state, screens); got != state {
		t.Fatalf("invalid state changed from %#v to %#v", state, got)
	}
}
