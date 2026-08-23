package shortcuts

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"a6core/internal/icons"
	"a6core/internal/state"
)

func smallPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding test PNG: %v", err)
	}
	return buf.Bytes()
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	stateStore, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	iconStore := icons.New(stateStore)
	return New(stateStore, iconStore)
}

// favicon server for the "auto-fetch succeeds" path.
func newFaviconServer(t *testing.T, iconBytes []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/favicon.ico" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(iconBytes)
	}))
}

func TestCreateWithExplicitIcon(t *testing.T) {
	stateStore, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	iconStore := icons.New(stateStore)
	scStore := New(stateStore, iconStore)

	iconID, err := iconStore.Save(smallPNG(t))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	sc, err := scStore.Create("YouTube", "https://youtube.com", iconID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sc.IconID != iconID {
		t.Fatalf("expected explicit icon_id to be used as-is, got %q", sc.IconID)
	}
	if sc.Name != "YouTube" || sc.URL != "https://youtube.com" {
		t.Fatalf("unexpected shortcut fields: %+v", sc)
	}
}

func TestCreateWithNonexistentExplicitIcon(t *testing.T) {
	scStore := newTestStore(t)
	_, err := scStore.Create("Site", "https://example.com", "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrIconNotFound) {
		t.Fatalf("expected ErrIconNotFound, got %v", err)
	}
}

func TestCreateRejectsInvalidURL(t *testing.T) {
	scStore := newTestStore(t)
	cases := []string{"not a url", "ftp://example.com", "javascript:alert(1)", ""}
	for _, u := range cases {
		if _, err := scStore.Create("Site", u, ""); !errors.Is(err, ErrInvalidURL) {
			t.Errorf("Create(url=%q): expected ErrInvalidURL, got %v", u, err)
		}
	}
}

func TestCreateRejectsEmptyName(t *testing.T) {
	scStore := newTestStore(t)
	if _, err := scStore.Create("   ", "https://example.com", ""); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("expected ErrInvalidName, got %v", err)
	}
}

func TestCreateAutoFaviconSuccess(t *testing.T) {
	stateStore, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	iconStore := icons.New(stateStore)
	iconBytes := smallPNG(t)
	ts := newFaviconServer(t, iconBytes)
	defer ts.Close()
	iconStore.SetFaviconClientForTest(ts.Client())
	scStore := New(stateStore, iconStore)

	sc, err := scStore.Create("Test Site", ts.URL, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sc.IconID == "" {
		t.Fatal("expected a fetched favicon icon_id, got empty (default) instead")
	}
	if !iconStore.Exists(sc.IconID) {
		t.Fatal("expected fetched favicon to actually be saved")
	}
}

func TestCreateAutoFaviconFailureFallsBackToDefault(t *testing.T) {
	stateStore, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	iconStore := icons.New(stateStore)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()
	iconStore.SetFaviconClientForTest(ts.Client())
	scStore := New(stateStore, iconStore)

	sc, err := scStore.Create("Test Site", ts.URL, "")
	if err != nil {
		t.Fatalf("Create should soft-fail to empty icon_id, not error: %v", err)
	}
	if sc.IconID != "" {
		t.Fatalf("expected empty icon_id (embedded default) on favicon failure, got %q", sc.IconID)
	}
}

func TestListReturnsEmptyNotNil(t *testing.T) {
	scStore := newTestStore(t)
	list := scStore.List()
	if list == nil {
		t.Fatal("expected List() to return an empty slice, not nil")
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 shortcuts, got %d", len(list))
	}
}

func TestUpdateRename(t *testing.T) {
	stateStore, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	iconStore := icons.New(stateStore)
	scStore := New(stateStore, iconStore)

	sc, err := scStore.Create("Old Name", "https://example.com", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	newName := "New Name"
	updated, err := scStore.Update(sc.ID, UpdateInput{Name: &newName})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "New Name" {
		t.Fatalf("expected renamed shortcut, got %+v", updated)
	}
	if updated.IconID != sc.IconID {
		t.Fatal("a plain rename must not change icon_id")
	}
}

func TestUpdateExplicitIconOverride(t *testing.T) {
	stateStore, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	iconStore := icons.New(stateStore)
	scStore := New(stateStore, iconStore)

	sc, err := scStore.Create("Site", "https://example.com", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	newIconID, err := iconStore.Save(smallPNG(t))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	updated, err := scStore.Update(sc.ID, UpdateInput{IconID: &newIconID})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.IconID != newIconID {
		t.Fatalf("expected icon_id override to take effect, got %q", updated.IconID)
	}
}

func TestUpdateClearIconToDefault(t *testing.T) {
	stateStore, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	iconStore := icons.New(stateStore)
	scStore := New(stateStore, iconStore)

	iconID, err := iconStore.Save(smallPNG(t))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	sc, err := scStore.Create("Site", "https://example.com", iconID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	empty := ""
	updated, err := scStore.Update(sc.ID, UpdateInput{IconID: &empty})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.IconID != "" {
		t.Fatalf("expected explicit clear to result in empty icon_id, got %q", updated.IconID)
	}
}

func TestUpdateNotFound(t *testing.T) {
	scStore := newTestStore(t)
	name := "X"
	_, err := scStore.Update("00000000-0000-0000-0000-000000000000", UpdateInput{Name: &name})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteThenGetNotFound(t *testing.T) {
	scStore := newTestStore(t)
	sc, err := scStore.Create("Site", "https://example.com", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := scStore.Delete(sc.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := scStore.Get(sc.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestDeleteNotFound(t *testing.T) {
	scStore := newTestStore(t)
	if err := scStore.Delete("00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
