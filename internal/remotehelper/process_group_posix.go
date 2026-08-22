//go:build linux || darwin

package remotehelper

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateProcessGroup(pid int) {
	if pid <= 0 {
		return
	}
	for _, step := range []struct {
		signal syscall.Signal
		wait   time.Duration
	}{{syscall.SIGINT, 250 * time.Millisecond}, {syscall.SIGTERM, 250 * time.Millisecond}, {syscall.SIGKILL, 0}} {
		err := syscall.Kill(-pid, step.signal)
		if errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone) {
			return
		}
		if step.wait > 0 {
			time.Sleep(step.wait)
		}
	}
}

func terminateTerminalGroup(pid int) {
	if pid <= 0 {
		return
	}
	for _, step := range []struct {
		signal syscall.Signal
		wait   time.Duration
	}{{syscall.SIGHUP, 250 * time.Millisecond}, {syscall.SIGTERM, 250 * time.Millisecond}, {syscall.SIGKILL, 0}} {
		err := syscall.Kill(-pid, step.signal)
		if errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone) {
			return
		}
		if step.wait > 0 {
			time.Sleep(step.wait)
		}
	}
}
