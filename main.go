package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"a6core/internal/modes"
	"a6core/internal/server"
	"a6core/internal/state"
	"a6core/internal/system"
	"a6core/internal/version"
)

func main() {
	log.Printf("A6 Core v%s starting...", version.String)

	dataDir := os.Getenv("A6_DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}

	store, err := state.Open(dataDir)
	if err != nil {
		log.Fatalf("main: failed to open state store: %v", err)
	}

	modeMgr := modes.NewManager(store, nil, nil)

	// Delegate system suspend to switching mode to "sleep".
	sleepSetter := func(force bool) error {
		_, err := modeMgr.Switch(modes.ModeSleep, force)
		return err
	}

	sysMgr := system.NewManager(version.String, nil, nil, sleepSetter)

	srv := server.New("7887", store, modeMgr, sysMgr)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.Start()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		if err != nil {
			log.Fatalf("main: server failed: %v", err)
		}
	case sig := <-sigCh:
		log.Printf("main: received %s, shutting down gracefully...", sig)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("main: WARNING graceful shutdown did not complete cleanly: %v", err)
		}

		if err := store.Save(); err != nil {
			log.Fatalf("main: CRITICAL final state save failed: %v", err)
		}
		log.Println("main: final state saved, exiting cleanly")
	}
}
