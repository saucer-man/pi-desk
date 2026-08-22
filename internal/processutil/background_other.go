//go:build !windows

package processutil

import "os/exec"

func ConfigureBackground(_ *exec.Cmd) {}

func TerminateTree(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	return command.Process.Kill()
}
