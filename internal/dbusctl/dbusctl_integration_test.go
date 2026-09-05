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