//go:build windows

package processutil

import (
	"os/exec"
	"testing"
)

func TestConfigureBackgroundHidesConsoleWindow(t *testing.T) {
	command := exec.Command("git", "--version")
	ConfigureBackground(command)
	if command.SysProcAttr == nil || !command.SysProcAttr.HideWindow {
		t.Fatal("background command should hide its console window")
	}
	if command.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatal("background command should use CREATE_NO_WINDOW")
	}
}
