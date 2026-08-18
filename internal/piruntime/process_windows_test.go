//go:build windows

package piruntime

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestProcessAlreadyExited(t *testing.T) {
	for _, err := range []error{os.ErrProcessDone, syscall.EINVAL} {
		if !processAlreadyExited(err) {
			t.Fatalf("expected %v to be treated as an exited process", err)
		}
	}
	if processAlreadyExited(errors.New("access denied")) {
		t.Fatal("unexpectedly treated an unrelated process error as an exited process")
	}
}
