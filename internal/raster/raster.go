// Package raster decodes the driverless raster formats accepted by the virtual printer.
package raster

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/imbytecat/puqu-aq20-ipp/internal/printer"
)

const (
	FormatPWG   = "image/pwg-raster"
	FormatApple = "image/urf"

	pageHeaderSize = 1796
	maxRasterBytes = 16 << 20

	colorW        = 0
	colorRGB      = 1
	colorK        = 3
	colorSW       = 18
	colorSRGB     = 19
	colorAdobeRGB = 20
)

var (
	ErrFormat     = errors.New("unsupported raster format")
	ErrDimensions = errors.New("raster dimensions do not match active label profile")
)

type Profile struct {
	WidthUM  int64
	HeightUM int64
}

type page struct {
	width        int
	height       int
	xdpi         int
	ydpi         int
	bitsPerColor int
	bitsPerPixel int
	bytesPerLine int
	colorOrder   int
	colorSpace   int
}

func Decode(input io.Reader, format string, profile Profile) ([]printer.Job, error) {
	reader := bufio.NewReader(io.LimitReader(input, maxRasterBytes+1))
	sync := make([]byte, 4)
	if _, err := io.ReadFull(reader, sync); err != nil {
		return nil, fmt.Errorf("read raster sync: %w", err)
	}

	switch format {
	case FormatPWG:
		if !bytes.Equal(sync, []byte("RaS2")) {
			return nil, fmt.Errorf("%w: expected PWG RaS2 sync", ErrFormat)
		}
		return decodePWG(reader, profile)
	case FormatApple:
		if !bytes.Equal(sync, []byte("UNIR")) {
			return nil, fmt.Errorf("%w: expected Apple UNIR sync", ErrFormat)
		}
		return decodeApple(reader, profile)
	default:
		return nil, ErrFormat
	}
}

func decodePWG(reader *bufio.Reader, profile Profile) ([]printer.Job, error) {
	var jobs []printer.Job
	for {
		header := make([]byte, pageHeaderSize)
		_, err := io.ReadFull(reader, header)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read PWG page header: %w", err)
		}
		p := page{
			xdpi: int(u32(header, 276)), ydpi: int(u32(header, 280)),
			width: int(u32(header, 372)), height: int(u32(header, 376)),
			bitsPerColor: int(u32(header, 384)), bitsPerPixel: int(u32(header, 388)),
			bytesPerLine: int(u32(header, 392)), colorOrder: int(u32(header, 396)),
			colorSpace: int(u32(header, 400)),
		}
		job, err := decodePage(reader, p, profile)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if len(jobs) == 0 {
		return nil, errors.New("raster document contains no pages")
	}
	return jobs, nil
}

func decodeApple(reader *bufio.Reader, profile Profile) ([]printer.Job, error) {
	fileHeader := make([]byte, 8)
	if _, err := io.ReadFull(reader, fileHeader); err != nil {
		return nil, fmt.Errorf("read Apple raster file header: %w", err)
	}
	if !bytes.Equal(fileHeader[:4], []byte{'A', 'S', 'T', 0}) {
		return nil, fmt.Errorf("%w: invalid Apple raster file header", ErrFormat)
	}
	pageCount := binary.BigEndian.Uint32(fileHeader[4:])
	if pageCount == 0 || pageCount > 1000 {
		return nil, errors.New("invalid Apple raster page count")
	}
	jobs := make([]printer.Job, 0, pageCount)
	for range pageCount {
		header := make([]byte, 32)
		if _, err := io.ReadFull(reader, header); err != nil {
			return nil, fmt.Errorf("read Apple raster page header: %w", err)
		}
		colors, colorSpace := appleColorSpace(header[1])
		bitsPerPixel := int(header[0])
		if colors == 0 || bitsPerPixel%colors != 0 {
			return nil, ErrFormat
		}
		p := page{
			width: int(binary.BigEndian.Uint32(header[12:16])), height: int(binary.BigEndian.Uint32(header[16:20])),
			xdpi: int(binary.BigEndian.Uint32(header[20:24])), ydpi: int(binary.BigEndian.Uint32(header[20:24])),
			bitsPerColor: bitsPerPixel / colors, bitsPerPixel: bitsPerPixel,
			bytesPerLine: (int(binary.BigEndian.Uint32(header[12:16]))*bitsPerPixel + 7) / 8,
			colorOrder:   0, colorSpace: colorSpace,
		}
		job, err := decodePage(reader, p, profile)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func appleColorSpace(value byte) (colors, colorSpace int) {
	switch value {
	case 0:
		return 1, colorSW
	case 1:
		return 3, colorSRGB
	case 3:
		return 3, colorAdobeRGB
	case 4:
		return 1, colorW
	case 5:
		return 3, colorRGB
	default:
		return 0, 0
	}
}

func decodePage(reader io.Reader, p page, profile Profile) (printer.Job, error) {
	if err := validatePage(p, profile); err != nil {
		return printer.Job{}, err
	}
	bytesPerValue := (p.bitsPerPixel + 7) / 8
	widthBytes := (p.width + 7) / 8
	bitmap := make([]byte, widthBytes*p.height)
	row := 0
	for row < p.height {
		repeatByte, err := readByte(reader)
		if err != nil {
			return printer.Job{}, fmt.Errorf("read raster line repeat: %w", err)
		}
		line, err := decodeLine(reader, p.bytesPerLine, bytesPerValue)
		if err != nil {
			return printer.Job{}, err
		}
		repeats := int(repeatByte) + 1
		if row+repeats > p.height {
			return printer.Job{}, errors.New("raster line repeat exceeds page height")
		}
		packed, err := packLine(line, p)
		if err != nil {
			return printer.Job{}, err
		}
		for range repeats {
			copy(bitmap[row*widthBytes:], packed)
			row++
		}
	}
	return printer.Job{WidthBytes: widthBytes, HeightPx: p.height, Data: bitmap, Copies: 1}, nil
}

func validatePage(p page, profile Profile) error {
	if p.xdpi != 203 || p.ydpi != 203 {
		return fmt.Errorf("%w: resolution %dx%d, expected 203x203", ErrFormat, p.xdpi, p.ydpi)
	}
	if p.width < 1 || p.width > 2040 || p.height < 1 || p.height > 65535 {
		return errors.New("raster page dimensions exceed printer limits")
	}
	expectedWidth := dots(profile.WidthUM)
	expectedHeight := dots(profile.HeightUM)
	if abs(p.width-expectedWidth) > 1 || abs(p.height-expectedHeight) > 1 {
		return fmt.Errorf("%w: got %dx%d, expected %dx%d", ErrDimensions, p.width, p.height, expectedWidth, expectedHeight)
	}
	if p.colorOrder != 0 {
		return fmt.Errorf("%w: only chunked color order is supported", ErrFormat)
	}
	if p.bytesPerLine < (p.width*p.bitsPerPixel+7)/8 || p.bytesPerLine > 1<<20 {
		return errors.New("invalid raster bytes-per-line")
	}
	switch {
	case p.bitsPerColor == 1 && p.bitsPerPixel == 1 && (p.colorSpace == colorW || p.colorSpace == colorSW || p.colorSpace == colorK):
	case p.bitsPerColor == 8 && p.bitsPerPixel == 8 && (p.colorSpace == colorW || p.colorSpace == colorSW || p.colorSpace == colorK):
	case p.bitsPerColor == 8 && p.bitsPerPixel == 24 && (p.colorSpace == colorRGB || p.colorSpace == colorSRGB || p.colorSpace == colorAdobeRGB):
	default:
		return fmt.Errorf("%w: unsupported %d-bit color space %d", ErrFormat, p.bitsPerPixel, p.colorSpace)
	}
	return nil
}

func decodeLine(reader io.Reader, bytesPerLine, bytesPerValue int) ([]byte, error) {
	line := make([]byte, 0, bytesPerLine)
	for len(line) < bytesPerLine {
		control, err := readByte(reader)
		if err != nil {
			return nil, fmt.Errorf("read raster run: %w", err)
		}
		if control <= 127 {
			count := int(control) + 1
			value := make([]byte, bytesPerValue)
			if _, err := io.ReadFull(reader, value); err != nil {
				return nil, fmt.Errorf("read repeated raster value: %w", err)
			}
			if len(line)+count*bytesPerValue > bytesPerLine {
				return nil, errors.New("raster repeat run exceeds line")
			}
			for range count {
				line = append(line, value...)
			}
		} else {
			count := 257 - int(control)
			bytesNeeded := count * bytesPerValue
			if len(line)+bytesNeeded > bytesPerLine {
				return nil, errors.New("raster literal run exceeds line")
			}
			start := len(line)
			line = append(line, make([]byte, bytesNeeded)...)
			if _, err := io.ReadFull(reader, line[start:]); err != nil {
				return nil, fmt.Errorf("read literal raster values: %w", err)
			}
		}
	}
	return line, nil
}

func packLine(line []byte, p page) ([]byte, error) {
	out := make([]byte, (p.width+7)/8)
	if p.bitsPerPixel == 1 {
		copy(out, line[:len(out)])
		if p.colorSpace == colorW || p.colorSpace == colorSW {
			for i := range out {
				out[i] = ^out[i]
			}
		}
		maskTrailing(out, p.width)
		return out, nil
	}
	for x := range p.width {
		black := false
		switch p.bitsPerPixel {
		case 8:
			value := line[x]
			if p.colorSpace == colorK {
				black = value >= 128
			} else {
				black = value < 128
			}
		case 24:
			off := x * 3
			luma := (77*int(line[off]) + 150*int(line[off+1]) + 29*int(line[off+2])) >> 8
			black = luma < 128
		default:
			return nil, ErrFormat
		}
		if black {
			out[x/8] |= 0x80 >> (x % 8)
		}
	}
	return out, nil
}

func maskTrailing(data []byte, width int) {
	if rem := width % 8; rem != 0 {
		data[len(data)-1] &= byte(0xff << (8 - rem))
	}
}

func dots(micrometers int64) int {
	return int((micrometers*8 + 500) / 1000)
}

func u32(data []byte, offset int) uint32 {
	return binary.BigEndian.Uint32(data[offset : offset+4])
}

func readByte(reader io.Reader) (byte, error) {
	var data [1]byte
	_, err := io.ReadFull(reader, data[:])
	return data[0], err
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
