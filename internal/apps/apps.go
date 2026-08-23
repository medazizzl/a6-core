// Package apps manages the single active "foreground" session — a
// browser, a saved shortcut, or a retro game — per the v1 constraint
// that only one such session exists at a time (spec §18's explicit
// exclusion list: "more than one simultaneous foreground app").
//
// Session state is deliberately kept in memory only, never written
// to state.json. Persisting "browser was open" would misrepresent an
// external process's live state after any restart that wasn't a
// clean shutdown — this mirrors state.go's own recovery philosophy
// (§12: reconstruct from ground truth, never trust a stale claim).
// The real ground-truth query is Stage 15's job; today's
// LaunchExecutor/CloseExecutor are stubs, same pattern as
// internal/modes and internal/system.
package apps

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"a6core/internal/resources"
	"a6core/internal/retro"
	"a6core/internal/shortcuts"
	"a6core/internal/state"
)

const (
	TypeBrowser  = "browser"
	TypeShortcut = "shortcut"
	TypeRetro    = "retro"
)

var (
	ErrInvalidType       = errors.New("apps: type must be one of: browser, shortcut, retro")
	ErrMissingShortcutID = errors.New(`apps: shortcut_id is required for type "shortcut"`)
	ErrMissingConsole    = errors.New(`apps: console is required for type "retro"`)
	ErrMissingGameID     = errors.New(`apps: game_id is required for type "retro"`)
	ErrGameNotFound      = errors.New("apps: game_id does not exist for the given console")
)

// Session describes the currently active foreground app. Console/
// GameID are only meaningful when Type == TypeRetro; ShortcutID only
// when Type == TypeShortcut — callers should switch on Type, not
// infer it from which fields are populated.
type Session struct {
	ID         string
	Type       string
	ShortcutID string
	Console    string
	GameID     string
	StartedAt  time.Time
}

// LaunchInput carries POST /v1/apps/launch's fields (and, via the
// shared implementation decision, POST /v1/retro/launch's fields
// too — wave 3 maps both HTTP routes onto the same LaunchInput
// shape).
type LaunchInput struct {
	Type       string
	ShortcutID string
	Console    string
	GameID     string
}

// LaunchExecutor actually starts a session on the machine. Stage 11
// ships NoOpLaunchExecutor, which always succeeds and does nothing
// real — Stage 15 supplies the real one.
type LaunchExecutor func(Session) error

func NoOpLaunchExecutor(Session) error { return nil }

// CloseExecutor actually stops whatever is currently running. Same
// stub-now/real-later pattern.
type CloseExecutor func() error

func NoOpCloseExecutor() error { return nil }

// Manager owns the single in-memory session slot. Guarded by its own
// mutex — deliberately NOT state.Store's locking, since this state
// is deliberately kept out of state.json entirely (see package doc).
type Manager struct {
	mu             sync.RWMutex
	current        *Session
	shortcutStore  *shortcuts.Store
	retroStore     *retro.Store
	launchExecutor LaunchExecutor
	closeExecutor  CloseExecutor
}

func NewManager(shortcutStore *shortcuts.Store, retroStore *retro.Store, launchExec LaunchExecutor, closeExec CloseExecutor) *Manager {
	if launchExec == nil {
		launchExec = NoOpLaunchExecutor
	}
	if closeExec == nil {
		closeExec = NoOpCloseExecutor
	}
	return &Manager{
		shortcutStore:  shortcutStore,
		retroStore:     retroStore,
		launchExecutor: launchExec,
		closeExecutor:  closeExec,
	}
}

// Current returns a copy of the active session, or nil if nothing is
// running. A copy, not a pointer into internal state — callers can't
// accidentally mutate Manager's view of reality.
func (m *Manager) Current() *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.current == nil {
		return nil
	}
	cp := *m.current
	return &cp
}

// Blocking reports the active session as a resources.BlockingResource,
// for Stage 12/15 to later register with modes/system's force-guard
// logic. Implemented now, deliberately not wired into anything yet —
// wiring it up is a separate, reviewed change for a later stage.
func (m *Manager) Blocking() []resources.BlockingResource {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.current == nil {
		return nil
	}
	id := m.current.Type
	switch m.current.Type {
	case TypeShortcut:
		id = "shortcut:" + m.current.ShortcutID
	case TypeRetro:
		id = fmt.Sprintf("retro:%s/%s", m.current.Console, m.current.GameID)
	}
	return []resources.BlockingResource{{Type: "app", ID: id}}
}

// validate checks input against the type-specific rules and returns
// a not-yet-started Session (no ID/StartedAt) on success. Runs
// BEFORE anything is closed or launched — a bad request never
// disturbs whatever's currently running.
func (m *Manager) validate(input LaunchInput) (Session, error) {
	switch input.Type {
	case TypeBrowser:
		return Session{Type: TypeBrowser}, nil

	case TypeShortcut:
		if input.ShortcutID == "" {
			return Session{}, ErrMissingShortcutID
		}
		if _, err := m.shortcutStore.Get(input.ShortcutID); err != nil {
			return Session{}, fmt.Errorf("apps: %w", err) // wraps shortcuts.ErrNotFound
		}
		return Session{Type: TypeShortcut, ShortcutID: input.ShortcutID}, nil

	case TypeRetro:
		if input.Console == "" {
			return Session{}, ErrMissingConsole
		}
		if err := retro.ValidateConsole(input.Console); err != nil {
			return Session{}, err // retro.ErrUnknownConsole, same allowlist as the games-listing route
		}
		if input.GameID == "" {
			return Session{}, ErrMissingGameID
		}
		ok, err := m.retroStore.GameExists(input.Console, input.GameID)
		if err != nil {
			return Session{}, err
		}
		if !ok {
			return Session{}, ErrGameNotFound
		}
		return Session{Type: TypeRetro, Console: input.Console, GameID: input.GameID}, nil

	default:
		return Session{}, ErrInvalidType
	}
}

// Launch validates input, then implements the agreed "implicit
// replace" behavior: close whatever's running (if anything), then
// start the new session — sequential, never parallel (decision 3b).
//
// Two failure modes, both deliberate:
//   - close fails: current is left UNCHANGED (still the old session)
//     — we don't actually know it stopped, so claiming otherwise
//     would be dishonest.
//   - close succeeds but the new launch fails: current becomes nil,
//     not a resurrection of the old session (decision 3c) — the old
//     one is genuinely gone, and the new one never actually started.
func (m *Manager) Launch(input LaunchInput) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	candidate, err := m.validate(input)
	if err != nil {
		return Session{}, err
	}

	if m.current != nil {
		if err := m.closeExecutor(); err != nil {
			return Session{}, fmt.Errorf("apps: failed to close current session before launching new one: %w", err)
		}
		m.current = nil
	}

	candidate.ID = state.NewID()
	candidate.StartedAt = time.Now()

	if err := m.launchExecutor(candidate); err != nil {
		m.current = nil // fail-closed: old is gone, new never started — nothing is "current"
		return Session{}, fmt.Errorf("apps: launch failed: %w", err)
	}

	m.current = &candidate
	return candidate, nil
}

// Close stops the active session, if any. Idempotent when nothing is
// running (decision 3a) — returns nil without ever invoking the
// executor, rather than treating "already closed" as an error.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current == nil {
		return nil
	}
	if err := m.closeExecutor(); err != nil {
		return fmt.Errorf("apps: close failed: %w", err)
	}
	m.current = nil
	return nil
}
