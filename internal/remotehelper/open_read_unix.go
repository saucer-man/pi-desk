//go:build linux || darwin

package remotehelper

import (
	"os"
	"syscall"
)

func openRootRead(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
