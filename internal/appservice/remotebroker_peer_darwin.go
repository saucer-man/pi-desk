//go:build darwin

package appservice

import (
	"errors"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func remoteBrokerPeerPID(connection net.Conn) (int, error) {
	rawConnection, ok := connection.(syscall.Conn)
	if !ok {
		return 0, errors.New("remote broker connection has no peer credentials")
	}
	raw, err := rawConnection.SyscallConn()
	if err != nil {
		return 0, err
	}
	peerPID := 0
	var peerErr error
	if err := raw.Control(func(fd uintptr) {
		peerPID, peerErr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	}); err != nil {
		return 0, err
	}
	if peerErr != nil || peerPID <= 0 {
		return 0, errors.New("remote broker peer identity is invalid")
	}
	return peerPID, nil
}

func remoteBrokerPeerMatches(peerPID, launcherPID int) bool {
	return peerPID > 0 && peerPID == launcherPID
}
