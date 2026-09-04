package clipboard

import (
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Exercise the real Windows decoder using an owned HDROP, without replacing
// the user's system clipboard or needing Explorer/UI automation.
func TestFileDropPaths(t *testing.T) {
	for _, paths := range [][]string{
		{`D:\项目\需求 文档.pdf`, `D:\repo\main.go`},
		strings.Split(strings.Repeat("D:\\repo\\file.txt|", 128)+"D:\\repo\\last.txt", "|"),
	} {
		encoded := utf16.Encode([]rune(strings.Join(paths, "\x00") + "\x00\x00"))
		data := make([]byte, 20+len(encoded)*2)
		binary.LittleEndian.PutUint32(data, 20)
		binary.LittleEndian.PutUint32(data[16:], 1)
		for index, code := range encoded {
			binary.LittleEndian.PutUint16(data[20+index*2:], code)
		}
		kernel32 := windows.NewLazySystemDLL("kernel32.dll")
		handle, _, err := kernel32.NewProc("GlobalAlloc").Call(0x40, uintptr(len(data)))
		if handle == 0 {
			t.Fatal(err)
		}
		kernel32.NewProc("RtlMoveMemory").Call(handle, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
		actual, readErr := fileDropPaths(handle)
		kernel32.NewProc("GlobalFree").Call(handle)
		if len(paths) > 128 {
			if readErr == nil {
				t.Fatal("oversized batch accepted")
			}
		} else if readErr != nil || !reflect.DeepEqual(actual, paths) {
			t.Fatalf("decoded %v, %v; wanted %v", actual, readErr, paths)
		}
	}
}
