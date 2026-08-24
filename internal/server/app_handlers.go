package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"a6core/internal/apps"
	"a6core/internal/events"
	"a6core/internal/retro"
	"a6core/internal/shortcuts"
)

type appHandler struct {
	mgr *apps.Manager
	hub *events.Hub
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

// launchEventData builds the app.launched event payload from a
// session. Shared by BOTH POST /v1/apps/launch (below) and POST
// /v1/retro/launch (retro_handlers.go) — extending Stage 11's "two
// doors, one room" principle from the launch call itself to what
// gets published about it, so the two entry points can't diverge.
func launchEventData(s apps.Session) map[string]string {
	data := map[string]string{"type": s.Type}
	if s.ShortcutID != "" {
		data["shortcut_id"] = s.ShortcutID
	}
	if s.Console != "" {
		data["console"] = s.Console
	}
	if s.GameID != "" {
		data["game_id"] = s.GameID
	}
	return data
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

	h.hub.Publish(events.Event{Event: "app.launched", Data: launchEventData(session)})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(launchResponse{AppSessionID: session.ID})
}

// handleClose captures the session BEFORE calling Close(), since
// Close() clears it. Close() is idempotent when nothing is running
// (Stage 11 decision 3a) — the before != nil guard ensures
// app.closed only fires for a session that genuinely existed, never
// a phantom event for an already-idle close.
func (h *appHandler) handleClose(w http.ResponseWriter, r *http.Request) {
	before := h.mgr.Current()
	if err := h.mgr.Close(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "close_failed", err.Error())
		return
	}
	if before != nil {
		h.hub.Publish(events.Event{Event: "app.closed", Data: map[string]string{"type": before.Type}})
	}
	w.WriteHeader(http.StatusAccepted)
}

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