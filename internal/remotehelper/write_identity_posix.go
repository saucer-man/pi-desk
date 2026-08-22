//go:build linux || darwin

package remotehelper

import (
	"os"
	"syscall"
)

func fileOwnership(info os.FileInfo) (int, int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(stat.Uid), int(stat.Gid), true
}
