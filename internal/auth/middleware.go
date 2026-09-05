// Package auth implements authentication and authorization for A6 Core.
package auth

import (
	"context"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"a6core/internal/state"
)

type contextKey string

const deviceContextKey contextKey = "a6core_device"

func DeviceFromContext(ctx context.Context) (state.Device, bool) {
	d, ok := ctx.Value(deviceContextKey).(state.Device)
	return d, ok
}

const lastSeenThrottle = 60 * time.Second

func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ValidateDeviceKey checks key against every paired device and, on a
// match, touches last_seen (throttled, same as before). This is now
// the SINGLE implementation of device-key validation, shared by the
// REST middleware below and the WebSocket handshake (internal/events,
// Stage 13) — two authentication entry points, one implementation,
// so they can never silently drift apart.
func ValidateDeviceKey(store *state.Store, key string) (state.Device, bool) {
	snap := store.Snapshot()
	var matched *state.Device
	for i := range snap.Devices {
		if KeysMatch(key, snap.Devices[i].KeyHash) {
			matched = &snap.Devices[i]
			break
		}
	}
	if matched == nil {
		return state.Device{}, false
	}

	if time.Since(matched.LastSeen) > lastSeenThrottle {
		matchedID := matched.ID
		if err := store.Update(func(s *state.State) error {
			for i := range s.Devices {
				if s.Devices[i].ID == matchedID {
					s.Devices[i].LastSeen = time.Now()
				}
			}
			return nil
		}); err != nil {
			log.Printf("auth: WARNING failed to update last_seen for device %s: %v", matchedID, err)
		}
	}
	return *matched, true
}

// ExtractDeviceKey pulls a device key from a request using the same
// precedence the frozen spec describes for WebSocket auth (§7):
// Authorization: Bearer header preferred, ?token= query parameter as
// a fallback for clients that can't set headers on a WebSocket
// handshake. Shared by both /v1/events and /v1/input (Stage 17) so
// the extraction logic can never drift between the two — the same
// reasoning that already put ValidateDeviceKey here in Stage 13.
func ExtractDeviceKey(r *http.Request) string {
	const prefix = "Bearer "
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, prefix) {
		return strings.TrimPrefix(h, prefix)
	}
	return r.URL.Query().Get("token")
}

func RequireDevice(store *state.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(authHeader, prefix) {
			writeAuthError(w, http.StatusUnauthorized, "missing_token", "Authorization: Bearer <device_key> header required")
			return
		}
		key := strings.TrimPrefix(authHeader, prefix)

		device, ok := ValidateDeviceKey(store, key)
		if !ok {
			writeAuthError(w, http.StatusUnauthorized, "invalid_token", "device key not recognized or revoked")
			return
		}

		ctx := context.WithValue(r.Context(), deviceContextKey, device)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequireLocalhost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := net.ParseIP(ClientIP(r))
		if ip == nil || !ip.IsLoopback() {
			writeAuthError(w, http.StatusForbidden, "not_local", "this endpoint is only reachable from the device itself")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeAuthError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + code + `","message":"` + message + `"}`))
}