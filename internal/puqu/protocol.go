// Package puqu encodes the native PUQU USB raster stream used by the official
// Windows PUQUPrinterUni driver: 2A 76 30 02, little-endian width bytes and
// height, then raw 1bpp bitmap rows (MSB-first, bit 1 = black).
package puqu

const RasterMode203 byte = 0x02

// PrintHeader builds the 8-byte header preceding one 203 dpi raster page.
func PrintHeader(widthBytes, heightPx int) []byte {
	return []byte{
		0x2a, 0x76, 0x30, RasterMode203,
		byte(widthBytes), byte(widthBytes >> 8),
		byte(heightPx), byte(heightPx >> 8),
	}
}
