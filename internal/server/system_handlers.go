package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"a6core/internal/resources"
	"a6core/internal/system"
)

type systemHandler struct {
	sysMgr *system.Manager
}

type forceRequest struct {
	Force bool `json:"force"`
}

func (h *systemHandler) handleInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	info, err := h.sysMgr.Info()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func (h *systemHandler) handleReboot(w http.ResponseWriter, r *http.Request) {
	h.handleAction(w, r, h.sysMgr.Reboot)
}

func (h *systemHandler) handleShutdown(w http.ResponseWriter, r *http.Request) {
	h.handleAction(w, r, h.sysMgr.PowerOff)
}

func (h *systemHandler) handleAction(w http.ResponseWriter, r *http.Request, actionFn func(bool) error) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req forceRequest
	// Body is optional; ignore decoding error if empty
	_ = json.NewDecoder(r.Body).Decode(&req)
	err := actionFn(req.Force)
	if err != nil {
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
	w.WriteHeader(http.StatusAccepted)
}
