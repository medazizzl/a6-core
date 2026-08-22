package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"a6core/internal/modes"
	"a6core/internal/resources"
)

type modeHandler struct {
	mgr *modes.Manager
}

type switchRequest struct {
	Mode  string `json:"mode"`
	Force bool   `json:"force"`
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

func (h *modeHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	current := h.mgr.Current()
	resp := map[string]interface{}{
		"current":       current,
		"available":     modes.AllModes,
		"transitioning": false,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *modeHandler) handlePost(w http.ResponseWriter, r *http.Request) {
	var req switchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	target := modes.Mode(req.Mode)
	_, err := h.mgr.Switch(target, req.Force)
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

	h.handleGet(w, r)
}
