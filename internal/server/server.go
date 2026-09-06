package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"a6core/internal/apps"
	"a6core/internal/auth"
	"a6core/internal/events"
	"a6core/internal/icons"
	"a6core/internal/input"
	"a6core/internal/modes"
	"a6core/internal/retro"
	"a6core/internal/servers"
	"a6core/internal/shortcuts"
	"a6core/internal/state"
	"a6core/internal/system"
)

type Server struct {
	httpServer *http.Server
	store      *state.Store
	pairing    *auth.PairingManager
	rateLimit  *auth.RateLimiter
	hub        *events.Hub
}

func New(port string, store *state.Store, modeMgr *modes.Manager, sysMgr *system.Manager, scStore *shortcuts.Store, iconStore *icons.Store, appMgr *apps.Manager, retroStore *retro.Store, srvMgr *servers.Manager, hub *events.Hub, inputMgr *input.Manager) *Server {
	s := &Server{
		store:     store,
		pairing:   auth.NewPairingManager(),
		rateLimit: auth.NewRateLimiter(),
		hub:       hub,
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
	mux.Handle("GET /v1/me", auth.RequireDevice(store, http.HandlerFunc(s.handleMe)))
	mux.Handle("DELETE /v1/devices/{id}", auth.RequireDevice(store, http.HandlerFunc(s.handleDeviceDelete)))
	mux.Handle("POST /v1/devices/{id}/promote", auth.RequireDevice(store, auth.RequirePrimary(http.HandlerFunc(s.handleDevicePromote))))

	mh := &modeHandler{mgr: modeMgr, hub: hub}
	mux.Handle("/v1/modes", auth.RequireDevice(store, http.HandlerFunc(mh.handleModes)))

	sh := &systemHandler{sysMgr: sysMgr}
	mux.Handle("GET /v1/system/info", auth.RequireDevice(store, http.HandlerFunc(sh.handleInfo)))
	mux.Handle("/v1/system/reboot", auth.RequireDevice(store, auth.RequirePrimary(http.HandlerFunc(sh.handleReboot))))
	mux.Handle("/v1/system/shutdown", auth.RequireDevice(store, auth.RequirePrimary(http.HandlerFunc(sh.handleShutdown))))

	scH := &shortcutHandler{store: scStore}
	mux.Handle("GET /v1/shortcuts", auth.RequireDevice(store, http.HandlerFunc(scH.handleList)))
	mux.Handle("POST /v1/shortcuts", auth.RequireDevice(store, http.HandlerFunc(scH.handleCreate)))
	mux.Handle("PUT /v1/shortcuts/{id}", auth.RequireDevice(store, http.HandlerFunc(scH.handleUpdate)))
	mux.Handle("DELETE /v1/shortcuts/{id}", auth.RequireDevice(store, http.HandlerFunc(scH.handleDelete)))

	icH := &iconHandler{store: iconStore}
	mux.Handle("POST /v1/icons", auth.RequireDevice(store, http.HandlerFunc(icH.handleUpload)))
	mux.Handle("GET /v1/icons/default", auth.RequireDevice(store, http.HandlerFunc(icH.handleGetDefault)))
	mux.Handle("GET /v1/icons/{id}", auth.RequireDevice(store, http.HandlerFunc(icH.handleGet)))

	appH := &appHandler{mgr: appMgr, hub: hub}
	mux.Handle("GET /v1/apps/current", auth.RequireDevice(store, http.HandlerFunc(appH.handleCurrent)))
	mux.Handle("POST /v1/apps/launch", auth.RequireDevice(store, http.HandlerFunc(appH.handleLaunch)))
	mux.Handle("POST /v1/apps/close", auth.RequireDevice(store, http.HandlerFunc(appH.handleClose)))

	rtH := &retroHandler{appMgr: appMgr, store: retroStore, hub: hub}
	mux.Handle("GET /v1/retro/consoles", auth.RequireDevice(store, http.HandlerFunc(rtH.handleConsoles)))
	mux.Handle("GET /v1/retro/games", auth.RequireDevice(store, http.HandlerFunc(rtH.handleGames)))
	mux.Handle("POST /v1/retro/launch", auth.RequireDevice(store, http.HandlerFunc(rtH.handleLaunch)))

	srvH := &serverHandler{mgr: srvMgr}
	mux.Handle("GET /v1/servers", auth.RequireDevice(store, http.HandlerFunc(srvH.handleList)))
	mux.Handle("GET /v1/servers/{id}", auth.RequireDevice(store, http.HandlerFunc(srvH.handleGet)))
	mux.Handle("POST /v1/servers/{id}/start", auth.RequireDevice(store, http.HandlerFunc(srvH.handleStart)))
	mux.Handle("POST /v1/servers/{id}/stop", auth.RequireDevice(store, http.HandlerFunc(srvH.handleStop)))

	mux.Handle("GET /v1/events", events.Handler(hub, func(key string) (state.Device, bool) {
		return auth.ValidateDeviceKey(store, key)
	}))

	mux.Handle("GET /v1/input", input.Handler(inputMgr, func(key string) (state.Device, bool) {
		return auth.ValidateDeviceKey(store, key)
	}))

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
