package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"a6core/internal/servers"
)

type serverHandler struct {
	mgr *servers.Manager
}

func (h *serverHandler) handleList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.mgr.List())
}

func (h *serverHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	srv, err := h.mgr.Get(id)
	if err != nil {
		writeServerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, srv)
}

func (h *serverHandler) handleStart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, err := h.mgr.Start(id)
	if err != nil {
		writeServerError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

type stopRequest struct {
	Force bool `json:"force,omitempty"`
}

// handleStop: force here means specifically "re-trigger the sequence
// even if one is already in flight" (skip ErrStopInProgress) — NOT
// "skip the graceful sequence and kill immediately". The broadcast/
// grace/poll/escalate steps run identically regardless of force, per
// Stop()'s own contract from wave 2. This is the endpoint FOR the
// resource, not a caller checking whether the resource blocks
// something else — so force's meaning is intentionally narrower here
// than on /v1/modes or /v1/system/*.
func (h *serverHandler) handleStop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req stopRequest
	// Body is optional; a missing/empty body just means force=false,
	// matching how /v1/system/* already treats an absent body.
	_ = json.NewDecoder(r.Body).Decode(&req)

	err := h.mgr.Stop(id)
	if errors.Is(err, servers.ErrStopInProgress) && req.Force {
		err = h.mgr.ForceRestartStopSequence(id)
	}
	if err != nil {
		writeServerError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func writeServerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, servers.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "server_not_found", "no server with that id")
	case errors.Is(err, servers.ErrAlreadyRunning):
		writeJSONError(w, http.StatusConflict, "already_running", err.Error())
	case errors.Is(err, servers.ErrNotRunning):
		writeJSONError(w, http.StatusConflict, "not_running", err.Error())
	case errors.Is(err, servers.ErrStopInProgress):
		writeJSONError(w, http.StatusConflict, "stop_in_progress", "a stop is already in progress; retry with force to restart the sequence")
	default:
		writeJSONError(w, http.StatusInternalServerError, "server_action_failed", err.Error())
	}
}