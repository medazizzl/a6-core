package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"a6core/internal/auth"
	"a6core/internal/cloud"
)

type cloudHandler struct {
	cloud *cloud.Store
}

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *cloudHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_body", "expected JSON with username and password")
		return
	}
	user, err := h.cloud.Register(req.Username, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, cloud.ErrUsernameTaken):
			writeJSONError(w, http.StatusConflict, "username_taken", err.Error())
		default:
			writeJSONError(w, http.StatusBadRequest, "register_failed", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": user.ID, "username": user.Username})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *cloudHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_body", "expected JSON with username and password")
		return
	}
	token, user, err := h.cloud.Login(req.Username, req.Password)
	if err != nil {
		// Deliberately the same response whether the username doesn't
		// exist or the password is wrong -- Login() already collapses
		// these into one error for exactly this reason.
		writeJSONError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"session_token": token,
		"user_id":       user.ID,
		"username":      user.Username,
	})
}

func (h *cloudHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := auth.ExtractDeviceKey(r) // same Bearer-header extraction, reused -- it's generic, not device-specific despite the name
	if err := h.cloud.Logout(token); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "logout_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *cloudHandler) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.CloudUserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "no_user_context", "cloud user context missing")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": user.ID, "username": user.Username})
}

// maxUploadBytes caps a single upload at 500MB -- generous for real
// phone photos and short video clips, while still being a real,
// deliberate limit rather than accepting an unbounded body straight
// onto a 4GB-RAM machine's disk.
const maxUploadBytes = 500 << 20

func (h *cloudHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.CloudUserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "no_user_context", "cloud user context missing")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_upload", "expected multipart/form-data with a 'file' field, and under 500MB")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "missing_file", "expected a 'file' field in the multipart form")
		return
	}
	defer file.Close()

	// Real content-sniffing, never trusting the client-supplied
	// filename extension or Content-Type header -- either could
	// easily be wrong or deliberately misleading.
	sniff := make([]byte, 512)
	n, _ := io.ReadFull(file, sniff)
	mimeType := http.DetectContentType(sniff[:n])
	fullReader := io.MultiReader(bytes.NewReader(sniff[:n]), file)

	rec, err := h.cloud.SaveFile(user.ID, header.Filename, mimeType, fullReader)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rec)
}

func (h *cloudHandler) handleListFiles(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.CloudUserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "no_user_context", "cloud user context missing")
		return
	}
	category := r.URL.Query().Get("category")
	writeJSON(w, http.StatusOK, h.cloud.ListFiles(user.ID, category))
}

func (h *cloudHandler) handleGetFile(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.CloudUserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "no_user_context", "cloud user context missing")
		return
	}
	id := r.PathValue("id")
	rec, f, err := h.cloud.OpenFile(user.ID, id)
	if err != nil {
		writeCloudFileError(w, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", rec.MimeType)
	_, _ = io.Copy(w, f)
}

func (h *cloudHandler) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.CloudUserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "no_user_context", "cloud user context missing")
		return
	}
	id := r.PathValue("id")
	if err := h.cloud.DeleteFile(user.ID, id); err != nil {
		writeCloudFileError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeCloudFileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cloud.ErrFileNotFound):
		writeJSONError(w, http.StatusNotFound, "file_not_found", "no file with that id")
	case errors.Is(err, cloud.ErrNotOwner):
		// Deliberately the SAME response as not-found -- confirming a
		// file ID exists but belongs to someone else is itself a real
		// information leak (e.g. proving another user uploaded
		// something with that ID), not something to hint at.
		writeJSONError(w, http.StatusNotFound, "file_not_found", "no file with that id")
	default:
		writeJSONError(w, http.StatusInternalServerError, "file_error", err.Error())
	}
}
