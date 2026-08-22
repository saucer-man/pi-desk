//go:build linux || darwin

package remotehelper

import (
	"errors"
	"os"
	"syscall"
)

type rootFileIdentity struct {
	Device uint64
	Inode  uint64
}

func rootIdentity(info os.FileInfo) (rootFileIdentity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return rootFileIdentity{}, errors.New("POSIX stat identity unavailable")
	}
	return rootFileIdentity{Device: uint64(stat.Dev), Inode: uint64(stat.Ino)}, nil
}
