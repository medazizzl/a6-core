package main

import (
	"log"
	"os"

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

	// Delegate system suspend to switching mode to "sleep"
	sleepSetter := func(force bool) error {
		_, err := modeMgr.Switch(modes.ModeSleep, force)
		return err
	}

	sysMgr := system.NewManager(version.String, nil, nil, sleepSetter)

	srv := server.New("7887", store, modeMgr, sysMgr)

	if err := srv.Start(); err != nil {
		log.Fatalf("main: server failed: %v", err)
	}
}
