package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMigratesMissingDeviceType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Simulate a state.json written before Wave 4 added DeviceType:
	// the device object has no "device_type" key at all.
	raw := `{
		"core_id": "test-core",
		"core_name": "Test Core",
		"mode": "tv",
		"devices": [
			{"id": "dev-1", "name": "Old Phone", "key_hash": "abc", "paired_at": "2026-01-01T00:00:00Z", "last_seen": "2026-01-01T00:00:00Z"}
		]
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("writing test state.json: %v", err)
	}

	st, err := readStateFile(path)
	if err != nil {
		t.Fatalf("readStateFile: %v", err)
	}
	if len(st.Devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(st.Devices))
	}
	if st.Devices[0].DeviceType != DeviceTypePhone {
		t.Fatalf("expected migrated device_type %q, got %q", DeviceTypePhone, st.Devices[0].DeviceType)
	}
}

func TestLoadPreservesExplicitDeviceType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	raw := `{
		"core_id": "test-core",
		"core_name": "Test Core",
		"mode": "tv",
		"devices": [
			{"id": "dev-1", "name": "TV Shell", "key_hash": "abc", "paired_at": "2026-01-01T00:00:00Z", "last_seen": "2026-01-01T00:00:00Z", "device_type": "shell"}
		]
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("writing test state.json: %v", err)
	}

	st, err := readStateFile(path)
	if err != nil {
		t.Fatalf("readStateFile: %v", err)
	}
	if st.Devices[0].DeviceType != DeviceTypeShell {
		t.Fatalf("expected preserved device_type %q, got %q", DeviceTypeShell, st.Devices[0].DeviceType)
	}
}
