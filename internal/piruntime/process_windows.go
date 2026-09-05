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
	attributes := &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup,
		HideWindow:    true,
	}
	// invocationForPath hands cmd.exe a pre-quoted /c payload. Go's default
	// argument escaping rewrites the payload quotes to \", which cmd.exe does
	// not understand, so shim paths with spaces (C:\Program Files\...) fail
	// with a localized "not recognized" error. Deliver the raw command line.
	if len(command.Args) == 5 && command.Args[1] == "/d" && command.Args[2] == "/s" && command.Args[3] == "/c" {
		attributes.CmdLine = quoteCMDArgument(command.Path) + ` /d /s /c "` + command.Args[4] + `"`
	}
	command.SysProcAttr = attributes
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
