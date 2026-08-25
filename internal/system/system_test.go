package system

import (
	"context"
	"errors"
	"testing"
)

// fakeExecutor is a test implementation of ActionExecutor.
type fakeExecutor struct {
	rebootErr  error
	powerOffErr error
}

func (f *fakeExecutor) Reboot(ctx context.Context) error {
	return f.rebootErr
}

func (f *fakeExecutor) PowerOff(ctx context.Context) error {
	return f.powerOffErr
}

func TestSystemInfoReturnsFields(t *testing.T) {
	mgr := NewManager(&fakeExecutor{})
	info, err := mgr.Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info["reboot_supported"] != true {
		t.Fatalf("expected reboot_supported=true")
	}
	if info["poweroff_supported"] != true {
		t.Fatalf("expected poweroff_supported=true")
	}
	if info["suspend_supported"] != false {
		t.Fatalf("expected suspend_supported=false")
	}
}

func TestRebootCallsExecutor(t *testing.T) {
	err := errors.New("simulated reboot error")
	mgr := NewManager(&fakeExecutor{rebootErr: err})
	err = mgr.Reboot(false)
	if !errors.Is(err, err) {
		t.Fatalf("expected executor error, got %v", err)
	}
}

func TestPowerOffCallsExecutor(t *testing.T) {
	mgr := NewManager(&fakeExecutor{})
	err := mgr.PowerOff(false)
	if err != nil {
		t.Fatalf("PowerOff: %v", err)
	}
}

func TestSuspendReturnsNotSupported(t *testing.T) {
	mgr := NewManager(&fakeExecutor{})
	err := mgr.Suspend(false)
	if !errors.Is(err, ErrActionNotSupported) {
		t.Fatalf("expected ErrActionNotSupported, got %v", err)
	}
}