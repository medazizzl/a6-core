package cloud

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"a6core/internal/state"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	return New(st, t.TempDir())
}

func TestRegister_Success(t *testing.T) {
	s := newTestStore(t)
	user, err := s.Register("aziz", "correcthorsebattery")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if user.Username != "aziz" {
		t.Fatalf("expected username 'aziz', got %q", user.Username)
	}
	if user.PasswordHash == "correcthorsebattery" {
		t.Fatal("password was stored in plaintext, not hashed")
	}
}

func TestRegister_DuplicateUsernameRejected(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Register("aziz", "correcthorsebattery"); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	// Case-insensitive on purpose -- "Aziz" and "aziz" must be treated
	// as the same account, not a way to bypass the uniqueness check.
	if _, err := s.Register("Aziz", "differentpassword"); err != ErrUsernameTaken {
		t.Fatalf("expected ErrUsernameTaken, got %v", err)
	}
}

func TestRegister_ShortPasswordRejected(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Register("aziz", "short"); err == nil {
		t.Fatal("expected an error for a too-short password, got nil")
	}
}

func TestLogin_Success(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Register("aziz", "correcthorsebattery"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	token, user, err := s.Login("aziz", "correcthorsebattery")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if token == "" {
		t.Fatal("expected a non-empty token")
	}
	if user.Username != "aziz" {
		t.Fatalf("expected username 'aziz', got %q", user.Username)
	}
}

func TestLogin_WrongPasswordRejected(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Register("aziz", "correcthorsebattery"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, _, err := s.Login("aziz", "wrongpassword"); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLogin_UnknownUsernameGivesSameErrorAsWrongPassword(t *testing.T) {
	// Deliberately checking this: a different error for "no such
	// user" vs "wrong password" would let an attacker enumerate real
	// usernames one guess at a time.
	s := newTestStore(t)
	if _, _, err := s.Login("nobody", "whatever123"); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials for unknown user, got %v", err)
	}
}

func TestValidateSession_ValidToken(t *testing.T) {
	s := newTestStore(t)
	s.Register("aziz", "correcthorsebattery")
	token, _, _ := s.Login("aziz", "correcthorsebattery")

	user, err := s.ValidateSession(token)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if user.Username != "aziz" {
		t.Fatalf("expected username 'aziz', got %q", user.Username)
	}
}

func TestValidateSession_UnknownTokenRejected(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.ValidateSession("not-a-real-token"); err != ErrSessionInvalid {
		t.Fatalf("expected ErrSessionInvalid, got %v", err)
	}
}

func TestLogout_ReallyRevokesSession(t *testing.T) {
	s := newTestStore(t)
	s.Register("aziz", "correcthorsebattery")
	token, _, _ := s.Login("aziz", "correcthorsebattery")

	if _, err := s.ValidateSession(token); err != nil {
		t.Fatalf("expected session valid before logout, got %v", err)
	}
	if err := s.Logout(token); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := s.ValidateSession(token); err != ErrSessionInvalid {
		t.Fatalf("expected session invalid after logout, got %v", err)
	}
}

func TestSaveFile_CategorizesByRealMIMEType(t *testing.T) {
	s := newTestStore(t)
	user, _ := s.Register("aziz", "correcthorsebattery")

	photo, err := s.SaveFile(user.ID, "vacation.jpg", "image/jpeg", strings.NewReader("fake jpeg bytes"))
	if err != nil {
		t.Fatalf("SaveFile (photo): %v", err)
	}
	if photo.Category != state.CloudCategoryPhoto {
		t.Errorf("expected category photo, got %q", photo.Category)
	}

	video, err := s.SaveFile(user.ID, "clip.mp4", "video/mp4", strings.NewReader("fake mp4 bytes"))
	if err != nil {
		t.Fatalf("SaveFile (video): %v", err)
	}
	if video.Category != state.CloudCategoryVideo {
		t.Errorf("expected category video, got %q", video.Category)
	}

	doc, err := s.SaveFile(user.ID, "notes.pdf", "application/pdf", strings.NewReader("fake pdf bytes"))
	if err != nil {
		t.Fatalf("SaveFile (other): %v", err)
	}
	if doc.Category != state.CloudCategoryOther {
		t.Errorf("expected category other, got %q", doc.Category)
	}
}

func TestSaveFile_MaliciousFilenameCannotEscapeStorageDir(t *testing.T) {
	s := newTestStore(t)
	user, _ := s.Register("aziz", "correcthorsebattery")

	rec, err := s.SaveFile(user.ID, "../../../../etc/passwd", "image/jpeg", strings.NewReader("data"))
	if err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	// The DISPLAY name is allowed to just say "passwd" (harmless,
	// cosmetic) -- what actually matters is verified below: the real
	// file landed inside this store's own baseDir, nowhere else.
	if strings.Contains(rec.StoredName, "..") || strings.Contains(rec.StoredName, "/") {
		t.Fatalf("stored name still contains path traversal characters: %q", rec.StoredName)
	}
	realPath := filepath.Join(s.baseDir, rec.OwnerID, rec.Category, rec.StoredName)
	if _, err := os.Stat(realPath); err != nil {
		t.Fatalf("expected the real file inside baseDir at %s, got: %v", realPath, err)
	}
}

func TestListFiles_OnlyReturnsOwnersOwnFiles(t *testing.T) {
	s := newTestStore(t)
	alice, _ := s.Register("alice", "correcthorsebattery")
	bob, _ := s.Register("bob", "correcthorsebattery")

	s.SaveFile(alice.ID, "alice-photo.jpg", "image/jpeg", strings.NewReader("a"))
	s.SaveFile(bob.ID, "bob-photo.jpg", "image/jpeg", strings.NewReader("b"))

	aliceFiles := s.ListFiles(alice.ID, "")
	if len(aliceFiles) != 1 {
		t.Fatalf("expected alice to see exactly 1 file, got %d", len(aliceFiles))
	}
	if aliceFiles[0].Filename != "alice-photo.jpg" {
		t.Fatalf("alice saw the wrong file: %q", aliceFiles[0].Filename)
	}
}

func TestListFiles_FiltersByCategory(t *testing.T) {
	s := newTestStore(t)
	user, _ := s.Register("aziz", "correcthorsebattery")
	s.SaveFile(user.ID, "photo.jpg", "image/jpeg", strings.NewReader("a"))
	s.SaveFile(user.ID, "video.mp4", "video/mp4", strings.NewReader("b"))

	photos := s.ListFiles(user.ID, state.CloudCategoryPhoto)
	if len(photos) != 1 || photos[0].Category != state.CloudCategoryPhoto {
		t.Fatalf("expected exactly 1 photo, got %d", len(photos))
	}
}

func TestOpenFile_WrongOwnerRejected(t *testing.T) {
	s := newTestStore(t)
	alice, _ := s.Register("alice", "correcthorsebattery")
	bob, _ := s.Register("bob", "correcthorsebattery")
	file, _ := s.SaveFile(alice.ID, "private.jpg", "image/jpeg", strings.NewReader("a"))

	if _, _, err := s.OpenFile(bob.ID, file.ID); err != ErrNotOwner {
		t.Fatalf("expected ErrNotOwner, got %v", err)
	}
}

func TestOpenFile_RealContentRoundTrips(t *testing.T) {
	s := newTestStore(t)
	user, _ := s.Register("aziz", "correcthorsebattery")
	const content = "these are real photo bytes, not a placeholder"
	saved, _ := s.SaveFile(user.ID, "photo.jpg", "image/jpeg", strings.NewReader(content))

	_, r, err := s.OpenFile(user.ID, saved.ID)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer r.Close()
	buf := make([]byte, len(content))
	if _, err := r.Read(buf); err != nil {
		t.Fatalf("reading file content: %v", err)
	}
	if string(buf) != content {
		t.Fatalf("expected %q, got %q", content, string(buf))
	}
}

func TestDeleteFile_RemovesRealFileAndMetadata(t *testing.T) {
	s := newTestStore(t)
	user, _ := s.Register("aziz", "correcthorsebattery")
	saved, _ := s.SaveFile(user.ID, "photo.jpg", "image/jpeg", strings.NewReader("data"))
	realPath := filepath.Join(s.baseDir, saved.OwnerID, saved.Category, saved.StoredName)

	if _, err := os.Stat(realPath); err != nil {
		t.Fatalf("expected file to exist before delete: %v", err)
	}
	if err := s.DeleteFile(user.ID, saved.ID); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if _, err := os.Stat(realPath); !os.IsNotExist(err) {
		t.Fatal("expected the real file to be gone from disk after delete")
	}
	if len(s.ListFiles(user.ID, "")) != 0 {
		t.Fatal("expected metadata to be gone after delete")
	}
}

func TestDeleteFile_WrongOwnerRejected(t *testing.T) {
	s := newTestStore(t)
	alice, _ := s.Register("alice", "correcthorsebattery")
	bob, _ := s.Register("bob", "correcthorsebattery")
	file, _ := s.SaveFile(alice.ID, "private.jpg", "image/jpeg", strings.NewReader("a"))

	if err := s.DeleteFile(bob.ID, file.ID); err != ErrNotOwner {
		t.Fatalf("expected ErrNotOwner, got %v", err)
	}
	// And the file must still genuinely be there afterward.
	if len(s.ListFiles(alice.ID, "")) != 1 {
		t.Fatal("file should NOT have been deleted by the rejected attempt")
	}
}
