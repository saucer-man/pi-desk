//go:build windows

package appservice

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func listenRemoteBroker(_ string, nonce string) (net.Listener, string, error) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return nil, "", fmt.Errorf("inspect remote broker owner: %w", err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, "", fmt.Errorf("inspect remote broker owner: %w", err)
	}
	pipe := `\\.\pipe\pi-desk-remote-` + nonce
	listener, err := winio.ListenPipe(pipe, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;" + user.User.Sid.String() + ")",
		InputBufferSize:    64 << 10, OutputBufferSize: 64 << 10,
	})
	if err != nil {
		return nil, "", fmt.Errorf("listen on remote broker pipe: %w", err)
	}
	return listener, pipe, nil
}
