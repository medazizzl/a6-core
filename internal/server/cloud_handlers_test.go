package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func registerAndLogin(t *testing.T, srv *Server, deviceKey, username, password string) string {
	t.Helper()

	regBody, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/v1/cloud/register", bytes.NewReader(regBody))
	req.Header.Set("Authorization", "Bearer "+deviceKey)
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	loginBody, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req = httptest.NewRequest(http.MethodPost, "/v1/cloud/login", bytes.NewReader(loginBody))
	req.Header.Set("Authorization", "Bearer "+deviceKey)
	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		SessionToken string `json:"session_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding login response: %v", err)
	}
	return resp.SessionToken
}

// TestRealRoute_RegisterRequiresPairedDevice proves an UNPAIRED
// caller (no valid device key at all) cannot create a cloud account
// -- the real security boundary this design depends on.
func TestRealRoute_RegisterRequiresPairedDevice(t *testing.T) {
	srv, _ := newFullTestServer(t)
	body, _ := json.Marshal(map[string]string{"username": "aziz", "password": "correcthorsebattery"})
	req := httptest.NewRequest(http.MethodPost, "/v1/cloud/register", bytes.NewReader(body))
	// No Authorization header at all.
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an unpaired caller, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestRealRoute_FullCloudLifecycle proves register -> login -> upload
// -> list -> download -> delete all work through the REAL registered
// routes, end to end, with real bytes round-tripping through real
// disk storage.
func TestRealRoute_FullCloudLifecycle(t *testing.T) {
	srv, _ := newFullTestServer(t)
	deviceKey := pairRealDevice(t, srv, "Aziz's Phone")
	token := registerAndLogin(t, srv, deviceKey, "aziz", "correcthorsebattery")

	// Upload a real (fake but real-bytes) JPEG via real multipart.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", "vacation.jpg")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	// Real JPEG magic bytes so http.DetectContentType genuinely
	// reports image/jpeg, not a guess based on the filename.
	jpegMagic := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}
	part.Write(jpegMagic)
	part.Write([]byte("...rest of a fake but real jpeg body..."))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/cloud/upload", &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var uploaded struct {
		ID       string `json:"id"`
		Category string `json:"category"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decoding upload response: %v", err)
	}
	if uploaded.Category != "photo" {
		t.Fatalf("expected real content-sniffing to detect a photo, got category %q", uploaded.Category)
	}

	// List: the uploaded file should genuinely be there.
	req = httptest.NewRequest(http.MethodGet, "/v1/cloud/files", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var files []struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &files)
	if len(files) != 1 || files[0].ID != uploaded.ID {
		t.Fatalf("expected exactly the uploaded file in the list, got %+v", files)
	}

	// Download: real bytes come back.
	req = httptest.NewRequest(http.MethodGet, "/v1/cloud/files/"+uploaded.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("download: expected 200, got %d", rec.Code)
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), jpegMagic) {
		t.Fatal("downloaded content doesn't start with the real uploaded JPEG magic bytes")
	}

	// Delete: really gone afterward.
	req = httptest.NewRequest(http.MethodDelete, "/v1/cloud/files/"+uploaded.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/cloud/files/"+uploaded.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rec.Code)
	}
}

// TestRealRoute_LogoutRevokesAccessToRealEndpoints proves Logout
// genuinely blocks further real requests with that token, not just
// deleting a client-side copy of it.
func TestRealRoute_LogoutRevokesAccessToRealEndpoints(t *testing.T) {
	srv, _ := newFullTestServer(t)
	deviceKey := pairRealDevice(t, srv, "Aziz's Phone")
	token := registerAndLogin(t, srv, deviceKey, "aziz", "correcthorsebattery")

	req := httptest.NewRequest(http.MethodPost, "/v1/cloud/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/cloud/files", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 using a logged-out token, got %d", rec.Code)
	}
}

// TestRealRoute_UsersCannotSeeEachOthersFiles proves real, separate
// accounts have real, separate file lists through the actual routes
// -- not just a package-level check.
func TestRealRoute_UsersCannotSeeEachOthersFiles(t *testing.T) {
	srv, _ := newFullTestServer(t)
	deviceKey := pairRealDevice(t, srv, "Shared Family Phone")
	aliceToken := registerAndLogin(t, srv, deviceKey, "alice", "correcthorsebattery")
	bobToken := registerAndLogin(t, srv, deviceKey, "bob", "correcthorsebattery")

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, _ := mw.CreateFormFile("file", "alice-private.jpg")
	part.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0})
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/cloud/upload", &buf)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("alice upload: expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/cloud/files", nil)
	req.Header.Set("Authorization", "Bearer "+bobToken)
	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)
	var bobFiles []any
	json.Unmarshal(rec.Body.Bytes(), &bobFiles)
	if len(bobFiles) != 0 {
		t.Fatalf("bob should see 0 files (alice's are private), saw %d", len(bobFiles))
	}
}
