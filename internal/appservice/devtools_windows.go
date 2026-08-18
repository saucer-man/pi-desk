//go:build windows

package appservice

import (
	"strings"
	"syscall"
	"unsafe"
)

const windowClose = 0x0010

var (
	debugUser32              = syscall.NewLazyDLL("user32.dll")
	debugEnumWindows         = debugUser32.NewProc("EnumWindows")
	debugGetWindowThreadPID  = debugUser32.NewProc("GetWindowThreadProcessId")
	debugGetWindowTextLength = debugUser32.NewProc("GetWindowTextLengthW")
	debugGetWindowText       = debugUser32.NewProc("GetWindowTextW")
	debugPostMessage         = debugUser32.NewProc("PostMessageW")
)

func closeDevToolsWindow(parent unsafe.Pointer) bool {
	parentWindow := uintptr(parent)
	if parentWindow == 0 {
		return false
	}
	closed := false
	var parentProcessID uint32
	_, _, _ = debugGetWindowThreadPID.Call(parentWindow, uintptr(unsafe.Pointer(&parentProcessID)))
	if parentProcessID == 0 {
		return false
	}
	callback := syscall.NewCallback(func(window, _ uintptr) uintptr {
		var processID uint32
		_, _, _ = debugGetWindowThreadPID.Call(window, uintptr(unsafe.Pointer(&processID)))
		if processID != parentProcessID {
			return 1
		}
		if !strings.Contains(strings.ToLower(nativeWindowTitle(window)), "devtools") {
			return 1
		}
		result, _, _ := debugPostMessage.Call(window, windowClose, 0, 0)
		if result != 0 {
			closed = true
		}
		return 1
	})
	_, _, _ = debugEnumWindows.Call(callback, 0)
	return closed
}

func nativeWindowTitle(window uintptr) string {
	length, _, _ := debugGetWindowTextLength.Call(window)
	if length == 0 {
		return ""
	}
	buffer := make([]uint16, length+1)
	read, _, _ := debugGetWindowText.Call(window, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if read == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer[:read])
}
