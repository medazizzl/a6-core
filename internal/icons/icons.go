// Package icons implements storage, validation, and retrieval of
// shortcut icon files, per the frozen spec §14. Icons are stored as
// flat files under the state store's icons directory, named by a
// generated icon_id plus the real detected extension — never by any
// client-supplied filename.
package icons

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"a6core/internal/state"
)

// MaxIconSize is the hard cap on icon file size, per spec §14.
// Enforced here as well as (Wave 5) at the HTTP transport layer via
// http.MaxBytesReader — defense in depth: this package must be safe
// to call directly, not just safe behind one specific handler.
const MaxIconSize = 512 * 1024 // 512KB

// idPattern matches exactly the UUID shape produced by state.NewID()
// (8-4-4-4-12 lowercase hex). icon_id is validated against this
// BEFORE it is ever used to build a filesystem path — this is what
// closes off path traversal (spec §15): a value like
// "../../etc/passwd" simply never reaches os.Open.
var idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var (
	ErrInvalidID        = fmt.Errorf("icons: invalid icon id")
	ErrTooLarge         = fmt.Errorf("icons: image exceeds maximum size of %d bytes", MaxIconSize)
	ErrUnsupportedType  = fmt.Errorf("icons: unsupported image type (only PNG and JPEG are accepted)")
	ErrNotFound         = fmt.Errorf("icons: icon not found")
)

// extensionFor maps a sniffed content type to the file extension
// A6 Core stores it under. Only types present here are ever accepted
// by Save, regardless of filename or claimed Content-Type.
var extensionFor = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
}

// Store manages icon files under a state.Store's configured icons
// directory (state.Store.IconsDir(), established in Stage 2).
type Store struct {
	stateStore    *state.Store
	faviconClient *http.Client
}

// New wraps an existing state.Store for icon file management. It
// does not create the icons directory itself — state.Open already
// does that (Stage 2).
func New(stateStore *state.Store) *Store {
	return &Store{stateStore: stateStore, faviconClient: newFaviconClient()}
}

// Save validates data (size, real sniffed content type), generates a
// new icon_id, and writes it atomically (temp file + fsync + rename
// — the same pattern state.Store has used since Stage 2, applied
// here because this HDD has already had one real forced-shutdown
// incident) to the icons directory. Returns the new icon_id.
func (s *Store) Save(data []byte) (iconID string, err error) {
	if len(data) > MaxIconSize {
		return "", ErrTooLarge
	}

	sniffLen := len(data)
	if sniffLen > 512 {
		sniffLen = 512
	}
	contentType := http.DetectContentType(data[:sniffLen])
	ext, ok := extensionFor[contentType]
	if !ok {
		return "", ErrUnsupportedType
	}

	iconID = state.NewID()
	dir := s.stateStore.IconsDir()
	target := filepath.Join(dir, iconID+ext)

	if err := writeFileAtomic(dir, target, data); err != nil {
		return "", fmt.Errorf("icons: saving %s: %w", iconID, err)
	}
	return iconID, nil
}

// Exists reports whether iconID has a valid shape AND refers to a
// real file on disk. Used later (Wave 4) so shortcuts can enforce
// that icon_id must reference something real.
func (s *Store) Exists(iconID string) bool {
	_, _, err := s.locate(iconID)
	return err == nil
}

// Get validates iconID, locates its file, and returns its bytes
// along with the real content type (re-sniffed at read time, not
// trusted from the filename), ready to serve directly.
func (s *Store) Get(iconID string) (data []byte, contentType string, err error) {
	path, _, err := s.locate(iconID)
	if err != nil {
		return nil, "", err
	}

	data, err = os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("icons: reading %s: %w", iconID, err)
	}

	sniffLen := len(data)
	if sniffLen > 512 {
		sniffLen = 512
	}
	contentType = http.DetectContentType(data[:sniffLen])
	return data, contentType, nil
}

// locate validates iconID's shape first, then searches for a
// matching file under any accepted extension (Save doesn't record
// which extension it used anywhere except the filename itself).
func (s *Store) locate(iconID string) (path string, ext string, err error) {
	if !idPattern.MatchString(iconID) {
		return "", "", ErrInvalidID
	}
	dir := s.stateStore.IconsDir()
	for _, candidateExt := range extensionFor {
		candidate := filepath.Join(dir, iconID+candidateExt)
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, candidateExt, nil
		}
	}
	return "", "", ErrNotFound
}

// writeFileAtomic writes data to target via a temp-file-then-rename
// sequence within dir, so a reader — or a power loss — never
// observes a partially-written icon file. Mirrors state.Store's
// saveLocked pattern from Stage 2.
func writeFileAtomic(dir, target string, data []byte) error {
	tmp, err := os.CreateTemp(dir, "icon.tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, target)
}

