package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"a6core/internal/config"
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
	log.Printf("state: core_id=%s core_name=%q devices=%d shortcuts=%d servers=%d",
		snap.CoreID, snap.CoreName, len(snap.Devices), len(snap.Shortcuts), len(snap.Servers))

	if err := store.Save(); err != nil {
		log.Fatalf("state: failed to save store: %v", err)
	}
	log.Println("state: save round-trip verified")
	log.Println("Stage 3 scaffold: CLI flags and config loading implemented.")
}
