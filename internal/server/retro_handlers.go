package server

import (
	"encoding/json"
	"net/http"

	"a6core/internal/apps"
	"a6core/internal/retro"
)

type retroHandler struct {
	appMgr *apps.Manager
	store  *retro.Store
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
		writeLaunchError(w, err) // reuses the shared mapping: unknown console -> 422, per clarification C
		return
	}
	writeJSON(w, http.StatusOK, games)
}

// handleLaunch is the second door onto apps.Manager.Launch — same
// call, same writeLaunchError mapping as /v1/apps/launch. This
// handler only accepts console/game_id and hardcodes TypeRetro,
// rather than exposing the general launch shape a second time.
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(launchResponse{AppSessionID: session.ID})
}
