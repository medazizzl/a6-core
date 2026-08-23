package icons

import (
	"bytes"
	"encoding/binary"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"a6core/internal/state"
)

func buildTestICO(width, height int, pixel color.RGBA) []byte {
	bpp := 32
	rowSize := ((width*4 + 3) / 4) * 4
	pixelDataSize := rowSize * height
	dibSize := 40 + pixelDataSize

	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(1))

	buf.WriteByte(byte(width))
	buf.WriteByte(byte(height))
	buf.WriteByte(0)
	buf.WriteByte(0)
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(bpp))
	binary.Write(&buf, binary.LittleEndian, uint32(dibSize))
	binary.Write(&buf, binary.LittleEndian, uint32(22))

	binary.Write(&buf, binary.LittleEndian, uint32(40))
	binary.Write(&buf, binary.LittleEndian, int32(width))
	binary.Write(&buf, binary.LittleEndian, int32(height*2))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(bpp))
	binary.Write(&buf, binary.LittleEndian, uint32(0))
	binary.Write(&buf, binary.LittleEndian, uint32(pixelDataSize))
	binary.Write(&buf, binary.LittleEndian, int32(0))
	binary.Write(&buf, binary.LittleEndian, int32(0))
	binary.Write(&buf, binary.LittleEndian, uint32(0))
	binary.Write(&buf, binary.LittleEndian, uint32(0))

	row := make([]byte, rowSize)
	for x := 0; x < width; x++ {
		row[x*4+0] = pixel.B
		row[x*4+1] = pixel.G
		row[x*4+2] = pixel.R
		row[x*4+3] = pixel.A
	}
	for y := 0; y < height; y++ {
		buf.Write(row)
	}
	return buf.Bytes()
}

func TestDecodeICOToPNG(t *testing.T) {
	want := color.RGBA{R: 200, G: 100, B: 50, A: 255}
	pngData, err := decodeICOToPNG(buildTestICO(4, 4, want))
	if err != nil {
		t.Fatalf("decodeICOToPNG: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		t.Fatalf("decoding result as png: %v", err)
	}
	r, g, b, a := img.At(0, 0).RGBA()
	if uint8(r>>8) != want.R || uint8(g>>8) != want.G || uint8(b>>8) != want.B || uint8(a>>8) != want.A {
		t.Fatalf("pixel mismatch: got (%d,%d,%d,%d), want %+v", r>>8, g>>8, b>>8, a>>8, want)
	}
}

func TestDecodeICORejectsGarbage(t *testing.T) {
	if _, err := decodeICOToPNG([]byte("definitely not an ico file")); err == nil {
		t.Fatal("expected an error for garbage input")
	}
}

func TestFetchFaviconConvertsICO(t *testing.T) {
	icoBytes := buildTestICO(4, 4, color.RGBA{R: 11, G: 22, B: 33, A: 255})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/x-icon")
		w.Write(icoBytes)
	}))
	defer ts.Close()

	stateStore, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	store := New(stateStore)
	store.SetFaviconClientForTest(ts.Client())

	iconID, err := store.FetchFavicon(ts.URL)
	if err != nil {
		t.Fatalf("FetchFavicon: %v", err)
	}
	_, contentType, err := store.Get(iconID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if contentType != "image/png" {
		t.Fatalf("expected the converted icon to be stored as png, got %q", contentType)
	}
}
