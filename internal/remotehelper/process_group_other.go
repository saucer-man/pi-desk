//go:build !linux && !darwin

package remotehelper

import "os/exec"

func configureProcessGroup(*exec.Cmd) {}

func terminateProcessGroup(pid int)  {}
func terminateTerminalGroup(pid int) {}
