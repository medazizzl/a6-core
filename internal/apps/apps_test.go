package apps

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"a6core/internal/icons"
	"a6core/internal/retro"
	"a6core/internal/shortcuts"
	"a6core/internal/state"
)

func newTestManager(t *testing.T, launchExec LaunchExecutor, closeExec CloseExecutor) (*Manager, *shortcuts.Store, *retro.Store) {
	t.Helper()
	dir := t.TempDir()
	stateStore, err := state.Open(dir)
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	iconStore := icons.New(stateStore)
	scStore := shortcuts.New(stateStore, iconStore)
	retroStore := retro.New(dir)
	return NewManager(scStore, retroStore, launchExec, closeExec), scStore, retroStore
}

func TestLaunchBrowserSucceeds(t *testing.T) {
	m, _, _ := newTestManager(t, nil, nil)
	sc, err := m.Launch(LaunchInput{Type: TypeBrowser})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if sc.Type != TypeBrowser || sc.ID == "" || sc.StartedAt.IsZero() {
		t.Fatalf("unexpected session: %+v", sc)
	}
	if cur := m.Current(); cur == nil || cur.ID != sc.ID {
		t.Fatalf("Current() did not reflect the launched session: %+v", cur)
	}
}

func TestLaunchInvalidTypeRejected(t *testing.T) {
	m, _, _ := newTestManager(t, nil, nil)
	if _, err := m.Launch(LaunchInput{Type: "bogus"}); !errors.Is(err, ErrInvalidType) {
		t.Fatalf("expected ErrInvalidType, got %v", err)
	}
	if m.Current() != nil {
		t.Fatal("a rejected launch must not set current")
	}
}

func TestLaunchShortcutValidation(t *testing.T) {
	m, _, _ := newTestManager(t, nil, nil)
	if _, err := m.Launch(LaunchInput{Type: TypeShortcut}); !errors.Is(err, ErrMissingShortcutID) {
		t.Fatalf("expected ErrMissingShortcutID, got %v", err)
	}
	_, err := m.Launch(LaunchInput{Type: TypeShortcut, ShortcutID: "00000000-0000-0000-0000-000000000000"})
	if !errors.Is(err, shortcuts.ErrNotFound) {
		t.Fatalf("expected wrapped shortcuts.ErrNotFound, got %v", err)
	}
}

func TestLaunchShortcutSucceeds(t *testing.T) {
	m, scStore, _ := newTestManager(t, nil, nil)
	sc, err := scStore.Create("Test Site", "https://example.com", "")
	if err != nil {
		t.Fatalf("Create shortcut: %v", err)
	}
	session, err := m.Launch(LaunchInput{Type: TypeShortcut, ShortcutID: sc.ID})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if session.ShortcutID != sc.ID {
		t.Fatalf("expected shortcut_id %q, got %q", sc.ID, session.ShortcutID)
	}
}

func TestLaunchRetroValidation(t *testing.T) {
	m, _, _ := newTestManager(t, nil, nil)

	if _, err := m.Launch(LaunchInput{Type: TypeRetro}); !errors.Is(err, ErrMissingConsole) {
		t.Fatalf("expected ErrMissingConsole, got %v", err)
	}
	if _, err := m.Launch(LaunchInput{Type: TypeRetro, Console: "ps2", GameID: "x"}); !errors.Is(err, retro.ErrUnknownConsole) {
		t.Fatalf("expected retro.ErrUnknownConsole, got %v", err)
	}
	if _, err := m.Launch(LaunchInput{Type: TypeRetro, Console: "nes"}); !errors.Is(err, ErrMissingGameID) {
		t.Fatalf("expected ErrMissingGameID, got %v", err)
	}
	if _, err := m.Launch(LaunchInput{Type: TypeRetro, Console: "nes", GameID: "nope.nes"}); !errors.Is(err, ErrGameNotFound) {
		t.Fatalf("expected ErrGameNotFound, got %v", err)
	}
}

func TestLaunchRetroSucceeds(t *testing.T) {
	dir := t.TempDir()
	stateStore, err := state.Open(dir)
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	iconStore := icons.New(stateStore)
	scStore := shortcuts.New(stateStore, iconStore)
	retroStore := retro.New(dir)

	if err := writeRomFixture(dir, "nes", "Zelda.nes"); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	m := NewManager(scStore, retroStore, nil, nil)
	session, err := m.Launch(LaunchInput{Type: TypeRetro, Console: "nes", GameID: "Zelda.nes"})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if session.Console != "nes" || session.GameID != "Zelda.nes" {
		t.Fatalf("unexpected session: %+v", session)
	}
}

func TestImplicitReplaceClosesOldBeforeLaunchingNew(t *testing.T) {
	var events []string
	var mu sync.Mutex
	record := func(s string) {
		mu.Lock()
		events = append(events, s)
		mu.Unlock()
	}

	launchExec := func(s Session) error {
		record("launch:" + s.Type)
		return nil
	}
	closeExec := func() error {
		record("close")
		return nil
	}

	m, scStore, _ := newTestManager(t, launchExec, closeExec)
	sc, _ := scStore.Create("Site", "https://example.com", "")

	if _, err := m.Launch(LaunchInput{Type: TypeBrowser}); err != nil {
		t.Fatalf("first Launch: %v", err)
	}
	if _, err := m.Launch(LaunchInput{Type: TypeShortcut, ShortcutID: sc.ID}); err != nil {
		t.Fatalf("second Launch: %v", err)
	}

	want := []string{"launch:browser", "close", "launch:shortcut"}
	mu.Lock()
	got := append([]string(nil), events...)
	mu.Unlock()
	if len(got) != len(want) {
		t.Fatalf("event sequence = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event sequence = %v, want %v", got, want)
		}
	}

	cur := m.Current()
	if cur == nil || cur.Type != TypeShortcut || cur.ShortcutID != sc.ID {
		t.Fatalf("expected the new shortcut session to be current, got %+v", cur)
	}
}

func TestCloseIdempotentWhenNothingRunning(t *testing.T) {
	closeCalled := false
	closeExec := func() error {
		closeCalled = true
		return nil
	}
	m, _, _ := newTestManager(t, nil, closeExec)

	if err := m.Close(); err != nil {
		t.Fatalf("Close on idle manager should return nil, got %v", err)
	}
	if closeCalled {
		t.Fatal("Close on idle manager must not invoke the executor at all")
	}
}

func TestCloseClearsCurrentOnSuccess(t *testing.T) {
	m, _, _ := newTestManager(t, nil, nil)
	if _, err := m.Launch(LaunchInput{Type: TypeBrowser}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if m.Current() != nil {
		t.Fatal("expected Current() to be nil after a successful Close")
	}
}

func TestCloseFailureKeepsCurrentUnchanged(t *testing.T) {
	closeExec := func() error { return errors.New("simulated close failure") }
	m, _, _ := newTestManager(t, nil, closeExec)

	if _, err := m.Launch(LaunchInput{Type: TypeBrowser}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	before := m.Current()

	if err := m.Close(); err == nil {
		t.Fatal("expected Close to return an error when the executor fails")
	}
	after := m.Current()
	if after == nil || after.ID != before.ID {
		t.Fatalf("a failed Close must leave current unchanged: before=%+v after=%+v", before, after)
	}
}

func TestLaunchCloseFailureLeavesOldSessionCurrent(t *testing.T) {
	closeExec := func() error { return errors.New("simulated close failure") }
	m, scStore, _ := newTestManager(t, nil, closeExec)
	sc, _ := scStore.Create("Site", "https://example.com", "")

	first, err := m.Launch(LaunchInput{Type: TypeBrowser})
	if err != nil {
		t.Fatalf("first Launch: %v", err)
	}

	_, err = m.Launch(LaunchInput{Type: TypeShortcut, ShortcutID: sc.ID})
	if err == nil {
		t.Fatal("expected the second Launch to fail when closing the first fails")
	}

	cur := m.Current()
	if cur == nil || cur.ID != first.ID {
		t.Fatalf("expected the ORIGINAL session to remain current when close fails, got %+v", cur)
	}
}

func TestLaunchFailureAfterSuccessfulCloseResultsInNilCurrent(t *testing.T) {
	launchCount := 0
	launchExec := func(s Session) error {
		launchCount++
		if launchCount == 2 {
			return errors.New("simulated launch failure")
		}
		return nil
	}
	m, scStore, _ := newTestManager(t, launchExec, nil)
	sc, _ := scStore.Create("Site", "https://example.com", "")

	if _, err := m.Launch(LaunchInput{Type: TypeBrowser}); err != nil {
		t.Fatalf("first Launch: %v", err)
	}

	_, err := m.Launch(LaunchInput{Type: TypeShortcut, ShortcutID: sc.ID})
	if err == nil {
		t.Fatal("expected the second Launch to fail")
	}

	if cur := m.Current(); cur != nil {
		t.Fatalf("decision 3c: current must be nil (fail-closed, no resurrection), got %+v", cur)
	}
}

func TestBlockingReflectsCurrentSession(t *testing.T) {
	m, scStore, _ := newTestManager(t, nil, nil)

	if b := m.Blocking(); b != nil {
		t.Fatalf("expected no blocking resources when idle, got %v", b)
	}

	if _, err := m.Launch(LaunchInput{Type: TypeBrowser}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	b := m.Blocking()
	if len(b) != 1 || b[0].Type != "app" || b[0].ID != "browser" {
		t.Fatalf("unexpected blocking resource for browser: %+v", b)
	}

	sc, _ := scStore.Create("Site", "https://example.com", "")
	if _, err := m.Launch(LaunchInput{Type: TypeShortcut, ShortcutID: sc.ID}); err != nil {
		t.Fatalf("Launch shortcut: %v", err)
	}
	b = m.Blocking()
	wantID := "shortcut:" + sc.ID
	if len(b) != 1 || b[0].ID != wantID {
		t.Fatalf("unexpected blocking resource for shortcut: got %+v, want id %q", b, wantID)
	}
}

// TestConcurrentLaunchAndCloseIsRaceFree is only meaningful when run
// with the race detector: `go test -race ./internal/apps/...`. It
// proves Manager's mutex actually protects the first mutable,
// non-persisted shared state in this project.
func TestConcurrentLaunchAndCloseIsRaceFree(t *testing.T) {
	m, _, _ := newTestManager(t, nil, nil)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = m.Launch(LaunchInput{Type: TypeBrowser})
		}()
		go func() {
			defer wg.Done()
			_ = m.Close()
		}()
	}
	wg.Wait()

	// No assertion on the final state beyond internal consistency —
	// the point of this test is what the race detector observes
	// during the run, not the end result. A nil or a fully-formed
	// session are both valid outcomes of a fair race.
	if cur := m.Current(); cur != nil && cur.ID == "" {
		t.Fatal("current session, if non-nil, must be fully formed (non-empty ID)")
	}
}

func writeRomFixture(dataDir, console, filename string) error {
	dir := filepath.Join(dataDir, "roms", console)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, filename), []byte("\x00fake"), 0o644)
}
