package server

import (
	"encoding/json"
	"net/http"

	"a6core/internal/apps"
	"a6core/internal/events"
	"a6core/internal/retro"
)

type retroHandler struct {
	appMgr *apps.Manager
	store  *retro.Store
	hub    *events.Hub
}

func (h *retroHandler) handleConsoles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.store.Consoles())
}

func (h *retroHandler) handleGames(w http.ResponseWriter, r *http.Request) {
	console := r.URL.Query().Get("console")
	if console == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_console", "console query parameter is required")
		return
	}
	games, err := h.store.Games(console)
	if err != nil {
		writeLaunchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, games)
}

// handleLaunch is the second door onto apps.Manager.Launch — same
// call, same writeLaunchError mapping, and (this is the piece an
// earlier draft of this wave missed) the same app.launched publish
// via the shared launchEventData helper as POST /v1/apps/launch.
func (h *retroHandler) handleLaunch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Console string `json:"console"`
		GameID  string `json:"game_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_body", "malformed JSON body")
		return
	}
	session, err := h.appMgr.Launch(apps.LaunchInput{
		Type:    apps.TypeRetro,
		Console: req.Console,
		GameID:  req.GameID,
	})
	if err != nil {
		writeLaunchError(w, err)
		return
	}

	h.hub.Publish(events.Event{Event: "app.launched", Data: launchEventData(session)})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(launchResponse{AppSessionID: session.ID})
}