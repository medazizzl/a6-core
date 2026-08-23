package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"a6core/internal/shortcuts"
)

type shortcutHandler struct {
	store *shortcuts.Store
}

func (h *shortcutHandler) handleList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.store.List())
}

type createShortcutRequest struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	IconID string `json:"icon_id,omitempty"`
}

func (h *shortcutHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createShortcutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_body", "malformed JSON body")
		return
	}
	sc, err := h.store.Create(req.Name, req.URL, req.IconID)
	if err != nil {
		writeShortcutError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sc)
}

type updateShortcutRequest struct {
	Name        *string `json:"name,omitempty"`
	URL         *string `json:"url,omitempty"`
	IconID      *string `json:"icon_id,omitempty"`
	RefreshIcon bool    `json:"refresh_icon,omitempty"`
}

func (h *shortcutHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req updateShortcutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_body", "malformed JSON body")
		return
	}
	sc, err := h.store.Update(id, shortcuts.UpdateInput{
		Name:        req.Name,
		URL:         req.URL,
		IconID:      req.IconID,
		RefreshIcon: req.RefreshIcon,
	})
	if err != nil {
		writeShortcutError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sc)
}

func (h *shortcutHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.Delete(id); err != nil {
		writeShortcutError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeShortcutError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, shortcuts.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "not_found", "shortcut not found")
	case errors.Is(err, shortcuts.ErrInvalidName):
		writeJSONError(w, http.StatusBadRequest, "invalid_name", err.Error())
	case errors.Is(err, shortcuts.ErrInvalidURL):
		writeJSONError(w, http.StatusBadRequest, "invalid_url", err.Error())
	case errors.Is(err, shortcuts.ErrIconNotFound):
		writeJSONError(w, http.StatusBadRequest, "icon_not_found", "icon_id does not reference an existing icon")
	default:
		writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
	}
}
