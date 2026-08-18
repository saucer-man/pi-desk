//go:build windows

package piruntime

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const createNewProcessGroup = 0x00000200

func configureProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup,
		HideWindow:    true,
	}
}

func killProcessTree(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	killer := exec.Command("taskkill.exe", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F")
	killer.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := killer.Run(); err == nil {
		return nil
	}
	if err := command.Process.Kill(); err != nil && !processAlreadyExited(err) {
		return err
	}
	return nil
}

func processAlreadyExited(err error) bool {
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.EINVAL)
}
