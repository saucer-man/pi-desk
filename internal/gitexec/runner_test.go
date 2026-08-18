package gitexec

import "testing"

func TestLimitedBufferDrainsWhileBoundingMemory(t *testing.T) {
	buffer := &limitedBuffer{limit: 4}
	n, err := buffer.Write([]byte("123456"))
	if err != nil || n != 6 || buffer.buffer.String() != "1234" || !buffer.overflow {
		t.Fatalf("unexpected bounded write: n=%d err=%v buffer=%q overflow=%v", n, err, buffer.buffer.String(), buffer.overflow)
	}
}
