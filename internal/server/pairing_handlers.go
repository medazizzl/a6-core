package server

import (
	"encoding/json"
	"net/http"
	"time"

	"a6core/internal/auth"
	"a6core/internal/state"
)

const apiVersion = "1.0"

type pairGenerateResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// handlePairGenerate creates a new pairing token. Registered behind
// auth.RequireLocalhost in routes() — only the TV shell, running on
// this same machine, can ever reach it (spec §5).
func (s *Server) handlePairGenerate(w http.ResponseWriter, r *http.Request) {
	token, expiresAt, err := s.pairing.Generate()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "pairing_generate_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pairGenerateResponse{Token: token, ExpiresAt: expiresAt})
}

type pairRequest struct {
	Token      string `json:"token"`
	DeviceName string `json:"device_name"`
}

type pairResponse struct {
	DeviceID   string `json:"device_id"`
	DeviceKey  string `json:"device_key"`
	CoreName   string `json:"core_name"`
	APIVersion string `json:"api_version"`
}

// handlePair exchanges a valid, unexpired pairing token for a
// permanent device key. This is the ONLY point in the whole API
// where a raw device key is ever transmitted (spec §15) — never
// stored, logged, or retrievable again after this response.
func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	if !s.rateLimit.Allow(auth.ClientIP(r)) {
		writeJSONError(w, http.StatusTooManyRequests, "rate_limited", "too many pairing attempts, try again shortly")
		return
	}

	var req pairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_body", "malformed JSON body")
		return
	}
	if req.Token == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_token", "token is required")
		return
	}
	if !s.pairing.Redeem(req.Token) {
		writeJSONError(w, http.StatusUnauthorized, "invalid_token", "pairing token invalid, expired, or already used")
		return
	}

	deviceName := req.DeviceName
	if deviceName == "" {
		deviceName = "Unnamed device"
	}

	rawKey, err := auth.GenerateDeviceKey()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "key_generation_failed", err.Error())
		return
	}

	device := state.Device{
		ID:       state.NewID(),
		Name:     deviceName,
		KeyHash:  auth.HashDeviceKey(rawKey),
		PairedAt: time.Now(),
		LastSeen: time.Now(),
	}

	var coreName string
	err = s.store.Update(func(st *state.State) error {
		st.Devices = append(st.Devices, device)
		coreName = st.CoreName
		return nil
	})
	if err != nil {
		// Deliberately do NOT return the raw key if persistence
		// failed — never tell a phone "here's your key" for a
		// device that wasn't actually saved.
		writeJSONError(w, http.StatusInternalServerError, "save_failed", "device could not be persisted, please retry pairing")
		return
	}

	writeJSON(w, http.StatusOK, pairResponse{
		DeviceID:   device.ID,
		DeviceKey:  rawKey,
		CoreName:   coreName,
		APIVersion: apiVersion,
	})
}
