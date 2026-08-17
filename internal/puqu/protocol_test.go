package puqu

import (
	"bytes"
	"testing"
)

func TestPrintHeader(t *testing.T) {
	tests := []struct {
		name                 string
		widthBytes, heightPx int
		want                 []byte
	}{
		{"40-byte 96-row label", 40, 96, []byte{0x2a, 0x76, 0x30, 0x02, 40, 0, 96, 0}},
		{"little-endian dimensions", 0x123, 0x456, []byte{0x2a, 0x76, 0x30, 0x02, 0x23, 0x01, 0x56, 0x04}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := PrintHeader(tc.widthBytes, tc.heightPx)
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("got % x, want % x", got, tc.want)
			}
		})
	}
}
