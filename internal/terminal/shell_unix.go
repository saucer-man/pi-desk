//go:build !windows

package terminal

import (
	"errors"
	"os"
	"os/exec"
)

func defaultShell() (string, []string, error) {
	if shell := os.Getenv("SHELL"); shell != "" {
		if path, err := exec.LookPath(shell); err == nil {
			return path, nil, nil
		}
	}
	if path, err := exec.LookPath("sh"); err == nil {
		return path, nil, nil
	}
	return "", nil, errors.New("no supported shell was found")
}
