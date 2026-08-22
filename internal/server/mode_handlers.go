package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"a6core/internal/modes"
	"a6core/internal/resources"
	"a6core/internal/state"
)

type modeHandler struct {
	mgr *modes.Manager
}

type switchRequest struct {
	Target string `json:"target"`
	Force  bool   `json:"force"`
}

type modesGetResponse struct {
	Current       string   `json:"current"`
	Available     []string `json:"available"`
	Transitioning bool     `json:"transitioning"`
}

type modesPostResponse struct {
	TransitionID string `json:"transition_id"`
	From         string `json:"from"`
	To           string `json:"to"`
}

func (h *modeHandler) handleModes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleGet(w, r)
	case http.MethodPost:
		h.handlePost(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func availableModeStrings() []string {
	out := make([]string, 0, len(modes.AllModes))
	for _, m := range modes.AllModes {
		out = append(out, string(m))
	}
	return out
}

func (h *modeHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(modesGetResponse{
		Current:       string(h.mgr.Current()),
		Available:     availableModeStrings(),
		Transitioning: false,
	})
}

func (h *modeHandler) handlePost(w http.ResponseWriter, r *http.Request) {
	var req switchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	target := modes.Mode(req.Target)
	from, err := h.mgr.Switch(target, req.Force)
	if err != nil {
		if errors.Is(err, modes.ErrInvalidMode) {
			http.Error(w, "invalid mode", http.StatusUnprocessableEntity)
			return
		}

		var blocked *resources.ErrBlocked
		if errors.As(err, &blocked) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":    "busy_resource",
				"blocking": blocked.Blocking,
			})
			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(modesPostResponse{
		TransitionID: state.NewID(),
		From:         string(from),
		To:           string(target),
	})
}
