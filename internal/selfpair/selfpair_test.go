package selfpair

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFirstRunPairsAndSavesKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/pair/generate", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(pairGenerateResponse{Token: "TESTTOKEN", ExpiresAt: time.Now().Add(15 * time.Minute)})
	})
	mux.HandleFunc("POST /v1/pair", func(w http.ResponseWriter, r *http.Request) {
		var req pairRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("bad request body: %v", err)
		}
		if req.Token != "TESTTOKEN" {
			t.Fatalf("expected token TESTTOKEN, got %q", req.Token)
		}
		if req.DeviceName != deviceName {
			t.Fatalf("expected device name %q, got %q", deviceName, req.DeviceName)
		}
		json.NewEncoder(w).Encode(pairResponse{
			DeviceID:   "dev-1",
			DeviceKey:  "SECRETKEY123",
			CoreName:   "Test Core",
			APIVersion: "1.0",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	key, err := EnsureDeviceKey(srv.URL)
	if err != nil {
		t.Fatalf("EnsureDeviceKey failed: %v", err)
	}
	if key != "SECRETKEY123" {
		t.Fatalf("expected key SECRETKEY123, got %q", key)
	}

	path, _ := KeyFilePath()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected key file to exist: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected file mode 0600, got %v", info.Mode().Perm())
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading stored key: %v", err)
	}
	if string(stored) != "SECRETKEY123" {
		t.Fatalf("stored key mismatch: %q", string(stored))
	}
}

func TestSecondRunReusesStoredKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, _ := KeyFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("EXISTINGKEY"), 0o600); err != nil {
		t.Fatalf("setup write: %v", err)
	}

	generateCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/pair/generate", func(w http.ResponseWriter, r *http.Request) {
		generateCalls++
		json.NewEncoder(w).Encode(pairGenerateResponse{})
	})
	mux.HandleFunc("GET /v1/info", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer EXISTINGKEY" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	key, err := EnsureDeviceKey(srv.URL)
	if err != nil {
		t.Fatalf("EnsureDeviceKey failed: %v", err)
	}
	if key != "EXISTINGKEY" {
		t.Fatalf("expected existing key to be reused, got %q", key)
	}
	if generateCalls != 0 {
		t.Fatalf("expected no pairing attempt when a valid key already exists, got %d calls", generateCalls)
	}
}

func TestRejectedStoredKeyFailsLoud(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, _ := KeyFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("REVOKEDKEY"), 0o600); err != nil {
		t.Fatalf("setup write: %v", err)
	}

	generateCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/pair/generate", func(w http.ResponseWriter, r *http.Request) {
		generateCalls++
		json.NewEncoder(w).Encode(pairGenerateResponse{})
	})
	mux.HandleFunc("GET /v1/info", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := EnsureDeviceKey(srv.URL)
	if err == nil {
		t.Fatal("expected an error when the stored key is rejected, got nil")
	}
	if !errors.Is(err, ErrStoredKeyRejected) {
		t.Fatalf("expected ErrStoredKeyRejected, got: %v", err)
	}
	if generateCalls != 0 {
		t.Fatalf("must never silently re-pair after a rejected key, but pairing was attempted %d times", generateCalls)
	}
}
