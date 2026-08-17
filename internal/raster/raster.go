// Package raster decodes the driverless raster formats accepted by the virtual printer.
package raster

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image/jpeg"
	"io"

	"github.com/imbytecat/puqu-ipp-bridge/internal/printer"
)

const (
	FormatPWG      = "image/pwg-raster"
	FormatJPEG     = "image/jpeg"
	pageHeaderSize = 1796
	maxRasterBytes = 16 << 20

	colorW        = 0
	colorRGB      = 1
	colorK        = 3
	colorSW       = 18
	colorSRGB     = 19
	colorAdobeRGB = 20

	HalftoneAuto           = 0
	HalftoneDirect         = 1
	HalftoneClustered      = 2
	HalftoneErrorDiffusion = 3
)

var clustered4x4 = [16]byte{
	0, 136, 34, 170,
	204, 68, 238, 102,
	51, 187, 17, 153,
	255, 119, 221, 85,
}

var errorDiffusionWeights = [...]struct{ dx, dy, numerator int }{
	{1, 0, 8}, {2, 0, 4},
	{-2, 1, 2}, {-1, 1, 4}, {0, 1, 8}, {1, 1, 4}, {2, 1, 2},
	{-2, 2, 1}, {-1, 2, 2}, {0, 2, 4}, {1, 2, 2}, {2, 2, 1},
}

var (
	ErrFormat     = errors.New("unsupported raster format")
	ErrDimensions = errors.New("raster dimensions do not match active label profile")
)

type Profile struct {
	WidthUM        int64
	HeightUM       int64
	HalftoneMethod int64
	Brightness     int64
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
	if format == FormatJPEG {
		return decodeJPEG(input, profile)
	}
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

func decodeJPEG(input io.Reader, profile Profile) ([]printer.Job, error) {
	data, err := io.ReadAll(io.LimitReader(input, maxRasterBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read JPEG: %w", err)
	}
	if len(data) == 0 || len(data) > maxRasterBytes {
		return nil, errors.New("JPEG is empty or too large")
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid JPEG: %v", ErrFormat, err)
	}
	expectedWidth, expectedHeight := dots(profile.WidthUM), dots(profile.HeightUM)
	if config.Width != expectedWidth || config.Height != expectedHeight {
		return nil, fmt.Errorf("%w: got %dx%d, expected %dx%d", ErrDimensions, config.Width, config.Height, expectedWidth, expectedHeight)
	}
	if config.Width < 1 || config.Width > 576 || config.Height < 1 || config.Height > 65535 {
		return nil, errors.New("JPEG dimensions exceed printer limits")
	}
	image, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid JPEG: %v", ErrFormat, err)
	}
	grayscale := make([]byte, config.Width*config.Height)
	for y := range config.Height {
		for x := range config.Width {
			red, green, blue, _ := image.At(x, y).RGBA()
			grayscale[y*config.Width+x] = byte((77*int(red>>8) + 150*int(green>>8) + 29*int(blue>>8)) >> 8)
		}
	}
	bitmap := halftoneBitmap(grayscale, config.Width, config.Height, int(profile.HalftoneMethod), int(profile.Brightness))
	return []printer.Job{{WidthBytes: (config.Width + 7) / 8, HeightPx: config.Height, Data: bitmap, Copies: 1}}, nil
}

func decodePage(reader io.Reader, p page, profile Profile) (printer.Job, error) {
	if err := validatePage(p, profile); err != nil {
		return printer.Job{}, err
	}
	bytesPerValue := (p.bitsPerPixel + 7) / 8
	widthBytes := (p.width + 7) / 8
	bitmap := make([]byte, widthBytes*p.height)
	var grayscale []byte
	if p.bitsPerPixel != 1 {
		grayscale = make([]byte, p.width*p.height)
	}
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
		if p.bitsPerPixel == 1 {
			packed := packMonochromeLine(line, p)
			for range repeats {
				copy(bitmap[row*widthBytes:], packed)
				row++
			}
			continue
		}
		grayLine := grayscaleLine(line, p)
		for range repeats {
			copy(grayscale[row*p.width:], grayLine)
			row++
		}
	}
	if grayscale != nil {
		bitmap = halftoneBitmap(grayscale, p.width, p.height, int(profile.HalftoneMethod), int(profile.Brightness))
	}
	return printer.Job{WidthBytes: widthBytes, HeightPx: p.height, Data: bitmap, Copies: 1}, nil
}

func validatePage(p page, profile Profile) error {
	if p.xdpi != 203 || p.ydpi != 203 {
		return fmt.Errorf("%w: resolution %dx%d, expected 203x203", ErrFormat, p.xdpi, p.ydpi)
	}
	if p.width < 1 || p.width > 576 || p.height < 1 || p.height > 65535 {
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

func packMonochromeLine(line []byte, p page) []byte {
	out := make([]byte, (p.width+7)/8)
	copy(out, line[:len(out)])
	if p.colorSpace == colorW || p.colorSpace == colorSW {
		for i := range out {
			out[i] = ^out[i]
		}
	}
	maskTrailing(out, p.width)
	return out
}

func grayscaleLine(line []byte, p page) []byte {
	out := make([]byte, p.width)
	switch p.bitsPerPixel {
	case 8:
		for x := range p.width {
			if p.colorSpace == colorK {
				out[x] = 255 - line[x]
			} else {
				out[x] = line[x]
			}
		}
	case 24:
		for x := range p.width {
			off := x * 3
			out[x] = byte((77*int(line[off]) + 150*int(line[off+1]) + 29*int(line[off+2])) >> 8)
		}
	}
	return out
}

func halftoneBitmap(grayscale []byte, width, height, method, brightness int) []byte {
	values := grayscale
	if brightness != 0 {
		values = append([]byte(nil), grayscale...)
		offset := brightness * 13
		for i, value := range values {
			values[i] = byte(max(0, min(255, int(value)+offset)))
		}
	}
	switch method {
	case HalftoneDirect:
		return thresholdBitmap(values, width, height)
	case HalftoneClustered:
		return clusteredDither(values, width, height)
	case HalftoneErrorDiffusion:
		return extendedErrorDiffusion(values, width, height)
	default:
		return floydSteinberg(values, width, height)
	}
}

func thresholdBitmap(values []byte, width, height int) []byte {
	out := make([]byte, (width+7)/8*height)
	for y := range height {
		for x := range width {
			if values[y*width+x] < 128 {
				setBlack(out, width, x, y)
			}
		}
	}
	return out
}

func clusteredDither(values []byte, width, height int) []byte {
	out := make([]byte, (width+7)/8*height)
	for y := range height {
		for x := range width {
			if values[y*width+x] < clustered4x4[(y&3)*4+(x&3)] {
				setBlack(out, width, x, y)
			}
		}
	}
	return out
}

func floydSteinberg(values []byte, width, height int) []byte {
	work := make([]int, len(values))
	for i, value := range values {
		work[i] = int(value)
	}
	out := make([]byte, (width+7)/8*height)
	for y := range height {
		for x := range width {
			index := y*width + x
			old := max(0, min(255, work[index]))
			quantized := 0
			if old > 127 {
				quantized = 255
			} else {
				setBlack(out, width, x, y)
			}
			diffuse(work, width, height, x+1, y, (old-quantized)*7/16)
			diffuse(work, width, height, x-1, y+1, (old-quantized)*3/16)
			diffuse(work, width, height, x, y+1, (old-quantized)*5/16)
			diffuse(work, width, height, x+1, y+1, (old-quantized)/16)
		}
	}
	return out
}

func extendedErrorDiffusion(values []byte, width, height int) []byte {
	work := make([]int, len(values))
	for i, value := range values {
		work[i] = int(value)
	}
	out := make([]byte, (width+7)/8*height)
	for y := range height {
		for x := range width {
			index := y*width + x
			old := max(0, min(255, work[index]))
			quantized := 0
			if old > 127 {
				quantized = 255
			} else {
				setBlack(out, width, x, y)
			}
			errorValue := old - quantized
			for _, weight := range errorDiffusionWeights {
				diffuse(work, width, height, x+weight.dx, y+weight.dy, errorValue*weight.numerator/42)
			}
		}
	}
	return out
}

func diffuse(values []int, width, height, x, y, errorValue int) {
	if x >= 0 && x < width && y >= 0 && y < height {
		values[y*width+x] += errorValue
	}
}

func setBlack(bitmap []byte, width, x, y int) {
	widthBytes := (width + 7) / 8
	bitmap[y*widthBytes+x/8] |= 0x80 >> (x % 8)
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
