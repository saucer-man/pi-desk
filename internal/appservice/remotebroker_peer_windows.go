//go:build windows

package appservice

import (
	"errors"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

var getNamedPipeClientProcessID = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetNamedPipeClientProcessId")

func remoteBrokerPeerPID(connection net.Conn) (int, error) {
	handle, ok := connection.(interface{ Fd() uintptr })
	if !ok || handle.Fd() == 0 {
		return 0, errors.New("remote broker connection has no peer credentials")
	}
	var processID uint32
	result, _, callErr := getNamedPipeClientProcessID.Call(handle.Fd(), uintptr(unsafe.Pointer(&processID)))
	if result == 0 || processID == 0 {
		if callErr != nil && callErr != windows.ERROR_SUCCESS {
			return 0, callErr
		}
		return 0, errors.New("remote broker peer identity is invalid")
	}
	return int(processID), nil
}

func remoteBrokerPeerMatches(peerPID, launcherPID int) bool {
	if peerPID <= 0 || launcherPID <= 0 {
		return false
	}
	if peerPID == launcherPID {
		return true
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snapshot)
	parents := make(map[uint32]uint32)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if windows.Process32First(snapshot, &entry) != nil {
		return false
	}
	for {
		parents[entry.ProcessID] = entry.ParentProcessID
		if windows.Process32Next(snapshot, &entry) != nil {
			break
		}
	}
	current := uint32(peerPID)
	for range 64 {
		parent, exists := parents[current]
		if !exists || parent == 0 || parent == current {
			return false
		}
		if parent == uint32(launcherPID) {
			return true
		}
		current = parent
	}
	return false
}
