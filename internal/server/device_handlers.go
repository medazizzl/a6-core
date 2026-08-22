package server

import (
	"errors"
	"net/http"
	"time"

	"a6core/internal/state"
)

var errLastDevice = errors.New("cannot remove the last remaining paired device")

type deviceResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	PairedAt string `json:"paired_at"`
	LastSeen string `json:"last_seen"`
}

func (s *Server) handleDevicesList(w http.ResponseWriter, r *http.Request) {
	snap := s.store.Snapshot()
	out := make([]deviceResponse, 0, len(snap.Devices))
	for _, d := range snap.Devices {
		out = append(out, deviceResponse{
			ID:       d.ID,
			Name:     d.Name,
			PairedAt: d.PairedAt.Format(time.RFC3339),
			LastSeen: d.LastSeen.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDeviceDelete revokes a paired device. The "is this the last
// device" check happens INSIDE the store.Update closure, under the
// same lock as the mutation — checking it beforehand against a
// separate snapshot would leave a race where two concurrent deletes
// could both see "not the last one" and jointly empty the list.
func (s *Server) handleDeviceDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_id", "device id is required")
		return
	}

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
		w.WriteHeader(http.StatusNoContent)
	}
}
