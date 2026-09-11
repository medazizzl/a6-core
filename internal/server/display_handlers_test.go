package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRealRoute_GuestCanRequestDisplaySleep proves the real
// registered route accepts a guest device -- this is deliberately
// NOT primary-gated, unlike /v1/system/shutdown, since it's the
// guest-facing half of the Power Off intent (see DECISIONS.md).
func TestRealRoute_GuestCanRequestDisplaySleep(t *testing.T) {
	srv, _ := newFullTestServer(t)

	pairRealDevice(t, srv, "First Phone (becomes primary)")
	guestKey := pairRealDevice(t, srv, "Second Phone (guest)")

	req := httptest.NewRequest(http.MethodPost, "/v1/display/sleep", nil)
	req.Header.Set("Authorization", "Bearer "+guestKey)
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("guest display/sleep: expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}
}
