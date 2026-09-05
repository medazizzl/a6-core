package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"a6core/internal/state"
)

// newTestStoreWithDevices builds a real state.Store (temp-dir backed,
// same code path as production) with one primary and one guest
// device, returning their RAW keys so tests can send genuine
// Authorization headers through the real ValidateDeviceKey path --
// exactly what any real client, honest or malicious, would send. No
// shortcuts through fake/injected device structs for this one.
func newTestStoreWithDevices(t *testing.T) (store *state.Store, primaryKey, guestKey string) {
	t.Helper()
	dir := t.TempDir()
	s, err := state.Open(dir)
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}

	primaryKey, err = GenerateDeviceKey()
	if err != nil {
		t.Fatalf("GenerateDeviceKey (primary): %v", err)
	}
	guestKey, err = GenerateDeviceKey()
	if err != nil {
		t.Fatalf("GenerateDeviceKey (guest): %v", err)
	}

	err = s.Update(func(st *state.State) error {
		st.Devices = append(st.Devices,
			state.Device{
				ID:        "primary-1",
				Name:      "Primary Phone",
				KeyHash:   HashDeviceKey(primaryKey),
				PairedAt:  time.Now(),
				LastSeen:  time.Now(),
				IsPrimary: true,
			},
			state.Device{
				ID:        "guest-1",
				Name:      "Guest Phone",
				KeyHash:   HashDeviceKey(guestKey),
				PairedAt:  time.Now(),
				LastSeen:  time.Now(),
				IsPrimary: false,
			},
		)
		return nil
	})
	if err != nil {
		t.Fatalf("seeding devices: %v", err)
	}
	return s, primaryKey, guestKey
}

// gatedChain mirrors EXACTLY how server.go wires reboot/shutdown:
// RequireDevice(store, RequirePrimary(handler)). Testing this real
// composition -- not RequirePrimary in isolation -- is what actually
// proves the production route wiring behaves correctly, not just the
// middleware function on its own.
func gatedChain(store *state.Store, next http.Handler) http.Handler {
	return RequireDevice(store, RequirePrimary(next))
}

func acceptedHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
}

// Verification item 3: primary can perform a gated action.
func TestRequirePrimary_PrimaryDeviceAllowed(t *testing.T) {
	store, primaryKey, _ := newTestStoreWithDevices(t)
	handler := gatedChain(store, acceptedHandler())

	req := httptest.NewRequest(http.MethodPost, "/v1/system/reboot", nil)
	req.Header.Set("Authorization", "Bearer "+primaryKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("primary device: expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// Verification items 4 and 6 together: a guest sending a real, valid,
// non-primary device key straight at a gated endpoint -- exactly what
// a modified app or a raw curl/Postman request would do, with no app
// UI involved at all -- must be rejected server-side. This is the
// actual Stage 16 requirement; a hidden button in the app proves
// nothing on its own.
func TestRequirePrimary_GuestDeviceRejected(t *testing.T) {
	store, _, guestKey := newTestStoreWithDevices(t)
	handler := gatedChain(store, acceptedHandler())

	req := httptest.NewRequest(http.MethodPost, "/v1/system/shutdown", nil)
	req.Header.Set("Authorization", "Bearer "+guestKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("guest device: expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// Verification item 5: a guest must be unaffected on any route that
// was never gated with RequirePrimary in the first place -- e.g.
// /v1/modes in the real server.go. Stage 16 is additive; it must not
// accidentally restrict anything guests could already do.
func TestRequirePrimary_GuestStillAllowedOnUngatedRoute(t *testing.T) {
	store, _, guestKey := newTestStoreWithDevices(t)
	handler := RequireDevice(store, acceptedHandler()) // no RequirePrimary here

	req := httptest.NewRequest(http.MethodGet, "/v1/modes", nil)
	req.Header.Set("Authorization", "Bearer "+guestKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("guest on ungated route: expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// Defensive case: RequirePrimary is only ever meant to run behind
// RequireDevice, but if it were ever wired wrong (no device in
// context at all), it must fail closed, not panic or default-allow.
func TestRequirePrimary_NoDeviceInContextFailsClosed(t *testing.T) {
	handler := RequirePrimary(acceptedHandler())
	req := httptest.NewRequest(http.MethodPost, "/v1/system/reboot", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("no device in context: expected 403 fail-closed, got %d", rec.Code)
	}
}
