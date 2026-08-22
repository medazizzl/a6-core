package modes

import (
	"errors"
	"fmt"

	"a6core/internal/resources"
	"a6core/internal/state"
)

type Mode string

const (
	ModeTV     Mode = "tv"
	ModeServer Mode = "server"
	ModeSleep  Mode = "sleep"
)

var AllModes = []Mode{ModeTV, ModeServer, ModeSleep}

func IsValid(m Mode) bool {
	for _, v := range AllModes {
		if v == m {
			return true
		}
	}
	return false
}

type SystemExecutor func(from, to Mode) error

func NoOpExecutor(from, to Mode) error { return nil }

var ErrInvalidMode = errors.New("modes: invalid mode")

type Manager struct {
	store    *state.Store
	checker  resources.Checker
	executor SystemExecutor
}

func NewManager(store *state.Store, checker resources.Checker, executor SystemExecutor) *Manager {
	if checker == nil {
		checker = resources.NoBlocking
	}
	if executor == nil {
		executor = NoOpExecutor
	}
	return &Manager{store: store, checker: checker, executor: executor}
}

func (m *Manager) Current() Mode {
	return Mode(m.store.Snapshot().Mode)
}

func (m *Manager) Switch(target Mode, force bool) (from Mode, err error) {
	if !IsValid(target) {
		return "", ErrInvalidMode
	}
	from = m.Current()
	if from == target {
		return from, nil
	}

	if !force {
		if blocking := m.checker(); len(blocking) > 0 {
			return from, &resources.ErrBlocked{Blocking: blocking}
		}
	}

	if err := m.executor(from, target); err != nil {
		return from, fmt.Errorf("modes: executor failed: %w", err)
	}

	err = m.store.Update(func(st *state.State) error {
		st.Mode = string(target)
		return nil
	})
	if err != nil {
		return from, fmt.Errorf("modes: persisting new mode: %w", err)
	}
	return from, nil
}
