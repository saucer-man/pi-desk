//go:build !windows

package appservice

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
)

func listenRemoteBroker(directory, _ string) (net.Listener, string, error) {
	socket := filepath.Join(directory, "b.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return nil, "", fmt.Errorf("listen on remote broker socket: %w", err)
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		_ = listener.Close()
		return nil, "", fmt.Errorf("protect remote broker socket: %w", err)
	}
	return listener, socket, nil
}
