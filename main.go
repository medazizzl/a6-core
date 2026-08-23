package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"a6core/internal/apps"
	"a6core/internal/config"
	"a6core/internal/icons"
	"a6core/internal/modes"
	"a6core/internal/retro"
	"a6core/internal/server"
	"a6core/internal/shortcuts"
	"a6core/internal/state"
	"a6core/internal/system"
	"a6core/internal/version"
)

func main() {
	versionFlag := flag.Bool("version", false, "print version and exit")
	configPath := flag.String("config", "", "path to a JSON config file (optional; defaults are used if omitted)")
	flag.Parse()

	if *versionFlag {
		fmt.Println(version.String)
		return
	}

	log.Printf("A6 Core v%s starting...", version.String)

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("main: failed to load config: %v", err)
	}
	log.Printf("main: config loaded (env=%s, log_level=%s, data_dir=%s)", cfg.AppEnv, cfg.LogLevel, cfg.DataDir)

	store, err := state.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("main: failed to open state store: %v", err)
	}

	modeMgr := modes.NewManager(store, nil, nil)

	sleepSetter := func(force bool) error {
		_, err := modeMgr.Switch(modes.ModeSleep, force)
		return err
	}
	sysMgr := system.NewManager(version.String, nil, nil, sleepSetter)

	iconStore := icons.New(store)
	scStore := shortcuts.New(store, iconStore)
	retroStore := retro.New(cfg.DataDir)
	appMgr := apps.NewManager(scStore, retroStore, nil, nil)

	srv := server.New("7887", store, modeMgr, sysMgr, scStore, iconStore, appMgr, retroStore)

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
