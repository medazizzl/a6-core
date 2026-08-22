package state

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	defaultDir   = "/var/lib/a6core"
	iconsDirName = "icons"
	stateFile    = "state.json"
	backupFile   = "state.json.bak"
)

// defaultMode is used both for a brand-new install and as the
// backward-compatible fallback for any state.json written before
// Stage 8 added the Mode field (see readStateFile).
const defaultMode = "tv"

type State struct {
	CoreID    string     `json:"core_id"`
	CoreName  string     `json:"core_name"`
	Mode      string     `json:"mode"`
	Devices   []Device   `json:"devices"`
	Shortcuts []Shortcut `json:"shortcuts"`
	Servers   []Server   `json:"servers"`
	Settings  Settings   `json:"settings"`
}

type Device struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	KeyHash  string    `json:"key_hash"`
	PairedAt time.Time `json:"paired_at"`
	LastSeen time.Time `json:"last_seen"`
}

type Shortcut struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	IconID string `json:"icon_id,omitempty"`
}

type Server struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Name   string         `json:"name"`
	Config map[string]any `json:"config,omitempty"`
}

type Settings struct {
	Port int `json:"port"`
}

type Store struct {
	mu    sync.RWMutex
	state State
	dir   string
}

func Open(dir string) (*Store, error) {
	if dir == "" {
		dir = defaultDir
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("state: creating state dir %s: %w", dir, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, iconsDirName), 0o750); err != nil {
		return nil, fmt.Errorf("state: creating icons dir: %w", err)
	}
	s := &Store{dir: dir}
	st, err := s.load()
	if err != nil {
		return nil, err
	}
	s.state = st
	return s, nil
}

func (s *Store) IconsDir() string {
	return filepath.Join(s.dir, iconsDirName)
}

func (s *Store) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return deepCopy(s.state)
}

func (s *Store) Update(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := deepCopy(s.state)
	if err := fn(&s.state); err != nil {
		s.state = before
		return err
	}
	if err := s.saveLocked(); err != nil {
		s.state = before
		return fmt.Errorf("state: save failed, changes rolled back: %w", err)
	}
	return nil
}

func (s *Store) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("state: marshaling state: %w", err)
	}
	target := filepath.Join(s.dir, stateFile)
	backup := filepath.Join(s.dir, backupFile)

	tmp, err := os.CreateTemp(s.dir, "state.json.tmp-*")
	if err != nil {
		return fmt.Errorf("state: creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("state: writing temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("state: fsyncing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("state: closing temp file: %w", err)
	}

	if _, err := os.Stat(target); err == nil {
		if err := copyFileAtomic(target, backup); err != nil {
			log.Printf("state: WARNING could not update state.json.bak: %v", err)
		}
	}

	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("state: renaming into place: %w", err)
	}

	if dirF, err := os.Open(s.dir); err == nil {
		_ = dirF.Sync()
		dirF.Close()
	}

	return nil
}

func (s *Store) load() (State, error) {
	primary := filepath.Join(s.dir, stateFile)
	backup := filepath.Join(s.dir, backupFile)
	st, err := readStateFile(primary)
	switch {
	case err == nil:
		return st, nil
	case errors.Is(err, fs.ErrNotExist):
		log.Printf("state: no existing state.json found, initializing fresh state")
		return freshState(), nil
	default:
		log.Printf("state: WARNING primary state.json unreadable (%v), trying backup", err)
	}

	st, err = readStateFile(backup)
	if err == nil {
		log.Printf("state: recovered from state.json.bak successfully")
		return st, nil
	}

	ts := time.Now().Format("20060102-150405")
	quarantine(primary, fmt.Sprintf("%s.corrupt-%s", primary, ts))
	quarantine(backup, fmt.Sprintf("%s.corrupt-%s", backup, ts))
	log.Printf("state: ERROR both state.json and state.json.bak were unreadable; "+
		"originals preserved with a .corrupt-%s suffix for inspection; "+
		"starting from a fresh empty state so the appliance can still boot", ts)
	return freshState(), nil
}

func readStateFile(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	// Backward-compatible migration: any state.json written before
	// Stage 8 added Mode won't have this field at all, which decodes
	// to "" rather than erroring. "" is not a valid mode, so treat it
	// as an implicit default rather than letting it propagate.
	if st.Mode == "" {
		st.Mode = defaultMode
	}
	return st, nil
}

func quarantine(path, newPath string) {
	if err := os.Rename(path, newPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("state: could not quarantine %s: %v", path, err)
	}
}

func freshState() State {
	return State{
		CoreID:    NewID(),
		CoreName:  "Project A6",
		Mode:      defaultMode,
		Devices:   []Device{},
		Shortcuts: []Shortcut{},
		Servers:   []Server{},
		Settings:  Settings{Port: 7887},
	}
}

func copyFileAtomic(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "state.bak.tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, dst)
}

func deepCopy(st State) State {
	data, err := json.Marshal(st)
	if err != nil {
		panic(fmt.Sprintf("state: deepCopy marshal failed (should be unreachable): %v", err))
	}
	var out State
	if err := json.Unmarshal(data, &out); err != nil {
		panic(fmt.Sprintf("state: deepCopy unmarshal failed (should be unreachable): %v", err))
	}
	return out
}

func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
