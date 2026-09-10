package retro

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGamePathReturnsRealPathForExistingGame(t *testing.T) {
	dir := t.TempDir()
	romDir := filepath.Join(dir, "roms", "nes")
	if err := os.MkdirAll(romDir, 0o755); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(romDir, "test.nes"), []byte("fake rom data"), 0o644); err != nil {
		t.Fatalf("setup write: %v", err)
	}

	store := New(dir)
	path, err := store.GamePath("nes", "test.nes")
	if err != nil {
		t.Fatalf("GamePath: %v", err)
	}
	want := filepath.Join(romDir, "test.nes")
	if path != want {
		t.Fatalf("GamePath = %q, want %q", path, want)
	}
}

func TestGamePathRejectsUnknownGame(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	if _, err := store.GamePath("nes", "nonexistent.nes"); err == nil {
		t.Fatal("expected an error for a game that does not exist, got nil")
	}
}

func TestGamePathRejectsTraversalAttempt(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	if _, err := store.GamePath("nes", "../../../etc/passwd"); err == nil {
		t.Fatal("expected an error for a traversal-shaped game id, got nil")
	}
}
