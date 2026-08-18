//go:build windows

package workspaceapp

import (
	"errors"
	"image"
	"os"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
)

var privateExtractIcons = windows.NewLazySystemDLL("user32.dll").NewProc("PrivateExtractIconsW")

func loadNativeIcon(item candidate) (image.Image, error) {
	for _, path := range item.iconPaths {
		if icon, err := decodeIconFile(path); err == nil {
			return icon, nil
		}
	}
	if item.executable == "" {
		return nil, errors.New("application executable is unavailable")
	}
	hicon, err := extractLargeWindowsIcon(item.executable)
	if err != nil {
		return nil, err
	}
	defer w32.DestroyIcon(hicon)

	file, err := os.CreateTemp("", "pi-desk-workspace-icon-*.png")
	if err != nil {
		return nil, err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	defer os.Remove(path)
	if err := w32.SaveHIconAsPNG(hicon, path); err != nil {
		return nil, err
	}
	return decodeIconFile(path)
}

func extractLargeWindowsIcon(executable string) (w32.HICON, error) {
	path, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return 0, err
	}
	var hicon w32.HICON
	var resourceID uint32
	count, _, callErr := privateExtractIcons.Call(
		uintptr(unsafe.Pointer(path)),
		0,
		256,
		256,
		uintptr(unsafe.Pointer(&hicon)),
		uintptr(unsafe.Pointer(&resourceID)),
		1,
		0,
	)
	if count == 0 || hicon == 0 {
		if callErr != nil && !errors.Is(callErr, windows.ERROR_SUCCESS) {
			return 0, callErr
		}
		return 0, errors.New("application has no extractable Windows icon")
	}
	return hicon, nil
}
