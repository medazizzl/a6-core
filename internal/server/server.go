package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"a6core/internal/auth"
	"a6core/internal/modes"
	"a6core/internal/state"
	"a6core/internal/system"
)

type Server struct {
	httpServer *http.Server
	store      *state.Store
	pairing    *auth.PairingManager
	rateLimit  *auth.RateLimiter
}

func New(port string, store *state.Store, modeMgr *modes.Manager, sysMgr *system.Manager) *Server {
	s := &Server{
		store:     store,
		pairing:   auth.NewPairingManager(),
		rateLimit: auth.NewRateLimiter(),
	}

	mux := http.NewServeMux()

	// --- Public: no authentication ---
	mux.HandleFunc("GET /healthz", s.handleHealth)

	// --- Pairing: bootstraps the first authenticated device ---
	mux.Handle("POST /v1/pair/generate", auth.RequireLocalhost(http.HandlerFunc(s.handlePairGenerate)))
	mux.HandleFunc("POST /v1/pair", s.handlePair)

	// --- Authenticated: every route below requires a valid device key ---
	mux.Handle("GET /v1/info", auth.RequireDevice(store, http.HandlerFunc(s.handleInfo)))
	mux.Handle("/v1/state", auth.RequireDevice(store, http.HandlerFunc(s.handleState)))
	mux.Handle("GET /v1/devices", auth.RequireDevice(store, http.HandlerFunc(s.handleDevicesList)))
	mux.Handle("DELETE /v1/devices/{id}", auth.RequireDevice(store, http.HandlerFunc(s.handleDeviceDelete)))

	mh := &modeHandler{mgr: modeMgr}
	mux.Handle("/v1/modes", auth.RequireDevice(store, http.HandlerFunc(mh.handleModes)))

	sh := &systemHandler{sysMgr: sysMgr}
	mux.Handle("/v1/system/info", auth.RequireDevice(store, http.HandlerFunc(sh.handleInfo)))
	mux.Handle("/v1/system/reboot", auth.RequireDevice(store, http.HandlerFunc(sh.handleReboot)))
	mux.Handle("/v1/system/shutdown", auth.RequireDevice(store, http.HandlerFunc(sh.handleShutdown)))
	mux.Handle("/v1/system/suspend", auth.RequireDevice(store, http.HandlerFunc(sh.handleSuspend)))

	s.httpServer = &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	return s
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

func (s *Server) Start() error {
	log.Printf("http: starting HTTP server on %s", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
