//go:build !linux && !darwin

package remotehelper

import (
	"errors"
	"os"
)

type rootFileIdentity struct {
	Device uint64
	Inode  uint64
}

func rootIdentity(os.FileInfo) (rootFileIdentity, error) {
	return rootFileIdentity{}, errors.New("remote root identity is only supported on Linux and macOS")
}
