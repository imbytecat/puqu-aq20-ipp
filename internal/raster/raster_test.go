package raster

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestDecodePWGBilevel(t *testing.T) {
	header := pwgHeader(8, 2, 1, 1, colorSW)
	data := append([]byte("RaS2"), header...)
	data = append(data,
		0x00, 0x00, 0x00, // one line, one repeated byte: white-space value 0 -> black
		0x00, 0x00, 0xff, // one line, one repeated byte: white-space value 1 -> white
	)
	jobs, err := Decode(bytes.NewReader(data), FormatPWG, Profile{WidthUM: 1000, HeightUM: 250})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].WidthBytes != 1 || jobs[0].HeightPx != 2 {
		t.Fatalf("jobs = %+v", jobs)
	}
	if !bytes.Equal(jobs[0].Data, []byte{0xff, 0x00}) {
		t.Fatalf("bitmap = % x", jobs[0].Data)
	}
}

func TestDecodeAppleGrayscale(t *testing.T) {
	data := []byte("UNIRAST\x00\x00\x00\x00\x01")
	header := make([]byte, 32)
	header[0] = 8 // bits per pixel
	header[1] = 0 // sGray
	binary.BigEndian.PutUint32(header[12:16], 8)
	binary.BigEndian.PutUint32(header[16:20], 1)
	binary.BigEndian.PutUint32(header[20:24], 203)
	data = append(data, header...)
	data = append(data, 0x00, 0xf9, 0, 255, 0, 255, 0, 255, 0, 255)
	jobs, err := Decode(bytes.NewReader(data), FormatApple, Profile{WidthUM: 1000, HeightUM: 125})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(jobs[0].Data, []byte{0xaa}) {
		t.Fatalf("bitmap = % x, want aa", jobs[0].Data)
	}
}
func TestDecodeJPEG(t *testing.T) {
	source := image.NewGray(image.Rect(0, 0, 8, 1))
	for x := range 8 {
		if x%2 == 0 {
			source.SetGray(x, 0, color.Gray{Y: 0})
		} else {
			source.SetGray(x, 0, color.Gray{Y: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, source, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	jobs, err := Decode(&encoded, FormatJPEG, Profile{WidthUM: 1000, HeightUM: 125})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(jobs[0].Data, []byte{0xaa}) {
		t.Fatalf("bitmap = % x, want aa", jobs[0].Data)
	}
}

func TestCUPSReferenceCompressionExample(t *testing.T) {
	// First compressed row from the CUPS Raster Format specification Figure 3:
	// one white RGB value, three yellow values, four white values.
	encoded := []byte{
		0x00, 0xff, 0xff, 0xff,
		0x02, 0xff, 0xff, 0x00,
		0x03, 0xff, 0xff, 0xff,
	}
	line, err := decodeLine(bytes.NewReader(encoded), 8*3, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		255, 255, 255,
		255, 255, 0, 255, 255, 0, 255, 255, 0,
		255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	}
	if !bytes.Equal(line, want) {
		t.Fatalf("line = % x\nwant = % x", line, want)
	}
}

func TestDecodeRejectsWrongMedia(t *testing.T) {
	header := pwgHeader(8, 1, 8, 8, colorSW)
	data := append([]byte("RaS2"), header...)
	data = append(data, 0, 7, 255)
	_, err := Decode(bytes.NewReader(data), FormatPWG, Profile{WidthUM: 40000, HeightUM: 30000})
	if !errors.Is(err, ErrDimensions) {
		t.Fatalf("error = %v, want dimensions error", err)
	}
}

func TestDecodeRejectsUnsupportedResolution(t *testing.T) {
	header := pwgHeader(8, 1, 1, 1, colorSW)
	binary.BigEndian.PutUint32(header[276:280], 300)
	data := append([]byte("RaS2"), header...)
	_, err := Decode(bytes.NewReader(data), FormatPWG, Profile{WidthUM: 1000, HeightUM: 125})
	if !errors.Is(err, ErrFormat) {
		t.Fatalf("error = %v, want format error", err)
	}
}

func pwgHeader(width, height, bitsPerColor, bitsPerPixel, colorSpace int) []byte {
	header := make([]byte, pageHeaderSize)
	binary.BigEndian.PutUint32(header[276:280], 203)
	binary.BigEndian.PutUint32(header[280:284], 203)
	binary.BigEndian.PutUint32(header[372:376], uint32(width))
	binary.BigEndian.PutUint32(header[376:380], uint32(height))
	binary.BigEndian.PutUint32(header[384:388], uint32(bitsPerColor))
	binary.BigEndian.PutUint32(header[388:392], uint32(bitsPerPixel))
	binary.BigEndian.PutUint32(header[392:396], uint32((width*bitsPerPixel+7)/8))
	binary.BigEndian.PutUint32(header[396:400], 0)
	binary.BigEndian.PutUint32(header[400:404], uint32(colorSpace))
	return header
}
