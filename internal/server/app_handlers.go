package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"a6core/internal/apps"
	"a6core/internal/retro"
	"a6core/internal/shortcuts"
)

type appHandler struct {
	mgr *apps.Manager
}

type currentAppResponse struct {
	Type       *string `json:"type"`
	ShortcutID string  `json:"shortcut_id,omitempty"`
	Console    string  `json:"console,omitempty"`
	GameID     string  `json:"game_id,omitempty"`
	StartedAt  *string `json:"started_at,omitempty"`
}

func sessionToResponse(s *apps.Session) currentAppResponse {
	if s == nil {
		return currentAppResponse{Type: nil}
	}
	t := s.Type
	started := s.StartedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	return currentAppResponse{
		Type:       &t,
		ShortcutID: s.ShortcutID,
		Console:    s.Console,
		GameID:     s.GameID,
		StartedAt:  &started,
	}
}

func (h *appHandler) handleCurrent(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, sessionToResponse(h.mgr.Current()))
}

type launchAppRequest struct {
	Type       string `json:"type"`
	ShortcutID string `json:"shortcut_id,omitempty"`
	Console    string `json:"console,omitempty"`
	GameID     string `json:"game_id,omitempty"`
}

type launchResponse struct {
	AppSessionID string `json:"app_session_id"`
}

func (h *appHandler) handleLaunch(w http.ResponseWriter, r *http.Request) {
	var req launchAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_body", "malformed JSON body")
		return
	}
	session, err := h.mgr.Launch(apps.LaunchInput{
		Type:       req.Type,
		ShortcutID: req.ShortcutID,
		Console:    req.Console,
		GameID:     req.GameID,
	})
	if err != nil {
		writeLaunchError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(launchResponse{AppSessionID: session.ID})
}

func (h *appHandler) handleClose(w http.ResponseWriter, r *http.Request) {
	if err := h.mgr.Close(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "close_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// writeLaunchError is the SINGLE error-mapping function used by both
// /v1/apps/launch and /v1/retro/launch (retro_handlers.go below) —
// two doors, one room, same furniture, per Stage 11 decision 1.
func writeLaunchError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, retro.ErrUnknownConsole):
		writeJSONError(w, http.StatusUnprocessableEntity, "unknown_console", err.Error())
	case errors.Is(err, apps.ErrInvalidType),
		errors.Is(err, apps.ErrMissingShortcutID),
		errors.Is(err, apps.ErrMissingConsole),
		errors.Is(err, apps.ErrMissingGameID):
		writeJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, shortcuts.ErrNotFound):
		writeJSONError(w, http.StatusBadRequest, "shortcut_not_found", "shortcut_id does not reference an existing shortcut")
	case errors.Is(err, apps.ErrGameNotFound):
		writeJSONError(w, http.StatusBadRequest, "game_not_found", "game_id does not exist for the given console")
	default:
		writeJSONError(w, http.StatusInternalServerError, "launch_failed", err.Error())
	}
}
