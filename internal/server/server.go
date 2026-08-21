package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"a6core/internal/config"
	"a6core/internal/state"
)

type Server struct {
	cfg     *config.Config
	store   *state.Store
	mux     *http.ServeMux
	httpSrv *http.Server
}

func New(cfg *config.Config, store *state.Store) *Server {
	s := &Server{
		cfg:   cfg,
		store: store,
		mux:   http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("/healthz", s.handleHealth)
	s.mux.HandleFunc("/api/v1/info", s.handleInfo)
	s.mux.HandleFunc("/api/v1/state", s.handleState)
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
