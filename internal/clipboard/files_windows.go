package clipboard

import (
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                     = windows.NewLazySystemDLL("user32.dll")
	openClipboard              = user32.NewProc("OpenClipboard")
	closeClipboard             = user32.NewProc("CloseClipboard")
	getClipboardData           = user32.NewProc("GetClipboardData")
	isClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	dragQueryFile              = windows.NewLazySystemDLL("shell32.dll").NewProc("DragQueryFileW")
)

// FilePaths reads Explorer's file list without consuming or changing the clipboard.
// The clipboard owns the HDROP; it must not be freed with DragFinish.
func FilePaths() ([]string, error) {
	const cfHdrop = 15
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	available, _, _ := isClipboardFormatAvailable.Call(cfHdrop)
	if available == 0 {
		return nil, nil
	}
	opened, _, _ := openClipboard.Call(0)
	if opened == 0 {
		return nil, errors.New("clipboard is busy; please paste again")
	}
	defer closeClipboard.Call()
	handle, _, _ := getClipboardData.Call(cfHdrop)
	if handle == 0 {
		return nil, errors.New("cannot read clipboard files; please paste again")
	}
	return fileDropPaths(handle)
}

func fileDropPaths(handle uintptr) ([]string, error) {
	count, _, _ := dragQueryFile.Call(handle, 0xffffffff, 0, 0)
	if count > 128 {
		return nil, errors.New("paste at most 128 files at a time")
	}
	paths := make([]string, 0, count)
	for index := uintptr(0); index < count; index++ {
		length, _, _ := dragQueryFile.Call(handle, index, 0, 0)
		if length == 0 || length >= 32768 {
			return nil, errors.New("invalid clipboard file path")
		}
		buffer := make([]uint16, length+1)
		copied, _, _ := dragQueryFile.Call(handle, index, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
		if copied != length {
			return nil, errors.New("cannot read clipboard file path")
		}
		paths = append(paths, windows.UTF16ToString(buffer))
	}
	return paths, nil
}
