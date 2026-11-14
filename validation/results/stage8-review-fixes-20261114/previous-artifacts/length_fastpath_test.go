package mariadb

import (
	"bytes"
	"testing"
)

func TestDedicatedShortLengthBoundaries(t *testing.T) {
	for n := 0; n <= 250; n++ {
		packet := append([]byte{byte(n)}, bytes.Repeat([]byte{'x'}, n)...)
		value, null, read, err := readLengthEncodedString(packet)
		if err != nil || null || read != len(packet) || len(value) != n || cap(value) != n {
			t.Fatalf("length %d: bytes=%d cap=%d read=%d null=%v err=%v", n, len(value), cap(value), read, null, err)
		}
		if n > 0 {
			if _, _, _, err := readLengthEncodedString(packet[:len(packet)-1]); err == nil {
				t.Fatalf("truncated length %d accepted", n)
			}
		}
	}
	value, null, n, err := readLengthEncodedString([]byte{0xfb})
	if err != nil || !null || len(value) != 0 || n != 1 {
		t.Fatal("NULL fast-path boundary changed")
	}
}
