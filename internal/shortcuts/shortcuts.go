// Package shortcuts implements CRUD for Quick Access shortcuts, per
// the frozen spec §2-4/§8, orchestrating internal/state (persistence)
// and internal/icons (explicit upload, auto-favicon, embedded
// fallback — Waves 1-3 of Stage 10).
package shortcuts

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"a6core/internal/icons"
	"a6core/internal/state"
)

var (
	ErrNotFound      = errors.New("shortcuts: not found")
	ErrInvalidName   = errors.New("shortcuts: name must not be empty")
	ErrInvalidURL    = errors.New("shortcuts: url must be an absolute http or https URL")
	ErrIconNotFound  = errors.New("shortcuts: icon_id does not reference an existing icon")
)

// Store orchestrates shortcut persistence and icon resolution.
type Store struct {
	stateStore *state.Store
	iconStore  *icons.Store
}

func New(stateStore *state.Store, iconStore *icons.Store) *Store {
	return &Store{stateStore: stateStore, iconStore: iconStore}
}

func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrInvalidName
	}
	return name, nil
}

func validateURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", ErrInvalidURL
	}
	return raw, nil
}

// resolveIcon implements the priority order agreed in the Stage 10
// review: an explicit, existing icon_id always wins; otherwise a
// bounded favicon fetch is attempted; any failure there is a SOFT
// failure — it resolves to "" (the embedded default), never an error
// that blocks shortcut creation.
func (s *Store) resolveIcon(explicitIconID, shortcutURL string) (string, error) {
	if explicitIconID != "" {
		if !s.iconStore.Exists(explicitIconID) {
			return "", ErrIconNotFound
		}
		return explicitIconID, nil
	}
	fetched, err := s.iconStore.FetchFavicon(shortcutURL)
	if err != nil {
		return "", nil // soft failure: fall back to the embedded default
	}
	return fetched, nil
}

// List returns all shortcuts. Always a non-nil slice (even when
// empty) so it marshals to JSON "[]", not "null" — matters for the
// mobile app parsing GET /v1/shortcuts.
func (s *Store) List() []state.Shortcut {
	snap := s.stateStore.Snapshot()
	out := make([]state.Shortcut, len(snap.Shortcuts))
	copy(out, snap.Shortcuts)
	return out
}

// Get returns a single shortcut by id, or ErrNotFound.
func (s *Store) Get(id string) (state.Shortcut, error) {
	snap := s.stateStore.Snapshot()
	for _, sc := range snap.Shortcuts {
		if sc.ID == id {
			return sc, nil
		}
	}
	return state.Shortcut{}, ErrNotFound
}

// Create validates name/url, resolves the icon per the priority
// order above, and persists the new shortcut atomically.
func (s *Store) Create(name, rawURL, explicitIconID string) (state.Shortcut, error) {
	name, err := validateName(name)
	if err != nil {
		return state.Shortcut{}, err
	}
	validURL, err := validateURL(rawURL)
	if err != nil {
		return state.Shortcut{}, err
	}
	iconID, err := s.resolveIcon(explicitIconID, validURL)
	if err != nil {
		return state.Shortcut{}, err
	}

	sc := state.Shortcut{
		ID:     state.NewID(),
		Name:   name,
		URL:    validURL,
		IconID: iconID,
	}

	err = s.stateStore.Update(func(st *state.State) error {
		st.Shortcuts = append(st.Shortcuts, sc)
		return nil
	})
	if err != nil {
		return state.Shortcut{}, fmt.Errorf("shortcuts: persisting new shortcut: %w", err)
	}
	return sc, nil
}

// UpdateInput carries PUT /v1/shortcuts/{id}'s optional fields.
// Nil pointers mean "leave unchanged". IconID's semantics: nil =
// leave unchanged; pointer to "" = explicitly clear to the embedded
// default; pointer to a real id = must already exist. RefreshIcon,
// only when IconID is nil, re-attempts a favicon fetch — normal
// renames never trigger a fetch, per the earlier decision.
type UpdateInput struct {
	Name        *string
	URL         *string
	IconID      *string
	RefreshIcon bool
}

func (s *Store) Update(id string, input UpdateInput) (state.Shortcut, error) {
	var updated state.Shortcut
	var found bool

	err := s.stateStore.Update(func(st *state.State) error {
		idx := -1
		for i, sc := range st.Shortcuts {
			if sc.ID == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			return nil // handled as ErrNotFound below, outside the closure
		}
		found = true
		sc := st.Shortcuts[idx]

		if input.Name != nil {
			n, err := validateName(*input.Name)
			if err != nil {
				return err
			}
			sc.Name = n
		}
		if input.URL != nil {
			u, err := validateURL(*input.URL)
			if err != nil {
				return err
			}
			sc.URL = u
		}

		switch {
		case input.IconID != nil && *input.IconID == "":
			sc.IconID = "" // explicit clear to embedded default
		case input.IconID != nil:
			if !s.iconStore.Exists(*input.IconID) {
				return ErrIconNotFound
			}
			sc.IconID = *input.IconID
		case input.RefreshIcon:
			fetched, ferr := s.iconStore.FetchFavicon(sc.URL)
			if ferr == nil {
				sc.IconID = fetched
			} else {
				sc.IconID = "" // soft failure, same as Create
			}
		}
		// otherwise: icon left exactly as it was — a plain rename
		// must never silently trigger a network fetch.

		st.Shortcuts[idx] = sc
		updated = sc
		return nil
	})

	if err != nil {
		return state.Shortcut{}, err
	}
	if !found {
		return state.Shortcut{}, ErrNotFound
	}
	return updated, nil
}

// Delete removes a shortcut by id. Note: per the Stage 10 blueprint,
// this deliberately does NOT clean up the shortcut's icon file on
// disk — accepted, not fixed, in v1 (a little orphaned disk usage
// against a 512KB cap, rather than the complexity of reference
// counting shared icon files).
func (s *Store) Delete(id string) error {
	var found bool
	err := s.stateStore.Update(func(st *state.State) error {
		idx := -1
		for i, sc := range st.Shortcuts {
			if sc.ID == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			return nil
		}
		found = true
		st.Shortcuts = append(st.Shortcuts[:idx], st.Shortcuts[idx+1:]...)
		return nil
	})
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	return nil
}
