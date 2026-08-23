package icons

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"a6core/internal/state"
)

func newTestIconStore(t *testing.T) *Store {
	t.Helper()
	stateStore, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	return New(stateStore)
}

// smallPNG returns a tiny, genuinely valid, encoded 1x1 PNG — needed
// because Save/Get rely on http.DetectContentType sniffing real
// image bytes, not a file extension or a claimed content type.
func smallPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding test PNG: %v", err)
	}
	return buf.Bytes()
}

func TestSaveAndGetRoundTrip(t *testing.T) {
	store := newTestIconStore(t)
	data := smallPNG(t)

	id, err := store.Save(data)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !idPattern.MatchString(id) {
		t.Fatalf("Save returned an id not matching the expected shape: %q", id)
	}

	got, contentType, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if contentType != "image/png" {
		t.Fatalf("expected content type image/png, got %q", contentType)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("retrieved icon bytes do not match what was saved")
	}
}

func TestExists(t *testing.T) {
	store := newTestIconStore(t)
	id, err := store.Save(smallPNG(t))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !store.Exists(id) {
		t.Fatal("expected Exists to be true for a freshly saved icon")
	}
	if store.Exists("00000000-0000-0000-0000-000000000000") {
		t.Fatal("expected Exists to be false for a well-formed but nonexistent id")
	}
}

func TestSaveRejectsOversized(t *testing.T) {
	store := newTestIconStore(t)
	oversized := make([]byte, MaxIconSize+1)
	if _, err := store.Save(oversized); err != ErrTooLarge {
		t.Fatalf("expected ErrTooLarge, got %v", err)
	}
}

func TestSaveRejectsUnsupportedType(t *testing.T) {
	store := newTestIconStore(t)
	if _, err := store.Save([]byte("this is not an image, just text")); err != ErrUnsupportedType {
		t.Fatalf("expected ErrUnsupportedType, got %v", err)
	}
}

func TestGetRejectsPathTraversal(t *testing.T) {
	store := newTestIconStore(t)
	traversalAttempts := []string{
		"../../../etc/passwd",
		"../../etc/passwd",
		"foo/bar",
		"not-a-uuid-at-all",
		"",
	}
	for _, attempt := range traversalAttempts {
		if _, _, err := store.Get(attempt); err != ErrInvalidID {
			t.Fatalf("Get(%q): expected ErrInvalidID, got %v", attempt, err)
		}
	}
}

func TestGetNotFound(t *testing.T) {
	store := newTestIconStore(t)
	_, _, err := store.Get("00000000-0000-0000-0000-000000000000")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
