// Package system provides system action management for A6 Core.
// It wraps ActionExecutor implementations and provides HTTP handlers.
package system

import (
	"context"
	"encoding/json"
	"net/http"
)

// ActionExecutor defines the interface for system actions.
type ActionExecutor interface {
	Reboot(ctx context.Context) error
	PowerOff(ctx context.Context) error
}

// Manager manages system actions and provides HTTP handlers.
type Manager struct {
	executor ActionExecutor
}

// NewManager creates a new Manager with the given executor.
func NewManager(executor ActionExecutor) *Manager {
	return &Manager{executor: executor}
}

// Info returns system information.
func (m *Manager) Info() (map[string]any, error) {
	return map[string]any{
		"reboot_supported":   true,
		"poweroff_supported": true,
		"suspend_supported":  false,
	}, nil
}

// Reboot triggers a system reboot.
func (m *Manager) Reboot(force bool) error {
	ctx := context.Background()
	return m.executor.Reboot(ctx)
}

// PowerOff triggers a system shutdown.
func (m *Manager) PowerOff(force bool) error {
	ctx := context.Background()
	return m.executor.PowerOff(ctx)
}

// Handler returns the HTTP handlers for system routes.
type Handler struct {
	mgr *Manager
}

func NewHandler(mgr *Manager) *Handler {
	return &Handler{mgr: mgr}
}

func (h *Handler) handleInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	info, err := h.mgr.Info()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "info_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

type forceRequest struct {
	Force bool `json:"force,omitempty"`
}

func (h *Handler) handleReboot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req forceRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	err := h.mgr.Reboot(req.Force)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "reboot_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req forceRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	err := h.mgr.PowerOff(req.Force)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "shutdown_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   code,
		"message": message,
	})
}
