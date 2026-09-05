package server

import (
	"errors"
	"net/http"
	"time"

	"a6core/internal/events"
	"a6core/internal/state"
)

var errLastDevice = errors.New("cannot remove the last remaining paired device")

type deviceResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	DeviceType string `json:"device_type"`
	IsPrimary  bool   `json:"is_primary"`
	PairedAt   string `json:"paired_at"`
	LastSeen   string `json:"last_seen"`
}

func (s *Server) handleDevicesList(w http.ResponseWriter, r *http.Request) {
	snap := s.store.Snapshot()
	out := make([]deviceResponse, 0, len(snap.Devices))
	for _, d := range snap.Devices {
		out = append(out, deviceResponse{
			ID:         d.ID,
			Name:       d.Name,
			DeviceType: d.DeviceType,
			IsPrimary:  d.IsPrimary,
			PairedAt:   d.PairedAt.Format(time.RFC3339),
			LastSeen:   d.LastSeen.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDevicePromote grants primary status to another device. It is
// itself gated to primary-only callers (see server.go route wiring),
// and is deliberately ADDITIVE ONLY for Stage 16 — it never removes
// primary status from anyone, including the caller. A demotion
// mechanism is an explicit future design pass (same shape as the
// original Stage 7 "last device" lockout problem: removing the wrong
// primary with no one left to undo it is a real way to brick the
// appliance), not something to improvise here.
func (s *Server) handleDevicePromote(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_id", "device id is required")
		return
	}

	var existed bool
	var promotedName string
	err := s.store.Update(func(st *state.State) error {
		for i := range st.Devices {
			if st.Devices[i].ID == id {
				existed = true
				st.Devices[i].IsPrimary = true
				promotedName = st.Devices[i].Name
				return nil
			}
		}
		return nil
	})

	switch {
	case err != nil:
		writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
	case !existed:
		writeJSONError(w, http.StatusNotFound, "device_not_found", "no device with that id")
	default:
		s.hub.Publish(events.Event{
			Event: "device.promoted",
			Data:  map[string]string{"device_id": id, "device_name": promotedName},
		})
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleDeviceDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_id", "device id is required")
		return
	}

	// Captured inside the closure before removal, so device.revoked
	// can report a name — the entry is gone from st.Devices by the
	// time Update returns successfully.
	var revokedName string

	var existed bool
	err := s.store.Update(func(st *state.State) error {
		idx := -1
		for i, d := range st.Devices {
			if d.ID == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			return nil
		}
		existed = true
		if len(st.Devices) <= 1 {
			return errLastDevice
		}
		revokedName = st.Devices[idx].Name
		st.Devices = append(st.Devices[:idx], st.Devices[idx+1:]...)
		return nil
	})

	switch {
	case errors.Is(err, errLastDevice):
		writeJSONError(w, http.StatusConflict, "last_device", "cannot revoke the last remaining paired device")
	case err != nil:
		writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
	case !existed:
		writeJSONError(w, http.StatusNotFound, "device_not_found", "no device with that id")
	default:
		s.hub.Publish(events.Event{
			Event: "device.revoked",
			Data:  map[string]string{"device_id": id, "device_name": revokedName},
		})
		w.WriteHeader(http.StatusNoContent)
	}
}
