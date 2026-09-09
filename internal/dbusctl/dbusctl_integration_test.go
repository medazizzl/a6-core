//go:build integration

// These tests require a real system D-Bus bus with logind running —
// meaningful only on the actual Acer, never in a normal `go test
// ./...` run. Run explicitly with:
//   go test -tags=integration ./internal/dbusctl/... -v
// This file deliberately never calls Suspend/Reboot/PowerOff — only
// the Can* queries, which report permission without performing the
// action. The real, physical actions are verified manually, once,
// deliberately, per the Stage 15 verification plan — not from an
// automated test that could fire unexpectedly.
package dbusctl

import (
	"context"
	"testing"
)

func TestConnectToRealSystemBus(t *testing.T) {
	client, err := Connect()
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()
}

func TestCanRebootAndCanPowerOffReportPermissionValues(t *testing.T) {
	client, err := Connect()
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	valid := map[string]bool{"yes": true, "no": true, "challenge": true, "na": true}

	reboot, err := client.CanReboot(context.Background())
	if err != nil {
		t.Fatalf("CanReboot: %v", err)
	}
	t.Logf("CanReboot() on this machine: %q", reboot)
	if !valid[reboot] {
		t.Fatalf("unexpected CanReboot value: %q", reboot)
	}

	poweroff, err := client.CanPowerOff(context.Background())
	if err != nil {
		t.Fatalf("CanPowerOff: %v", err)
	}
	t.Logf("CanPowerOff() on this machine: %q", poweroff)
	if !valid[poweroff] {
		t.Fatalf("unexpected CanPowerOff value: %q", poweroff)
	}
}

// TestUnitActiveStateReportsRealState is read-only, like the Can*
// tests above -- it never calls StartUnit/StopUnit. Real start/stop
// of minecraft-bedrock-server.service is verified manually, once,
// deliberately, the same way Stage 15's real reboot/poweroff were --
// not from an automated test that could fire unexpectedly against a
// real running game server.
func TestUnitActiveStateReportsRealState(t *testing.T) {
	client, err := Connect()
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	valid := map[string]bool{"active": true, "inactive": true, "activating": true, "deactivating": true, "failed": true, "reloading": true}

	state, err := client.UnitActiveState(context.Background(), "minecraft-bedrock-server.service")
	if err != nil {
		t.Fatalf("UnitActiveState: %v", err)
	}
	t.Logf("minecraft-bedrock-server.service ActiveState on this machine: %q", state)
	if !valid[state] {
		t.Fatalf("unexpected ActiveState value: %q", state)
	}
}

func TestUnitActiveStateRefusesNonAllowlistedUnit(t *testing.T) {
	client, err := Connect()
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	// sshd.service definitely exists on this machine (it's how we're
	// running this test at all) -- proving THIS specific real unit is
	// refused, not just a nonexistent one, is what actually confirms
	// the allowlist check runs before any real D-Bus call is made.
	_, err = client.UnitActiveState(context.Background(), "sshd.service")
	if err == nil {
		t.Fatal("expected an error querying a non-allowlisted unit, got nil")
	}
}