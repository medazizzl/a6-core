package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMigratesMissingMode(t *testing.T) {
	dir := t.TempDir()
	// A state.json written before Stage 8 added Mode at all — this is
	// exactly the shape the real Acer's state.json was in prior to
	// this stage, missing the "mode" key entirely.
	preStage8 := `{
		"core_id": "test-core",
		"core_name": "Project A6",
		"devices": [],
		"shortcuts": [],
		"servers": [],
		"settings": {"port": 7887}
	}`
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(preStage8), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := store.Snapshot().Mode; got != "tv" {
		t.Fatalf("expected missing mode field to migrate to %q, got %q", "tv", got)
	}
}
