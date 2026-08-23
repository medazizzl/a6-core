package server

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"a6core/internal/icons"
)

type iconHandler struct {
	store *icons.Store
}

func (h *iconHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, icons.MaxIconSize)

	var data []byte
	var err error

	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		file, _, ferr := r.FormFile("icon")
		if ferr != nil {
			writeJSONError(w, http.StatusBadRequest, "missing_file", "missing 'icon' form field")
			return
		}
		defer file.Close()
		// io.ReadAll drains the stream fully until EOF or an error.
		// A single Read() call (as the previous, discarded version
		// did) is not guaranteed to return the whole body in one
		// call for a network-backed reader — that gap could have
		// silently truncated a legitimate multi-chunk upload.
		data, err = io.ReadAll(file)
	} else {
		data, err = io.ReadAll(r.Body)
	}

	if err != nil {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "too_large", "icon exceeds maximum size of 512KB")
		return
	}

	iconID, err := h.store.Save(data)
	if err != nil {
		switch {
		case errors.Is(err, icons.ErrTooLarge):
			writeJSONError(w, http.StatusRequestEntityTooLarge, "too_large", err.Error())
		case errors.Is(err, icons.ErrUnsupportedType):
			writeJSONError(w, http.StatusBadRequest, "unsupported_type", err.Error())
		default:
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
		}
		return
	}

	_, contentType, _ := h.store.Get(iconID)
	ext := ".png"
	if contentType == "image/jpeg" {
		ext = ".jpg"
	}
	// Matches the frozen spec's response shape exactly: {icon_id, filename}.
	writeJSON(w, http.StatusCreated, map[string]string{
		"icon_id":  iconID,
		"filename": iconID + ext,
	})
}

func (h *iconHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	data, contentType, err := h.store.Get(id)
	if err != nil {
		if errors.Is(err, icons.ErrNotFound) || errors.Is(err, icons.ErrInvalidID) {
			writeJSONError(w, http.StatusNotFound, "not_found", "icon not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "read_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleGetDefault serves the embedded fallback icon (icons.go,
// Wave 3). Registered as a separate literal route, GET
// /v1/icons/default — distinct from GET /v1/icons/{id}, which only
// ever accepts real generated icon UUIDs. No collision risk: the
// literal string "default" never matches that UUID pattern, and
// Go's ServeMux resolves a literal segment over a wildcard one
// regardless of registration order.
func (h *iconHandler) handleGetDefault(w http.ResponseWriter, r *http.Request) {
	data, contentType := icons.Default()
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
