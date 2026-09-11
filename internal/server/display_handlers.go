package server

import (
	"net/http"

	"a6core/internal/events"
)

// handleDisplaySleep is the real backend half of the guest-facing
// "Power Off" intent (see DECISIONS.md). It does exactly one thing:
// publish an event. It never touches hardware, never calls dbusctl,
// and is deliberately open to ANY paired device including guests —
// unlike /v1/system/shutdown, which stays primary-only and does a
// real shutdown.
//
// This exists so the contract is real and stable now, without
// building anything that currently does nothing visible: no phone-app
// UI calls this endpoint yet (deliberately not exposed — see
// DECISIONS.md), and nothing currently subscribes to this event
// either, since the real display-blanking behavior belongs to the TV
// shell (per the original Stage 17 design: HDMI auto-blanking is
// shell-owned), which doesn't exist yet. Once it does, it starts
// listening for this event and this endpoint needs no further
// changes at all.
func (s *Server) handleDisplaySleep(w http.ResponseWriter, r *http.Request) {
	s.hub.Publish(events.Event{
		Event: "display.sleep_requested",
		Data:  map[string]string{},
	})
	w.WriteHeader(http.StatusAccepted)
}
