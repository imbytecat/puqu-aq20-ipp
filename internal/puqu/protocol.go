// PUQU (AQ/Q-series) BLE print protocol, reverse-engineered from the official SDK
// (com.puqu.sdk.old.PuQuPrintOld). Verified against a real Q20/AQ20 over ae01/ae02.
//
//   - Control frames: 3A 5A xx xx xx xx xx xx  ("Z" command)
//   - Print: an 8-byte header 3A [widthBytes] [hLo] [hHi] [lenLo] [lenMid] [lenHi] 15
//     immediately followed by the raw 1bpp bitmap (MSB-first, bit 1 = black, rows
//     byte-padded) — exactly the format the browser canvas already produces.
package puqu

import "encoding/hex"

const (
	DPIFlag200 byte = 0x15
	DPIFlag300 byte = 0x16
)

// PrintHeader builds the 8-byte raster header that precedes the 1bpp bitmap.
func PrintHeader(widthBytes, heightPx, dataLength int) []byte {
	return []byte{
		0x3a,
		byte(widthBytes),
		byte(heightPx),
		byte(heightPx >> 8),
		byte(dataLength),
		byte(dataLength >> 8),
		byte(dataLength >> 16),
		DPIFlag200,
	}
}

func Wake() []byte      { return []byte{0x3a, 0x5a, 0, 0, 0, 0, 0, 0x3a} }
func ReadState() []byte { return []byte{0x3a, 0x5a, 0, 0, 0, 0, 0, 0x0a} }
func Cancel() []byte    { return []byte{0x3a, 0x5a, 0x33, 0, 0, 0, 0, 0x3a} }

// DeviceSettings mirrors the temporary/permanent device-detail frame.
// darkness 0-11, speed 0-5, paperType: 1 continuous, 2 gap, 3 black-mark.
type DeviceSettings struct {
	Darkness  int
	Speed     int
	PaperType int
	Temporary bool
}

// DeviceDetails encodes a settings frame; paperFeed is fixed at 1 (matches the SDK).
func DeviceDetails(s DeviceSettings) []byte {
	const paperFeed = 1
	code1 := byte(((s.Darkness << 4) & 0xff) + (s.Speed & 0x0f))
	code2 := byte(((s.PaperType << 4) & 0xff) + (paperFeed & 0x0f))
	last := byte(0xda)
	if s.Temporary {
		last = 0xca
	}
	return []byte{0x3a, 0x5a, code1, code2, 0, 0, 0, last}
}

// Status is the decoded reply the printer pushes on the notify characteristic.
type Status struct {
	Busy bool
	Hex  string
}

// ParseStatus decodes a notify frame; ok is false when the frame isn't a status reply.
func ParseStatus(data []byte) (Status, bool) {
	if len(data) < 8 || data[0] != 0x3a {
		return Status{}, false
	}
	return Status{Busy: data[1]&0x08 != 0, Hex: hex.EncodeToString(data)}, true
}
