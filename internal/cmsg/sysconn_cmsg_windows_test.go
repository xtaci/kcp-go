//go:build windows

package cmsg

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestSocketControlMessageHeaderAndDataRejectsShortHeader(t *testing.T) {
	for _, size := range []int{0, sizeofWSACMSGHDR - 1} {
		_, _, err := socketControlMessageHeaderAndData(make([]byte, size))
		if err != windows.WSAEINVAL {
			t.Fatalf("error for %d-byte header = %v, want %v", size, err, windows.WSAEINVAL)
		}
	}
}
