package servers

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestListReturnsCatalogInOrder(t *testing.T) {
	m := NewManager(Executor{}, 0, 0)
	list := m.List()
	if len(list) != len(catalog) {
		t.Fatalf("got %d servers, want %d", len(list), len(catalog))
	}
	if list[0].ID != "minecraft" {
		t.Fatalf("expected minecraft first, got %q", list[0].ID)
	}
	if list[0].Status != StatusStopped {
		t.Fatalf("expected initial status stopped, got %q", list[0].Status)
	}
}

func TestGetUnknownReturnsErrNotFound(t *testing.T) {
	m := NewManager(Executor{}, 0, 0)
	if _, err := m.Get("terraria"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetKnownServer(t *testing.T) {
	m := NewManager(Executor{}, 0, 0)
	srv, err := m.Get("minecraft")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if srv.Type != "minecraft-bedrock" || srv.Port != 19132 {
		t.Fatalf("unexpected server: %+v", srv)
	}
}

func TestBlockingEmptyWhenNothingRunning(t *testing.T) {
	m := NewManager(Executor{}, 0, 0)
	if b := m.Blocking(); b != nil {
		t.Fatalf("expected no blocking resources at startup, got %v", b)
	}
}

func TestBlockingReflectsRunningWithPlayers(t *testing.T) {
	m := NewManager(Executor{}, 0, 0)
	m.setStatus("minecraft", StatusRunning)
	m.SetPlayers("minecraft", &Players{Current: 3, Max: 10})

	b := m.Blocking()
	if len(b) != 1 || b[0].Type != "server" || b[0].ID != "minecraft" || b[0].Players != 3 {
		t.Fatalf("unexpected blocking resources: %+v", b)
	}
}

func TestBlockingIgnoresRunningWithZeroPlayers(t *testing.T) {
	m := NewManager(Executor{}, 0, 0)
	m.setStatus("minecraft", StatusRunning)
	m.SetPlayers("minecraft", &Players{Current: 0})

	if b := m.Blocking(); b != nil {
		t.Fatalf("expected no blocking resources with zero players, got %v", b)
	}
}

// --- Wave 2 tests below ---

func fastManager(t *testing.T, exec Executor) *Manager {
	t.Helper()
	// Milliseconds, not the real 30s/60s spec defaults — same
	// injectable-duration principle as icons.go's 3s favicon timeout:
	// the REAL logic under test is unchanged, only how long it takes.
	m := NewManager(exec, 20*time.Millisecond, 100*time.Millisecond)
	m.pollEvery = 5 * time.Millisecond
	return m
}

func TestStartSucceeds(t *testing.T) {
	m := fastManager(t, NoOpExecutor())
	srv, err := m.Start("minecraft")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if srv.Status != StatusRunning {
		t.Fatalf("expected status running after Start, got %q", srv.Status)
	}
}

func TestStartUnknownServer(t *testing.T) {
	m := fastManager(t, NoOpExecutor())
	if _, err := m.Start("terraria"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStartAlreadyRunningRejected(t *testing.T) {
	m := fastManager(t, NoOpExecutor())
	if _, err := m.Start("minecraft"); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if _, err := m.Start("minecraft"); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("expected ErrAlreadyRunning, got %v", err)
	}
}

func TestStartExecutorFailureLeavesStopped(t *testing.T) {
	exec := NoOpExecutor()
	exec.Start = func(Server) error { return errors.New("simulated start failure") }
	m := fastManager(t, exec)

	if _, err := m.Start("minecraft"); err == nil {
		t.Fatal("expected Start to fail")
	}
	srv, _ := m.Get("minecraft")
	if srv.Status != StatusStopped {
		t.Fatalf("a failed Start must leave status stopped (honest), got %q", srv.Status)
	}
}

func TestStopNotRunningRejected(t *testing.T) {
	m := fastManager(t, NoOpExecutor())
	if err := m.Stop("minecraft"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("expected ErrNotRunning, got %v", err)
	}
}

func TestStopFullSequenceReachesStopped(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	exec := Executor{
		Start: func(Server) error { return nil },
		Broadcast: func(_ Server, msg string) error {
			mu.Lock()
			calls = append(calls, "broadcast:"+msg)
			mu.Unlock()
			return nil
		},
		Stop: func(Server) error {
			mu.Lock()
			calls = append(calls, "stop")
			mu.Unlock()
			return nil
		},
		IsStopped: func(Server) (bool, error) {
			return true, nil // confirm immediately on first poll
		},
	}
	m := fastManager(t, exec)

	if _, err := m.Start("minecraft"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.Stop("minecraft"); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Status flips to "stopping" near-instantly — Stop must not block.
	srv, _ := m.Get("minecraft")
	if srv.Status != StatusStopping {
		t.Fatalf("expected status stopping immediately after Stop returns, got %q", srv.Status)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		srv, _ = m.Get("minecraft")
		if srv.Status == StatusStopped {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if srv.Status != StatusStopped {
		t.Fatalf("expected status stopped after the sequence completes, got %q", srv.Status)
	}
	if srv.Players != nil {
		t.Fatal("expected players to be cleared once genuinely stopped")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 || calls[0] != "broadcast:Server shutting down soon" || calls[1] != "stop" {
		t.Fatalf("unexpected call sequence: %v", calls)
	}
}

func TestStopEscalatesWhenNeverConfirmed(t *testing.T) {
	var stopCalls int
	var mu sync.Mutex
	exec := Executor{
		Start:     func(Server) error { return nil },
		Broadcast: func(Server, string) error { return nil },
		Stop: func(Server) error {
			mu.Lock()
			stopCalls++
			mu.Unlock()
			return nil
		},
		IsStopped: func(Server) (bool, error) {
			return false, nil // never confirms — forces the escalation path
		},
	}
	m := fastManager(t, exec)

	if _, err := m.Start("minecraft"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.Stop("minecraft"); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	time.Sleep(300 * time.Millisecond) // well past the 20ms grace + 100ms maxWait fixture

	srv, _ := m.Get("minecraft")
	if srv.Status == StatusStopped {
		t.Fatal("expected status to remain stopping (honest ambiguity), not falsely claim stopped")
	}

	mu.Lock()
	defer mu.Unlock()
	if stopCalls != 2 {
		t.Fatalf("expected exactly 2 Stop calls (graceful attempt + escalation), got %d", stopCalls)
	}
}

func TestStopWhileAlreadyStoppingRejected(t *testing.T) {
	exec := NoOpExecutor()
	exec.IsStopped = func(Server) (bool, error) {
		time.Sleep(50 * time.Millisecond) // keep the sequence in flight briefly
		return true, nil
	}
	m := fastManager(t, exec)

	if _, err := m.Start("minecraft"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.Stop("minecraft"); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := m.Stop("minecraft"); !errors.Is(err, ErrStopInProgress) {
		t.Fatalf("expected ErrStopInProgress for a concurrent second Stop, got %v", err)
	}
}

// --- Wave 3 test below ---

func TestForceRestartStopSequenceBypassesInProgressGuard(t *testing.T) {
	exec := NoOpExecutor()
	exec.IsStopped = func(Server) (bool, error) {
		time.Sleep(30 * time.Millisecond)
		return true, nil
	}
	m := fastManager(t, exec)

	if _, err := m.Start("minecraft"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.Stop("minecraft"); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	// Without force, this is exactly TestStopWhileAlreadyStoppingRejected's
	// scenario. ForceRestartStopSequence must succeed where a second
	// plain Stop() would not.
	if err := m.ForceRestartStopSequence("minecraft"); err != nil {
		t.Fatalf("ForceRestartStopSequence: %v", err)
	}
}