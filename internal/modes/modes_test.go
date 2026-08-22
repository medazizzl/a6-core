package modes

import (
	"errors"
	"testing"

	"a6core/internal/resources"
	"a6core/internal/state"
)

func newTestStore(t *testing.T) *state.Store {
	t.Helper()
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	return store
}

func TestSwitchValidTransition(t *testing.T) {
	store := newTestStore(t)
	mgr := NewManager(store, nil, nil)
	if got := mgr.Current(); got != ModeTV {
		t.Fatalf("expected default mode tv, got %q", got)
	}
	from, err := mgr.Switch(ModeServer, false)
	if err != nil {
		t.Fatalf("Switch: %v", err)
	}
	if from != ModeTV {
		t.Fatalf("expected from=tv, got %q", from)
	}
	if got := mgr.Current(); got != ModeServer {
		t.Fatalf("expected current mode server, got %q", got)
	}
}

func TestSwitchInvalidMode(t *testing.T) {
	mgr := NewManager(newTestStore(t), nil, nil)
	if _, err := mgr.Switch(Mode("nonsense"), false); err != ErrInvalidMode {
		t.Fatalf("expected ErrInvalidMode, got %v", err)
	}
}

func TestSwitchSameModeIsNoOp(t *testing.T) {
	mgr := NewManager(newTestStore(t), nil, nil)
	if _, err := mgr.Switch(ModeTV, false); err != nil {
		t.Fatalf("expected no-op switch to succeed, got %v", err)
	}
}

func TestSwitchBlockedWithoutForce(t *testing.T) {
	fakeChecker := func() []resources.BlockingResource {
		return []resources.BlockingResource{{Type: "server", ID: "minecraft", Players: 2}}
	}
	store := newTestStore(t)
	mgr := NewManager(store, fakeChecker, nil)
	_, err := mgr.Switch(ModeServer, false)
	var blocked *resources.ErrBlocked
	if !errors.As(err, &blocked) {
		t.Fatalf("expected *resources.ErrBlocked, got %v", err)
	}
	if len(blocked.Blocking) != 1 {
		t.Fatalf("expected 1 blocking resource, got %d", len(blocked.Blocking))
	}
	if got := mgr.Current(); got != ModeTV {
		t.Fatalf("mode should not have changed, got %q", got)
	}
}

func TestSwitchForceOverridesBlock(t *testing.T) {
	fakeChecker := func() []resources.BlockingResource {
		return []resources.BlockingResource{{Type: "server", ID: "minecraft", Players: 2}}
	}
	store := newTestStore(t)
	mgr := NewManager(store, fakeChecker, nil)
	if _, err := mgr.Switch(ModeServer, true); err != nil {
		t.Fatalf("expected force to override block, got %v", err)
	}
	if got := mgr.Current(); got != ModeServer {
		t.Fatalf("expected mode server after forced switch, got %q", got)
	}
}
