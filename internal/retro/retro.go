// Package retro provides read-only visibility into the ROM library
// stored at {DataDir}/roms/<console>/ . There is intentionally no
// write path here in v1: no metadata pipeline, no cover art, no
// format gating. Every regular file in a console folder IS a game,
// because nobody has verified which formats RetroArch cores on this
// hardware actually accept yet, and pretending otherwise would be
// architecture promising something untested.
//
// Security posture (matching icons' discipline): the console id is
// validated against a fixed allowlist BEFORE any filesystem path is
// built, so no client-supplied string ever becomes a path component.
// game_id is never turned into a path at all — existence is checked
// by scanning the real directory and comparing byte-for-byte.
package retro

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrUnknownConsole is returned for any console id outside the fixed
// v1 allowlist. Callers (HTTP layer in wave 3) map this to 422.
var ErrUnknownConsole = errors.New("retro: unknown console id")

type consoleDef struct {
	id   string
	name string
}

// consoleList is deliberately an ordered slice, not a map: the API
// must return consoles in this canonical order every time, and PS2
// is absent entirely by explicit project decision (a "try it and
// see" experiment, never an architectural promise).
var consoleList = []consoleDef{
	{"nes", "NES"},
	{"snes", "SNES"},
	{"genesis", "Genesis"},
	{"ps1", "PS1"},
	{"n64", "N64"},
}

// ValidateConsole enforces the single source of truth for what counts
// as a console id. Both the launch path (wave 2) and the games-listing
// path go through THIS function — one copy of the logic, no drift.
func ValidateConsole(console string) error {
	for _, c := range consoleList {
		if c.id == console {
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrUnknownConsole, console)
}

// Store reads from <dataDir>/roms/<console>/ . Stateless beyond the
// root path; safe for concurrent use because every call re-scans.
type Store struct {
	dataDir string
}

func New(dataDir string) *Store {
	return &Store{dataDir: dataDir}
}

func (s *Store) romDir(console string) string {
	return filepath.Join(s.dataDir, "roms", console)
}

type Console struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

type Game struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Consoles lists every v1 console in canonical order. available means
// the console's ROM directory currently contains at least one listed
// game — genuinely scanned, never hardcoded optimism.
func (s *Store) Consoles() []Console {
	out := make([]Console, 0, len(consoleList))
	for _, c := range consoleList {
		games, err := s.Games(c.id)
		if err != nil {
			games = nil
		}
		out = append(out, Console{ID: c.id, Name: c.name, Available: len(games) > 0})
	}
	return out
}

// Games lists one console's ROMs. Flat listing only: subdirectories
// are skipped, never recursed into (deliberate v1 decision — nested
// ROM organization would be a future choice, not an accident).
// Dotfiles (.DS_Store, .gitkeep) are also skipped: that is not
// content gatekeeping, it is not lying about what a file is. A
// missing directory is the expected fresh-install state and yields
// an empty list, NOT an error; any other read error is propagated
// loudly rather than silently looking like "no games".
func (s *Store) Games(console string) ([]Game, error) {
	if err := ValidateConsole(console); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.romDir(console))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []Game{}, nil
		}
		return nil, fmt.Errorf("retro: reading rom dir for %q: %w", console, err)
	}
	games := make([]Game, 0, len(entries))
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		title := name
		if i := strings.LastIndexByte(name, '.'); i > 0 {
			title = name[:i]
		}
		games = append(games, Game{ID: name, Title: title})
	}
	return games, nil
}

// GameExists checks game_id against a real scan of the directory —
// byte-for-byte, case-sensitive — instead of ever building a path
// from unchecked client input. Traversal-shaped ids simply never
// match anything, which is exactly the desired failure mode.
func (s *Store) GameExists(console, gameID string) (bool, error) {
	games, err := s.Games(console)
	if err != nil {
		return false, err
	}
	for _, g := range games {
		if g.ID == gameID {
			return true, nil
		}
	}
	return false, nil
}
