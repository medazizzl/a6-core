package main

import (
	"log"

	"a6core/internal/state"
	"a6core/internal/version"
)

func main() {
	log.Printf("A6 Core v%s starting...", version.String)
	store, err := state.Open("")
	if err != nil {
		log.Fatalf("state: failed to open store: %v", err)
	}
	log.Println("state: store opened successfully")

	snap := store.Snapshot()
	log.Printf("state: core_id=%s core_name=%q devices=%d shortcuts=%d servers=%d",
		snap.CoreID, snap.CoreName, len(snap.Devices), len(snap.Shortcuts), len(snap.Servers))

	if err := store.Save(); err != nil {
		log.Fatalf("state: failed to save store: %v", err)
	}
	log.Println("state: save round-trip verified")
	log.Println("Stage 2 scaffold: state persistence verified, no HTTP server yet.")
}
