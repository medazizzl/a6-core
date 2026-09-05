package state

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadMigratesMissingIsPrimary covers verification item 1: a
// state.json written before Stage 16 added IsPrimary has multiple
// devices and none marked primary at all. The earliest-paired device
// (by paired_at) must be auto-promoted so the appliance never ends up
// with paired devices but no primary able to manage it.
func TestLoadMigratesMissingIsPrimary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Deliberately out of paired_at order in the JSON array itself, so
	// this also proves the migration picks the earliest TIME, not just
	// the first array element.
	raw := `{
		"core_id": "test-core",
		"core_name": "Test Core",
		"mode": "tv",
		"devices": [
			{"id": "dev-2", "name": "Second Phone", "key_hash": "abc", "paired_at": "2026-02-01T00:00:00Z", "last_seen": "2026-02-01T00:00:00Z", "device_type": "phone"},
			{"id": "dev-1", "name": "First Phone", "key_hash": "def", "paired_at": "2026-01-01T00:00:00Z", "last_seen": "2026-01-01T00:00:00Z", "device_type": "phone"}
		]
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("writing test state.json: %v", err)
	}

	st, err := readStateFile(path)
	if err != nil {
		t.Fatalf("readStateFile: %v", err)
	}
	if len(st.Devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(st.Devices))
	}

	var primaryCount int
	var primaryID string
	for _, d := range st.Devices {
		if d.IsPrimary {
			primaryCount++
			primaryID = d.ID
		}
	}
	if primaryCount != 1 {
		t.Fatalf("expected exactly 1 device to be auto-promoted, got %d", primaryCount)
	}
	if primaryID != "dev-1" {
		t.Fatalf("expected the earliest-paired device (dev-1) to become primary, got %q", primaryID)
	}
}

// TestLoadPreservesExplicitIsPrimary confirms migration doesn't kick
// in, or double-promote, when a real Stage-16-era state.json already
// has a primary set correctly.
func TestLoadPreservesExplicitIsPrimary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	raw := `{
		"core_id": "test-core",
		"core_name": "Test Core",
		"mode": "tv",
		"devices": [
			{"id": "dev-1", "name": "Guest Phone", "key_hash": "abc", "paired_at": "2026-01-01T00:00:00Z", "last_seen": "2026-01-01T00:00:00Z", "device_type": "phone", "is_primary": false},
			{"id": "dev-2", "name": "Primary Phone", "key_hash": "def", "paired_at": "2026-02-01T00:00:00Z", "last_seen": "2026-02-01T00:00:00Z", "device_type": "phone", "is_primary": true}
		]
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("writing test state.json: %v", err)
	}

	st, err := readStateFile(path)
	if err != nil {
		t.Fatalf("readStateFile: %v", err)
	}
	if st.Devices[0].IsPrimary {
		t.Fatal("explicit non-primary device should not have been touched")
	}
	if !st.Devices[1].IsPrimary {
		t.Fatal("explicit primary device should have been preserved")
	}
}

// TestPrimaryStatusSurvivesRestart covers verification item 7: the
// restriction must not be an in-memory-only fluke. This simulates a
// real restart by closing over the same on-disk directory and calling
// Open again fresh -- exactly what happens when a6core restarts on
// the real Acer -- and confirms IsPrimary is read back correctly from
// the real saved state.json, not just held in a running process.
func TestPrimaryStatusSurvivesRestart(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	err = store.Update(func(st *State) error {
		st.Devices = append(st.Devices, Device{
			ID:        "dev-1",
			Name:      "Primary Phone",
			KeyHash:   "somehash",
			IsPrimary: true,
		})
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Simulate a real restart: a brand new Store, opened fresh against
	// the same directory, with no shared in-memory state at all.
	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("re-Open after simulated restart: %v", err)
	}
	devices := reopened.Snapshot().Devices
	if len(devices) != 1 {
		t.Fatalf("expected 1 device after restart, got %d", len(devices))
	}
	if !devices[0].IsPrimary {
		t.Fatal("IsPrimary should have survived a real save + reload, not just lived in memory")
	}
}
