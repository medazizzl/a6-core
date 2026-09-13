package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"a6core/internal/apps"
	"a6core/internal/cloud"
	"a6core/internal/events"
	"a6core/internal/icons"
	"a6core/internal/input"
	"a6core/internal/modes"
	"a6core/internal/retro"
	"a6core/internal/servers"
	"a6core/internal/shortcuts"
	"a6core/internal/state"
	"a6core/internal/system"
)

// fakeSystemExecutor never actually reboots or powers off anything —
// it just records that it was called. This is what makes it safe to
// test the REAL registered /v1/system/reboot route (not just
// RequirePrimary in isolation) without ever triggering a real reboot,
// which is exactly the gap a live curl test couldn't safely cover.
type fakeSystemExecutor struct {
	rebootCalled   bool
	powerOffCalled bool
}

func (f *fakeSystemExecutor) Reboot(ctx context.Context) error {
	f.rebootCalled = true
	return nil
}

func (f *fakeSystemExecutor) PowerOff(ctx context.Context) error {
	f.powerOffCalled = true
	return nil
}

// newFullTestServer builds a real *Server with every real dependency
// wired the same way main.go does, differing only in using safe fakes
// where a real one would touch actual hardware (system reboot/poweroff,
// Minecraft's executor). This is what lets a test hit the ACTUAL
// registered mux, proving the real route wiring, not a hand-built
// stand-in for it.
func newFullTestServer(t *testing.T) (*Server, *fakeSystemExecutor) {
	t.Helper()
	dir := t.TempDir()
	store, err := state.Open(dir)
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}

	modeMgr := modes.NewManager(store, nil, nil)
	fakeExec := &fakeSystemExecutor{}
	sysMgr := system.NewManager(fakeExec)
	iconStore := icons.New(store)
	scStore := shortcuts.New(store, iconStore)
	retroStore := retro.New(t.TempDir())
	appMgr := apps.NewManager(scStore, retroStore, nil, nil)
	srvMgr := servers.NewManager(servers.Executor{}, 0, 0) // falls back to NoOpExecutor
	hub := events.NewHub()
	inputMgr := input.NewManager(nil)
	cloudStore := cloud.New(store, t.TempDir())

	srv := New("0", store, modeMgr, sysMgr, scStore, iconStore, appMgr, retroStore, srvMgr, hub, inputMgr, cloudStore)
	return srv, fakeExec
}

// pairRealDevice pairs a real device through the real /v1/pair
// handler (not a hand-inserted state.Device) and returns its raw
// key — so tests exercise the exact same path a real phone would.
func pairRealDevice(t *testing.T, srv *Server, name string) string {
	t.Helper()
	token, _, err := srv.pairing.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	body := `{"token":"` + token + `","device_name":"` + name + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/pair", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handlePair(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pairing %q: expected 200, got %d body=%s", name, rec.Code, rec.Body.String())
	}
	var resp struct {
		DeviceKey string `json:"device_key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding pair response: %v", err)
	}
	return resp.DeviceKey
}

// TestRealRoute_GuestCanReboot proves the ACTUAL registered route
// allows a guest device through -- the real product decision this
// stage made (Reboot open to everyone, Shutdown primary-only). Safe
// to run automatically because fakeSystemExecutor never really
// reboots anything.
func TestRealRoute_GuestCanReboot(t *testing.T) {
	srv, fakeExec := newFullTestServer(t)

	primaryKey := pairRealDevice(t, srv, "First Phone (becomes primary)")
	guestKey := pairRealDevice(t, srv, "Second Phone (guest)")
	_ = primaryKey

	req := httptest.NewRequest(http.MethodPost, "/v1/system/reboot", nil)
	req.Header.Set("Authorization", "Bearer "+guestKey)
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted && rec.Code != http.StatusOK {
		t.Fatalf("guest reboot: expected success, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !fakeExec.rebootCalled {
		t.Fatal("expected the real Reboot handler to have called the executor, it didn't")
	}
}

// TestRealRoute_GuestCannotShutdown proves the real route still
// rejects a guest for Shutdown -- the half of this change that must
// NOT have moved.
func TestRealRoute_GuestCannotShutdown(t *testing.T) {
	srv, fakeExec := newFullTestServer(t)

	pairRealDevice(t, srv, "First Phone (becomes primary)")
	guestKey := pairRealDevice(t, srv, "Second Phone (guest)")

	req := httptest.NewRequest(http.MethodPost, "/v1/system/shutdown", nil)
	req.Header.Set("Authorization", "Bearer "+guestKey)
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("guest shutdown: expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	if fakeExec.powerOffCalled {
		t.Fatal("guest shutdown must never reach the executor -- it did")
	}
}

// TestRealRoute_PrimaryCanShutdown is the positive-path complement --
// proving the primary-only gate still lets the actual primary through.
func TestRealRoute_PrimaryCanShutdown(t *testing.T) {
	srv, fakeExec := newFullTestServer(t)

	primaryKey := pairRealDevice(t, srv, "First Phone (becomes primary)")

	req := httptest.NewRequest(http.MethodPost, "/v1/system/shutdown", nil)
	req.Header.Set("Authorization", "Bearer "+primaryKey)
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted && rec.Code != http.StatusOK {
		t.Fatalf("primary shutdown: expected success, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !fakeExec.powerOffCalled {
		t.Fatal("expected the real PowerOff handler to have called the executor, it didn't")
	}
}
