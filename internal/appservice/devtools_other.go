//go:build !windows

package appservice

import "unsafe"

func closeDevToolsWindow(unsafe.Pointer) bool {
	return false
}
