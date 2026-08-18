package gitexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"pi-desk/internal/processutil"
)

const (
	MaxOutputBytes = 4 << 20
	MaxErrorBytes  = 64 << 10
)

var ErrOutputTooLarge = errors.New("git output exceeds the safety limit")

type Runner struct{}

func (Runner) Run(ctx context.Context, directory string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, args...)...)
	processutil.ConfigureBackground(command)
	stdout := &limitedBuffer{limit: MaxOutputBytes}
	stderr := &limitedBuffer{limit: MaxErrorBytes}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if stdout.overflow {
		return nil, ErrOutputTooLarge
	}
	if err != nil {
		message := strings.TrimSpace(stderr.buffer.String())
		if message != "" {
			return nil, fmt.Errorf("%w: %s", err, message)
		}
		return nil, err
	}
	return stdout.buffer.Bytes(), nil
}

type limitedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (buffer *limitedBuffer) Write(value []byte) (int, error) {
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining > 0 {
		buffer.buffer.Write(value[:min(len(value), remaining)])
	}
	if len(value) > remaining {
		buffer.overflow = true
	}
	return len(value), nil
}
