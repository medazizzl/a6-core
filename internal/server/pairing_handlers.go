package server

import (
	"encoding/json"
	"net/http"
	"time"

	"a6core/internal/auth"
	"a6core/internal/events"
	"a6core/internal/state"
)

const apiVersion = "1.0"

type pairGenerateResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

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

// deviceTypeForRequest derives whether a pairing request is coming
// from the shell itself (running locally, as Wave 3's self-pairing
// does) or from an external phone, using the same loopback check
// RequireLocalhost enforces elsewhere. This is intentionally never
// something the client can specify in the request body — device_type
// must be exactly as trustworthy as RequireLocalhost already is, or
// not set at all.
func deviceTypeForRequest(r *http.Request) string {
	if auth.IsLocalRequest(r) {
		return state.DeviceTypeShell
	}
	return state.DeviceTypePhone
}

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
		ID:         state.NewID(),
		Name:       deviceName,
		KeyHash:    auth.HashDeviceKey(rawKey),
		DeviceType: deviceTypeForRequest(r),
		PairedAt:   time.Now(),
		LastSeen:   time.Now(),
	}

	var coreName string
	err = s.store.Update(func(st *state.State) error {
		// The very first device ever paired automatically becomes
		// primary — there's no existing primary yet to grant that role,
		// and the appliance needs at least one device able to
		// reboot/shut it down from day one. Checked inside the locked
		// Update closure so two simultaneous first-pairings can't both
		// end up primary. Every device paired after this defaults to
		// guest; an existing primary has to explicitly promote them
		// (see handleDevicePromote) — additive only, no demotion yet.
		if len(st.Devices) == 0 {
			device.IsPrimary = true
		}
		st.Devices = append(st.Devices, device)
		coreName = st.CoreName
		return nil
	})
	if err != nil {
		// Deliberately no device.paired publish here either — same
		// reasoning as not returning the raw key on this path:
		// persistence failed, so nothing genuinely joined the
		// paired-device list.
		writeJSONError(w, http.StatusInternalServerError, "save_failed", "device could not be persisted, please retry pairing")
		return
	}

	s.hub.Publish(events.Event{
		Event: "device.paired",
		Data:  map[string]string{"device_id": device.ID, "device_name": device.Name},
	})

	writeJSON(w, http.StatusOK, pairResponse{
		DeviceID:   device.ID,
		DeviceKey:  rawKey,
		CoreName:   coreName,
		APIVersion: apiVersion,
	})
}
