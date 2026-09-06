package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"a6core/internal/auth"
	"a6core/internal/events"
	"a6core/internal/state"
)

// newTestServer builds a minimal but real Server -- real state.Store
// backed by a temp dir, real PairingManager and RateLimiter, a real
// (unstarted) Hub -- everything handlePair actually touches, without
// pulling in the full New() dependency graph (apps/retro/servers
// managers etc.) that reboot/shutdown/pairing tests don't need.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	return &Server{
		store:     store,
		pairing:   auth.NewPairingManager(),
		rateLimit: auth.NewRateLimiter(),
		hub:       events.NewHub(),
	}
}

func pairDevice(t *testing.T, s *Server, name string) {
	t.Helper()
	token, _, err := s.pairing.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	body := strings.NewReader(`{"token":"` + token + `","device_name":"` + name + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/pair", body)
	rec := httptest.NewRecorder()
	s.handlePair(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pairing %q: expected 200, got %d body=%s", name, rec.Code, rec.Body.String())
	}
}

// Verification item 2 (first half): the very first device ever
// paired must automatically become primary.
func TestHandlePair_FirstDeviceBecomesPrimary(t *testing.T) {
	s := newTestServer(t)
	pairDevice(t, s, "First Phone")

	devices := s.store.Snapshot().Devices
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if !devices[0].IsPrimary {
		t.Fatal("first-ever paired device should be primary")
	}
}

// Verification item 2 (second half): everything paired after the
// first device defaults to guest, not primary.
func TestHandlePair_SecondDeviceDoesNotBecomePrimary(t *testing.T) {
	s := newTestServer(t)
	pairDevice(t, s, "First Phone")
	pairDevice(t, s, "Second Phone")

	devices := s.store.Snapshot().Devices
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(devices))
	}
	if !devices[0].IsPrimary {
		t.Fatal("first device should still be primary")
	}
	if devices[1].IsPrimary {
		t.Fatal("second device should default to guest (not primary)")
	}
}

// TestHandleMe_ReflectsOwnDeviceCorrectly proves a device can learn
// its own is_primary status via /v1/me without needing to separately
// store and cross-reference its own device ID against /v1/devices —
// this is the real fix for the phone app's original gap (it never
// stored its own device ID at pairing time).
func TestHandleMe_ReflectsOwnDeviceCorrectly(t *testing.T) {
	dir := t.TempDir()
	store, err := state.Open(dir)
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	primaryKey, err := auth.GenerateDeviceKey()
	if err != nil {
		t.Fatalf("GenerateDeviceKey: %v", err)
	}
	guestKey, err := auth.GenerateDeviceKey()
	if err != nil {
		t.Fatalf("GenerateDeviceKey: %v", err)
	}
	err = store.Update(func(st *state.State) error {
		st.Devices = append(st.Devices,
			state.Device{ID: "primary-1", Name: "Primary Phone", KeyHash: auth.HashDeviceKey(primaryKey), IsPrimary: true},
			state.Device{ID: "guest-1", Name: "Guest Phone", KeyHash: auth.HashDeviceKey(guestKey), IsPrimary: false},
		)
		return nil
	})
	if err != nil {
		t.Fatalf("seeding devices: %v", err)
	}

	s := &Server{store: store}
	handler := auth.RequireDevice(store, http.HandlerFunc(s.handleMe))

	check := func(key string, wantID string, wantPrimary bool) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		var got deviceResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		if got.ID != wantID {
			t.Fatalf("expected id %q, got %q", wantID, got.ID)
		}
		if got.IsPrimary != wantPrimary {
			t.Fatalf("expected is_primary=%v for %q, got %v", wantPrimary, wantID, got.IsPrimary)
		}
	}

	check(primaryKey, "primary-1", true)
	check(guestKey, "guest-1", false)
}

func TestDeviceTypeForRequestLoopbackIsShell(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/pair", nil)
	r.RemoteAddr = "127.0.0.1:54321"
	if got := deviceTypeForRequest(r); got != state.DeviceTypeShell {
		t.Fatalf("expected %q for a loopback request, got %q", state.DeviceTypeShell, got)
	}
}

func TestDeviceTypeForRequestRemoteIsPhone(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/pair", nil)
	r.RemoteAddr = "192.168.1.50:54321"
	if got := deviceTypeForRequest(r); got != state.DeviceTypePhone {
		t.Fatalf("expected %q for a remote request, got %q", state.DeviceTypePhone, got)
	}
}
