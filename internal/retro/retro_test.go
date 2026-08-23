package retro

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return New(t.TempDir())
}

func TestConsolesCanonicalOrderAndNoPS2(t *testing.T) {
	s := newTestStore(t)
	consoles := s.Consoles()
	wantIDs := []string{"nes", "snes", "genesis", "ps1", "n64"}
	if len(consoles) != len(wantIDs) {
		t.Fatalf("got %d consoles, want %d", len(consoles), len(wantIDs))
	}
	for i, want := range wantIDs {
		if consoles[i].ID != want {
			t.Fatalf("console[%d].id = %q, want %q", i, consoles[i].ID, want)
		}
		if consoles[i].Available {
			t.Fatalf("console %q should be unavailable with no roms dir", want)
		}
	}
}

func TestMissingRomsDirMeansEmptyNotError(t *testing.T) {
	s := newTestStore(t)
	games, err := s.Games("nes")
	if err != nil {
		t.Fatalf("missing roms dir should not error: %v", err)
	}
	if len(games) != 0 {
		t.Fatalf("expected zero games, got %d", len(games))
	}
}

func TestEmptyConsoleDirIsNotAvailable(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "roms", "nes"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := New(dir)
	games, err := s.Games("nes")
	if err != nil || len(games) != 0 {
		t.Fatalf("empty dir: games=%v err=%v", games, err)
	}
	for _, c := range s.Consoles() {
		if c.ID == "nes" && c.Available {
			t.Fatal("nes must not report available with zero games")
		}
	}
}

func TestListingFlatOnlyWithDerivedTitles(t *testing.T) {
	dir := t.TempDir()
	nes := filepath.Join(dir, "roms", "nes")
	if err := os.MkdirAll(nes, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"Super Mario Bros. 3.nes": "\x00fake",
		"Бабушка Яга.sfc":         "\x00fake",
		"plain":                   "\x00fake",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(nes, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(nes, "nested_folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nes, ".DS_Store"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New(dir)
	games, err := s.Games("nes")
	if err != nil {
		t.Fatalf("Games: %v", err)
	}
	if len(games) != 3 {
		t.Fatalf("want 3 games (subdir and dotfile skipped), got %d: %+v", len(games), games)
	}
	byID := map[string]string{}
	for _, g := range games {
		byID[g.ID] = g.Title
	}
	if byID["Super Mario Bros. 3.nes"] != "Super Mario Bros. 3" {
		t.Fatalf("spaces/extension title wrong: %q", byID["Super Mario Bros. 3.nes"])
	}
	if byID["Бабушка Яга.sfc"] != "Бабушка Яга" {
		t.Fatalf("unicode title wrong: %q", byID["Бабушка Яга.sfc"])
	}
	if byID["plain"] != "plain" {
		t.Fatalf("extensionless title wrong: %q", byID["plain"])
	}
}

func TestAvailableReflectsRealFiles(t *testing.T) {
	dir := t.TempDir()
	nes := filepath.Join(dir, "roms", "nes")
	if err := os.MkdirAll(nes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nes, "game.nes"), []byte("\x00fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir)
	for _, c := range s.Consoles() {
		switch {
		case c.ID == "nes" && !c.Available:
			t.Fatal("nes should be available with one game present")
		case c.ID == "snes" && c.Available:
			t.Fatal("snes should stay unavailable")
		case c.ID == "ps2":
			t.Fatal("ps2 must not exist in the v1 console list at all")
		}
	}
}

func TestUnknownConsoleRejectedEverywhere(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Games("ps2"); !errors.Is(err, ErrUnknownConsole) {
		t.Fatalf("Games(ps2) err = %v, want ErrUnknownConsole", err)
	}
	if _, err := s.GameExists("dreamcast", "x.bin"); !errors.Is(err, ErrUnknownConsole) {
		t.Fatalf("GameExists(dreamcast) err = %v, want ErrUnknownConsole", err)
	}
	if err := ValidateConsole("../../etc"); !errors.Is(err, ErrUnknownConsole) {
		t.Fatalf("traversal-shaped id must hit the allowlist, got %v", err)
	}
}

func TestGameExistsByteExactAndTraversalInert(t *testing.T) {
	dir := t.TempDir()
	nes := filepath.Join(dir, "roms", "nes")
	if err := os.MkdirAll(nes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nes, "Zelda.nes"), []byte("\x00fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir)

	ok, err := s.GameExists("nes", "Zelda.nes")
	if err != nil || !ok {
		t.Fatalf("exact id should exist: ok=%v err=%v", ok, err)
	}
	ok, err = s.GameExists("nes", "zelda.NES")
	if err != nil || ok {
		t.Fatalf("case-insensitive match must fail: ok=%v err=%v", ok, err)
	}
	ok, err = s.GameExists("nes", "../../etc/passwd")
	if err != nil || ok {
		t.Fatalf("traversal id must simply not match: ok=%v err=%v", ok, err)
	}
	ok, err = s.GameExists("nes", "..\\..\\windows\\system32")
	if err != nil || ok {
		t.Fatalf("windows-style traversal must simply not match: ok=%v err=%v", ok, err)
	}
}
