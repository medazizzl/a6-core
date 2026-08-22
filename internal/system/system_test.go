package system

import (
	"errors"
	"testing"

	"a6core/internal/resources"
)

func TestSystemInfoReturnsFields(t *testing.T) {
	mgr := NewManager("v0.1.0-test", nil, nil, nil)
	info, err := mgr.Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.A6CoreVersion != "v0.1.0-test" {
		t.Fatalf("expected version v0.1.0-test, got %q", info.A6CoreVersion)
	}
	if info.Hostname == "" {
		t.Fatal("expected non-empty hostname")
	}
	if info.KernelVersion == "" {
		t.Fatal("expected non-empty kernel version")
	}
}

func TestRebootBlockedWithoutForce(t *testing.T) {
	fakeChecker := func() []resources.BlockingResource {
		return []resources.BlockingResource{{Type: "server", ID: "minecraft", Players: 1}}
	}
	mgr := NewManager("v0.1.0", fakeChecker, nil, nil)
	err := mgr.Reboot(false)
	var blocked *resources.ErrBlocked
	if !errors.As(err, &blocked) {
		t.Fatalf("expected *resources.ErrBlocked, got %v", err)
	}
	if len(blocked.Blocking) != 1 {
		t.Fatalf("expected 1 blocking resource, got %d", len(blocked.Blocking))
	}
}

func TestRebootForceOverridesBlock(t *testing.T) {
	fakeChecker := func() []resources.BlockingResource {
		return []resources.BlockingResource{{Type: "server", ID: "minecraft", Players: 1}}
	}
	mgr := NewManager("v0.1.0", fakeChecker, nil, nil)
	if err := mgr.Reboot(true); err != nil {
		t.Fatalf("expected force to override reboot block, got %v", err)
	}
}
