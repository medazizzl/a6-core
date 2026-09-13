// Package cloud implements the personal photo/video/file storage
// feature: real username/password accounts (deliberately separate
// from device pairing — see state.CloudUser), revocable login
// sessions (real Logout, not just a client-side token delete), and
// file storage on the Acer's own disk, categorized by real detected
// MIME type so photos, videos, and everything else land in separate
// directories from day one — the same storage layout the future
// video gallery and generic file browser will reuse unchanged.
package cloud

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"a6core/internal/state"
)

var (
	ErrUsernameTaken      = errors.New("cloud: username already taken")
	ErrInvalidCredentials = errors.New("cloud: invalid username or password")
	ErrSessionInvalid     = errors.New("cloud: session invalid or expired")
	ErrFileNotFound       = errors.New("cloud: file not found")
	ErrNotOwner           = errors.New("cloud: not the owner of this file")
)

type Store struct {
	state   *state.Store
	baseDir string // <data_dir>/cloud
}

// New creates a cloud Store rooted at <dataDir>/cloud. The directory
// is created lazily on first real upload, not here — an empty,
// never-used cloud feature shouldn't leave directories behind.
func New(st *state.Store, dataDir string) *Store {
	return &Store{state: st, baseDir: filepath.Join(dataDir, "cloud")}
}

func newRandomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("cloud: generating token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Register creates a new account. Deliberately does NOT log the new
// user in automatically — a separate, explicit Login call is what
// the frozen-spec-style discipline elsewhere in this codebase favors
// (pairing and login are always separate, explicit steps, never
// bundled together where one implies the other).
func (s *Store) Register(username, password string) (state.CloudUser, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return state.CloudUser{}, errors.New("cloud: username cannot be empty")
	}
	if len(password) < 8 {
		return state.CloudUser{}, errors.New("cloud: password must be at least 8 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return state.CloudUser{}, fmt.Errorf("cloud: hashing password: %w", err)
	}

	user := state.CloudUser{
		ID:           newID(),
		Username:     username,
		PasswordHash: string(hash),
		CreatedAt:    time.Now(),
	}

	err = s.state.Update(func(st *state.State) error {
		for _, u := range st.CloudUsers {
			if strings.EqualFold(u.Username, username) {
				return ErrUsernameTaken
			}
		}
		st.CloudUsers = append(st.CloudUsers, user)
		return nil
	})
	if err != nil {
		return state.CloudUser{}, err
	}
	return user, nil
}

// Login verifies credentials and creates a real, revocable session.
// "Stay signed in" needs ZERO backend logic — it's purely whether the
// CALLER (the app) chooses to persist the returned token or not. The
// token itself is identical either way.
func (s *Store) Login(username, password string) (rawToken string, user state.CloudUser, err error) {
	snap := s.state.Snapshot()
	var found *state.CloudUser
	for i := range snap.CloudUsers {
		if strings.EqualFold(snap.CloudUsers[i].Username, username) {
			found = &snap.CloudUsers[i]
			break
		}
	}
	if found == nil {
		// Deliberately identical error to a wrong password -- never
		// reveal whether a username exists at all.
		return "", state.CloudUser{}, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(found.PasswordHash), []byte(password)) != nil {
		return "", state.CloudUser{}, ErrInvalidCredentials
	}

	rawToken, err = newRandomToken()
	if err != nil {
		return "", state.CloudUser{}, err
	}
	session := state.CloudSession{
		ID:        newID(),
		UserID:    found.ID,
		TokenHash: hashToken(rawToken),
		CreatedAt: time.Now(),
	}
	err = s.state.Update(func(st *state.State) error {
		st.CloudSessions = append(st.CloudSessions, session)
		return nil
	})
	if err != nil {
		return "", state.CloudUser{}, err
	}
	return rawToken, *found, nil
}

// Logout really revokes the session server-side -- this is what
// makes Logout meaningful rather than cosmetic: even a copy of the
// old token sitting somewhere else stops working immediately.
func (s *Store) Logout(rawToken string) error {
	target := hashToken(rawToken)
	return s.state.Update(func(st *state.State) error {
		out := st.CloudSessions[:0]
		for _, sess := range st.CloudSessions {
			if sess.TokenHash != target {
				out = append(out, sess)
			}
		}
		st.CloudSessions = out
		return nil
	})
}

// ValidateSession is the cloud equivalent of auth.ValidateDeviceKey --
// same constant-time comparison discipline, same "no session found"
// vs "found but doesn't match" indistinguishability.
func (s *Store) ValidateSession(rawToken string) (state.CloudUser, error) {
	if rawToken == "" {
		return state.CloudUser{}, ErrSessionInvalid
	}
	targetHash := hashToken(rawToken)
	snap := s.state.Snapshot()

	var userID string
	found := false
	for _, sess := range snap.CloudSessions {
		if subtle.ConstantTimeCompare([]byte(sess.TokenHash), []byte(targetHash)) == 1 {
			userID = sess.UserID
			found = true
			break
		}
	}
	if !found {
		return state.CloudUser{}, ErrSessionInvalid
	}
	for _, u := range snap.CloudUsers {
		if u.ID == userID {
			return u, nil
		}
	}
	return state.CloudUser{}, ErrSessionInvalid
}

// categoryForMIME buckets a real detected MIME type into one of the
// three storage categories. Deliberately based on the MIME type A6
// Core itself detects from file content (via http.DetectContentType
// in the handler), never trusted from a client-supplied filename
// extension or header, which could easily lie.
func categoryForMIME(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return state.CloudCategoryPhoto
	case strings.HasPrefix(mimeType, "video/"):
		return state.CloudCategoryVideo
	default:
		return state.CloudCategoryOther
	}
}

// SaveFile writes real file bytes to disk under
// <baseDir>/<ownerID>/<category>/<storedName> and records the
// metadata. storedName is generated here, not taken from the
// client's filename -- the original Filename is preserved for
// display only, never used as an actual path component (a
// deliberately hostile filename like "../../etc/passwd" must never
// become a real path).
func (s *Store) SaveFile(ownerID, filename, mimeType string, r io.Reader) (state.CloudFile, error) {
	category := categoryForMIME(mimeType)
	storedName := newID() + storedExtensionFor(mimeType, filename)

	dir := filepath.Join(s.baseDir, ownerID, category)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return state.CloudFile{}, fmt.Errorf("cloud: creating storage dir: %w", err)
	}

	fullPath := filepath.Join(dir, storedName)
	out, err := os.Create(fullPath)
	if err != nil {
		return state.CloudFile{}, fmt.Errorf("cloud: creating file: %w", err)
	}
	defer out.Close()

	written, err := io.Copy(out, r)
	if err != nil {
		return state.CloudFile{}, fmt.Errorf("cloud: writing file: %w", err)
	}

	rec := state.CloudFile{
		ID:         newID(),
		OwnerID:    ownerID,
		Category:   category,
		Filename:   sanitizeDisplayName(filename),
		StoredName: storedName,
		SizeBytes:  written,
		MimeType:   mimeType,
		UploadedAt: time.Now(),
	}
	err = s.state.Update(func(st *state.State) error {
		st.CloudFiles = append(st.CloudFiles, rec)
		return nil
	})
	if err != nil {
		return state.CloudFile{}, err
	}
	return rec, nil
}

// sanitizeDisplayName strips any path-like content from a
// client-supplied filename before it's ever stored or shown --
// display-only, never used to build a real filesystem path.
func sanitizeDisplayName(name string) string {
	name = filepath.Base(name) // drops any directory components
	if name == "." || name == "/" || name == "" {
		return "unnamed"
	}
	return name
}

func storedExtensionFor(mimeType, originalFilename string) string {
	if exts, err := mime.ExtensionsByType(mimeType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return filepath.Ext(sanitizeDisplayName(originalFilename))
}

// ListFiles returns an owner's files, optionally filtered by
// category ("" means all categories).
func (s *Store) ListFiles(ownerID, category string) []state.CloudFile {
	snap := s.state.Snapshot()
	out := make([]state.CloudFile, 0)
	for _, f := range snap.CloudFiles {
		if f.OwnerID != ownerID {
			continue
		}
		if category != "" && f.Category != category {
			continue
		}
		out = append(out, f)
	}
	return out
}

// OpenFile returns the file's metadata and a real, readable handle
// to its content on disk. Ownership is checked here, not left to the
// caller -- a file ID alone is never sufficient to read it back,
// only the real owner's session can.
func (s *Store) OpenFile(ownerID, fileID string) (state.CloudFile, io.ReadCloser, error) {
	snap := s.state.Snapshot()
	var rec *state.CloudFile
	for i := range snap.CloudFiles {
		if snap.CloudFiles[i].ID == fileID {
			rec = &snap.CloudFiles[i]
			break
		}
	}
	if rec == nil {
		return state.CloudFile{}, nil, ErrFileNotFound
	}
	if rec.OwnerID != ownerID {
		return state.CloudFile{}, nil, ErrNotOwner
	}
	f, err := os.Open(filepath.Join(s.baseDir, rec.OwnerID, rec.Category, rec.StoredName))
	if err != nil {
		return state.CloudFile{}, nil, fmt.Errorf("cloud: opening stored file: %w", err)
	}
	return *rec, f, nil
}

// DeleteFile removes both the real file on disk and its metadata.
// Ownership-checked the same way OpenFile is.
func (s *Store) DeleteFile(ownerID, fileID string) error {
	snap := s.state.Snapshot()
	var rec *state.CloudFile
	for i := range snap.CloudFiles {
		if snap.CloudFiles[i].ID == fileID {
			rec = &snap.CloudFiles[i]
			break
		}
	}
	if rec == nil {
		return ErrFileNotFound
	}
	if rec.OwnerID != ownerID {
		return ErrNotOwner
	}

	err := s.state.Update(func(st *state.State) error {
		out := st.CloudFiles[:0]
		for _, f := range st.CloudFiles {
			if f.ID != fileID {
				out = append(out, f)
			}
		}
		st.CloudFiles = out
		return nil
	})
	if err != nil {
		return err
	}

	// Best-effort: the metadata is already gone (the source of
	// truth), so a failure to remove the real bytes is logged-worthy
	// but not something to fail the whole delete over -- an orphaned
	// file on disk is a cheap cleanup problem, a delete that silently
	// didn't happen is a worse one.
	path := filepath.Join(s.baseDir, rec.OwnerID, rec.Category, rec.StoredName)
	_ = os.Remove(path)
	return nil
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
