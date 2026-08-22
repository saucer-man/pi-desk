//go:build !windows && !linux && !darwin

package appservice

import (
	"errors"
	"net"
)

func remoteBrokerPeerPID(net.Conn) (int, error) {
	return 0, errors.New("remote broker peer credentials are unsupported on this platform")
}

func remoteBrokerPeerMatches(int, int) bool { return false }
