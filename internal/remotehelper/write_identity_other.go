//go:build !linux && !darwin

package remotehelper

import "os"

func fileOwnership(os.FileInfo) (int, int, bool) {
	return 0, 0, false
}
