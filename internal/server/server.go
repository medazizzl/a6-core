package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"a6core/internal/auth"
	"a6core/internal/config"
	"a6core/internal/state"
)

type Server struct {
	cfg       *config.Config
	store     *state.Store
	mux       *http.ServeMux
	httpSrv   *http.Server
	pairing   *auth.PairingManager
	rateLimit *auth.RateLimiter
}

func New(cfg *config.Config, store *state.Store) *Server {
	s := &Server{
		cfg:       cfg,
		store:     store,
		mux:       http.NewServeMux(),
		pairing:   auth.NewPairingManager(),
		rateLimit: auth.NewRateLimiter(),
	}
	s.routes()
	return s
}

// routes registers every endpoint in three explicit groups, matching
// the frozen spec's security model (§5, §15): public, pairing, and
// authenticated. Keeping these visually separate is deliberate — it
// makes it structurally obvious that /v1/pair is never wrapped in
// device auth (a phone has no key yet at that point), while
// everything else always is.
func (s *Server) routes() {
	// --- Public: no authentication ---
	s.mux.HandleFunc("GET /healthz", s.handleHealth)

	// --- Pairing: bootstraps the first authenticated device ---
	s.mux.Handle("POST /v1/pair/generate", auth.RequireLocalhost(http.HandlerFunc(s.handlePairGenerate)))
	s.mux.HandleFunc("POST /v1/pair", s.handlePair)

	// --- Authenticated: every route below requires a valid device key ---
	s.mux.Handle("GET /v1/info", auth.RequireDevice(s.store, http.HandlerFunc(s.handleInfo)))
	s.mux.Handle("/v1/state", auth.RequireDevice(s.store, http.HandlerFunc(s.handleState)))
	s.mux.Handle("GET /v1/devices", auth.RequireDevice(s.store, http.HandlerFunc(s.handleDevicesList)))
	s.mux.Handle("DELETE /v1/devices/{id}", auth.RequireDevice(s.store, http.HandlerFunc(s.handleDeviceDelete)))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	snap := s.store.Snapshot()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"core_id":   snap.CoreID,
		"core_name": snap.CoreName,
		"devices":   len(snap.Devices),
		"shortcuts": len(snap.Shortcuts),
		"servers":   len(snap.Servers),
	})
}

func (s *Server) ListenAndServe(addr string) error {
	s.httpSrv = &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}
	return s.httpSrv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpSrv == nil {
		return nil
	}
	return s.httpSrv.Shutdown(ctx)
}
