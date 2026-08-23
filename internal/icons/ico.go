// This file teaches A6 Core how to read the classic Windows .ico
// file format, which many websites still use for favicon.ico even
// today. A6 Core only ever STORES icons as PNG (per the frozen
// spec), so this converts an .ico into a PNG right when it's
// fetched, rather than changing what formats are accepted anywhere
// else (like manual uploads, which stay PNG/JPEG only, unchanged).
package icons

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

type icoEntry struct {
	width, height int
	size, offset  uint32
}

// decodeICOToPNG finds the largest image inside an .ico container
// and returns it as PNG bytes. Real .ico files store their image
// either as an embedded PNG directly (handled as-is), or as an
// older raw bitmap format (handled by decodeDIBToPNG below). If
// neither is recognized, this returns an error, and the caller
// treats that the same as any other unavailable favicon — falling
// back to the built-in default icon, never blocking shortcut
// creation.
func decodeICOToPNG(data []byte) ([]byte, error) {
	if len(data) < 6 {
		return nil, fmt.Errorf("icons: ico data too short")
	}
	if binary.LittleEndian.Uint16(data[0:2]) != 0 || binary.LittleEndian.Uint16(data[2:4]) != 1 {
		return nil, fmt.Errorf("icons: not a valid ico file")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count == 0 || len(data) < 6+count*16 {
		return nil, fmt.Errorf("icons: ico directory missing or truncated")
	}

	var best icoEntry
	for i := 0; i < count; i++ {
		e := data[6+i*16 : 6+i*16+16]
		w, h := int(e[0]), int(e[1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		size := binary.LittleEndian.Uint32(e[8:12])
		offset := binary.LittleEndian.Uint32(e[12:16])
		if w*h > best.width*best.height {
			best = icoEntry{width: w, height: h, size: size, offset: offset}
		}
	}
	if best.size == 0 || int(best.offset)+int(best.size) > len(data) {
		return nil, fmt.Errorf("icons: ico entry out of bounds")
	}
	img := data[best.offset : best.offset+best.size]

	if len(img) >= 8 && bytes.Equal(img[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return img, nil // already a real PNG inside the .ico — nothing to convert
	}
	return decodeDIBToPNG(img, best.width, best.height)
}

// decodeDIBToPNG handles the older, still-common raw bitmap format
// used inside classic .ico files (24 or 32 bits per pixel,
// uncompressed only — the vast majority of real favicons).
func decodeDIBToPNG(data []byte, width, height int) ([]byte, error) {
	if len(data) < 40 {
		return nil, fmt.Errorf("icons: DIB header too short")
	}
	bpp := binary.LittleEndian.Uint16(data[14:16])
	compression := binary.LittleEndian.Uint32(data[16:20])
	if compression != 0 {
		return nil, fmt.Errorf("icons: compressed DIB not supported")
	}
	if bpp != 32 && bpp != 24 {
		return nil, fmt.Errorf("icons: unsupported DIB bit depth %d", bpp)
	}

	bytesPerPixel := int(bpp) / 8
	rowSize := ((width*bytesPerPixel + 3) / 4) * 4
	pixelsStart := 40

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		srcRow := pixelsStart + (height-1-y)*rowSize // DIB rows are stored bottom-up
		if srcRow+width*bytesPerPixel > len(data) {
			return nil, fmt.Errorf("icons: DIB pixel data truncated")
		}
		for x := 0; x < width; x++ {
			i := srcRow + x*bytesPerPixel
			a := byte(255)
			if bytesPerPixel == 4 {
				a = data[i+3]
			}
			img.Set(x, y, color.RGBA{R: data[i+2], G: data[i+1], B: data[i], A: a})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("icons: encoding converted icon: %w", err)
	}
	return buf.Bytes(), nil
}
