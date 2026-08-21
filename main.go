package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"a6core/internal/config"
	"a6core/internal/server"
	"a6core/internal/state"
	"a6core/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "Print version information")
	configPath := flag.String("config", "", "Path to configuration file")
	flag.Parse()

	if *showVersion {
		fmt.Printf("A6 Core v%s\n", version.String)
		os.Exit(0)
	}

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("config: failed to load configuration: %v", err)
	}

	log.Printf("A6 Core v%s starting [%s]...", version.String, cfg.AppEnv)

	store, err := state.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("state: failed to open store: %v", err)
	}
	log.Printf("state: store opened successfully in %s", cfg.DataDir)

	snap := store.Snapshot()
	addr := fmt.Sprintf(":%d", snap.Settings.Port)

	srv := server.New(cfg, store)

	go func() {
		log.Printf("http: starting HTTP server on %s", addr)
		if err := srv.ListenAndServe(addr); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	sig := <-quit
	log.Printf("signal: received %v, initiating graceful shutdown", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("http: graceful shutdown failed: %v", err)
	} else {
		log.Printf("http: server stopped cleanly")
	}

	if err := store.Save(); err != nil {
		log.Printf("state: ERROR - failed to save state on shutdown: %v", err)
		os.Exit(1)
	}
	log.Printf("state: saved state to disk")

	log.Printf("A6 Core shutdown complete")
}
