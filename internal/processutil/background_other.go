//go:build !windows

package processutil

import "os/exec"

func ConfigureBackground(_ *exec.Cmd) {}
