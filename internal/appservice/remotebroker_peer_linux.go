//go:build linux

package appservice

import (
	"errors"
	"net"
	"os"
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
	var credentials *unix.Ucred
	var credentialErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, credentialErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if credentialErr != nil || credentials == nil || credentials.Pid <= 0 || credentials.Uid != uint32(os.Geteuid()) {
		return 0, errors.New("remote broker peer identity is invalid")
	}
	return int(credentials.Pid), nil
}

func remoteBrokerPeerMatches(peerPID, launcherPID int) bool {
	return peerPID > 0 && peerPID == launcherPID
}
