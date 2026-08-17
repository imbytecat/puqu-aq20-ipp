package puqu

import (
	"bytes"
	"testing"
)

func TestControlFrames(t *testing.T) {
	tests := []struct {
		name string
		got  []byte
		want []byte
	}{
		{"wake", Wake(), []byte{0x3a, 0x5a, 0, 0, 0, 0, 0, 0x3a}},
		{"readState", ReadState(), []byte{0x3a, 0x5a, 0, 0, 0, 0, 0, 0x0a}},
		{"cancel", Cancel(), []byte{0x3a, 0x5a, 0x33, 0, 0, 0, 0, 0x3a}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !bytes.Equal(tc.got, tc.want) {
				t.Fatalf("got % x, want % x", tc.got, tc.want)
			}
		})
	}
}

func TestPrintHeader(t *testing.T) {
	tests := []struct {
		name                          string
		widthBytes, heightPx, dataLen int
		want                          []byte
	}{
		{"small", 40, 96, 3840, []byte{0x3a, 40, 96, 0, 0x00, 0x0f, 0x00, 0x15}},
		{"multibyte height+len", 40, 300, 70000, []byte{0x3a, 40, 0x2c, 1, 0x70, 0x11, 1, 0x15}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := PrintHeader(tc.widthBytes, tc.heightPx, tc.dataLen)
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("got % x, want % x", got, tc.want)
			}
		})
	}
}

func TestDeviceDetails(t *testing.T) {
	// darkness 5, speed 3, paper 2, temporary -> code1=0x53, code2=0x21, last=0xca
	temp := DeviceDetails(DeviceSettings{Darkness: 5, Speed: 3, PaperType: 2, Temporary: true})
	if want := []byte{0x3a, 0x5a, 0x53, 0x21, 0, 0, 0, 0xca}; !bytes.Equal(temp, want) {
		t.Fatalf("temporary: got % x, want % x", temp, want)
	}
	// darkness 11, speed 5, paper 1, permanent -> code1=0xb5, code2=0x11, last=0xda
	perm := DeviceDetails(DeviceSettings{Darkness: 11, Speed: 5, PaperType: 1, Temporary: false})
	if want := []byte{0x3a, 0x5a, 0xb5, 0x11, 0, 0, 0, 0xda}; !bytes.Equal(perm, want) {
		t.Fatalf("permanent: got % x, want % x", perm, want)
	}
}

func TestParseStatus(t *testing.T) {
	// real not-busy reply observed from hardware
	s, ok := ParseStatus([]byte{0x3a, 0x00, 0x94, 0x00, 0x3c, 0x3c, 0x00, 0x0a})
	if !ok || s.Busy {
		t.Fatalf("not-busy: ok=%v busy=%v", ok, s.Busy)
	}
	if s.Hex != "3a0094003c3c000a" {
		t.Fatalf("hex = %q", s.Hex)
	}
	// busy bit (0x08) set in byte 1
	if s2, ok := ParseStatus([]byte{0x3a, 0x08, 0, 0, 0, 0, 0, 0}); !ok || !s2.Busy {
		t.Fatalf("busy: ok=%v busy=%v", ok, s2.Busy)
	}
	// rejects a too-short frame and a wrong prefix
	if _, ok := ParseStatus([]byte{0x3a, 0x00}); ok {
		t.Fatal("short frame should not parse")
	}
	if _, ok := ParseStatus([]byte{0x00, 0, 0, 0, 0, 0, 0, 0}); ok {
		t.Fatal("wrong prefix should not parse")
	}
}
