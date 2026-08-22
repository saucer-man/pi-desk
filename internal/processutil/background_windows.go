//go:build windows

package processutil

import (
	"os/exec"
	"strconv"
	"syscall"
)

const createNoWindow = 0x08000000

// ConfigureBackground prevents console subprocesses from flashing a window in
// the desktop application.
func ConfigureBackground(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow,
		HideWindow:    true,
	}
}

func TerminateTree(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	kill := exec.Command("taskkill.exe", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F")
	ConfigureBackground(kill)
	return kill.Run()
}
