//go:build !linux && !darwin

package remotehelper

import "os"

func openRootRead(root *os.Root, name string) (*os.File, error) {
	return root.Open(name)
}
