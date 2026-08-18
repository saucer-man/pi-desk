//go:build windows

package terminal

import (
	"errors"
	"os"
	"os/exec"
)

func defaultShell() (string, []string, error) {
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, []string{"-NoLogo"}, nil
		}
	}
	if commandProcessor := os.Getenv("ComSpec"); commandProcessor != "" {
		if path, err := exec.LookPath(commandProcessor); err == nil {
			return path, nil, nil
		}
	}
	if path, err := exec.LookPath("cmd.exe"); err == nil {
		return path, nil, nil
	}
	return "", nil, errors.New("no supported Windows shell was found")
}
