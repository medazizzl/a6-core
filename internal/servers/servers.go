// Package servers implements the server registry and lifecycle
// management described in the frozen spec §2-4/§8/§10 — currently
// backing exactly one entry, Minecraft, the flagship example named
// throughout the project brief.
//
// Server definitions are a small hardcoded catalog (mirrors
// internal/retro's console list) rather than state.json-backed CRUD,
// because the frozen spec defines list/get/start/stop but no create
// endpoint — see Stage 12 design notes for the full reasoning.
//
// Runtime status is kept in memory only, never persisted — same
// reasoning as internal/apps' session state: a persisted claim about
// a live external process would misrepresent reality after any
// restart that wasn't clean.
package servers

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"a6core/internal/resources"
)

type Status string

const (
	StatusStopped  Status = "stopped"
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusStopping Status = "stopping"
)

var (
	ErrNotFound        = errors.New("servers: unknown server id")
	ErrAlreadyRunning  = errors.New("servers: server is already running or starting")
	ErrStopInProgress  = errors.New("servers: a stop is already in progress")
	ErrNotRunning      = errors.New("servers: server is not running")
)

type definition struct {
	id   string
	typ  string
	name string
	port int
}

var catalog = []definition{
	{id: "minecraft", typ: "minecraft", name: "Survival World", port: 25565},
}

func findDefinition(id string) (definition, bool) {
	for _, d := range catalog {
		if d.id == id {
			return d, true
		}
	}
	return definition{}, false
}

type Players struct {
	Current int `json:"current"`
	Max     int `json:"max,omitempty"`
}

type Server struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	Status  Status   `json:"status"`
	Players *Players `json:"players,omitempty"`
	Port    int      `json:"port,omitempty"`
}

type runtimeState struct {
	status      Status
	players     *Players
	stopRunning bool // true only while an async Stop sequence is actively in flight
}

type Executor struct {
	Start     func(srv Server) error
	Broadcast func(srv Server, message string) error
	Stop      func(srv Server) error
	IsStopped func(srv Server) (bool, error)
}

func NoOpExecutor() Executor {
	return Executor{
		Start:     func(Server) error { return nil },
		Broadcast: func(Server, string) error { return nil },
		Stop:      func(Server) error { return nil },
		IsStopped: func(Server) (bool, error) { return true, nil },
	}
}

const (
	defaultGracePeriod = 30 * time.Second
	defaultMaxWait     = 60 * time.Second
	pollInterval       = 500 * time.Millisecond
)

type Manager struct {
	mu          sync.RWMutex
	runtime     map[string]*runtimeState
	executor    Executor
	gracePeriod time.Duration
	maxWait     time.Duration
	pollEvery   time.Duration
}

func NewManager(executor Executor, gracePeriod, maxWait time.Duration) *Manager {
	if executor.Start == nil {
		executor = NoOpExecutor()
	}
	if gracePeriod <= 0 {
		gracePeriod = defaultGracePeriod
	}
	if maxWait <= 0 {
		maxWait = defaultMaxWait
	}
	rt := make(map[string]*runtimeState, len(catalog))
	for _, d := range catalog {
		rt[d.id] = &runtimeState{status: StatusStopped}
	}
	return &Manager{
		runtime:     rt,
		executor:    executor,
		gracePeriod: gracePeriod,
		maxWait:     maxWait,
		pollEvery:   pollInterval,
	}
}

func toServer(d definition, rt *runtimeState) Server {
	return Server{
		ID:      d.id,
		Type:    d.typ,
		Name:    d.name,
		Status:  rt.status,
		Players: rt.players,
		Port:    d.port,
	}
}

func (m *Manager) List() []Server {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Server, 0, len(catalog))
	for _, d := range catalog {
		out = append(out, toServer(d, m.runtime[d.id]))
	}
	return out
}

func (m *Manager) Get(id string) (Server, error) {
	d, ok := findDefinition(id)
	if !ok {
		return Server{}, ErrNotFound
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return toServer(d, m.runtime[id]), nil
}

func (m *Manager) Blocking() []resources.BlockingResource {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []resources.BlockingResource
	for id, rt := range m.runtime {
		if rt.status == StatusRunning && rt.players != nil && rt.players.Current > 0 {
			out = append(out, resources.BlockingResource{Type: "server", ID: id, Players: rt.players.Current})
		}
	}
	return out
}

func (m *Manager) setStatus(id string, status Status) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rt, ok := m.runtime[id]; ok {
		rt.status = status
	}
}

func (m *Manager) setPlayers(id string, players *Players) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rt, ok := m.runtime[id]; ok {
		rt.players = players
	}
}

// --- Wave 2 additions below ---

// Start is synchronous — the frozen spec describes no phased
// algorithm for starting (§10 is explicitly about STOPPING), so
// unlike Stop there is no reason to make this async. Rejects if the
// server is already running/starting/stopping rather than silently
// doing nothing, so a client can't lose track of what actually
// happened to their request.
func (m *Manager) Start(id string) (Server, error) {
	d, ok := findDefinition(id)
	if !ok {
		return Server{}, ErrNotFound
	}

	m.mu.Lock()
	rt := m.runtime[id]
	if rt.status != StatusStopped {
		m.mu.Unlock()
		return Server{}, ErrAlreadyRunning
	}
	rt.status = StatusStarting
	m.mu.Unlock()

	current := toServer(d, rt)
	if err := m.executor.Start(current); err != nil {
		m.setStatus(id, StatusStopped) // failed to start: back to a clean, honest state
		return Server{}, fmt.Errorf("servers: starting %q: %w", id, err)
	}

	m.setStatus(id, StatusRunning)
	return m.mustGet(id), nil
}

// Stop begins the graceful shutdown sequence from spec §10 and
// returns almost immediately — status flips to StatusStopping before
// this function returns, and the actual broadcast/wait/escalate
// sequence runs in a background goroutine. force controls only
// whether the CALLER needed confirmation before calling this at all
// (that's the HTTP layer's job in wave 3, via the busy_resource/force
// pattern already used by modes and system) — it does NOT mean
// "skip straight to SIGKILL." The sequence itself is identical either
// way: broadcast, wait out the grace period, send the graceful stop,
// poll for real termination, and escalate only as an absolute last
// resort if that all fails — never as a shortcut for `force`.
func (m *Manager) Stop(id string) error {
	d, ok := findDefinition(id)
	if !ok {
		return ErrNotFound
	}

	m.mu.Lock()
	rt := m.runtime[id]
	if rt.status == StatusStopped {
		m.mu.Unlock()
		return ErrNotRunning
	}
	if rt.stopRunning {
		m.mu.Unlock()
		return ErrStopInProgress
	}
	rt.status = StatusStopping
	rt.stopRunning = true
	current := toServer(d, rt)
	m.mu.Unlock()

	go m.runStopSequence(id, current)
	return nil
}

// runStopSequence implements spec §10 steps 2-7 exactly:
//  2. broadcast an in-game warning (soft-failure: a broadcast the
//     executor can't deliver should not abort the shutdown itself)
//  3. wait out the grace period
//  4. send the graceful stop command
//  5. poll for the process actually going inactive, up to maxWait
//  6. escalate (Stop again, treated as a last-resort signal by
//     whatever Stage 15's real executor maps it to) only if step 5's
//     polling timed out — logged as an anomaly, per spec wording,
//     which here means leaving status in a state Wave 3 can surface
//     distinctly rather than silently claiming success
//  7. mark stopped
func (m *Manager) runStopSequence(id string, srv Server) {
	defer func() {
		m.mu.Lock()
		if rt, ok := m.runtime[id]; ok {
			rt.stopRunning = false
		}
		m.mu.Unlock()
	}()

	_ = m.executor.Broadcast(srv, "Server shutting down soon")
	time.Sleep(m.gracePeriod)

	if err := m.executor.Stop(srv); err != nil {
		// Could not even issue the graceful stop command. Leave
		// status as "stopping" rather than falsely claiming
		// "stopped" — Wave 3's Get/List will honestly keep showing
		// this as still stopping, which is the truth.
		return
	}

	deadline := time.Now().Add(m.maxWait)
	for time.Now().Before(deadline) {
		stopped, err := m.executor.IsStopped(srv)
		if err == nil && stopped {
			m.mu.Lock()
			if rt, ok := m.runtime[id]; ok {
				rt.status = StatusStopped
				rt.players = nil
			}
			m.mu.Unlock()
			return
		}
		time.Sleep(m.pollEvery)
	}

	// Escalation path: polling never confirmed termination within
	// maxWait. Per spec §10 step 6, this is a genuine anomaly, not
	// silently treated as success — issue one last Stop call as the
	// escalation signal, but leave status as "stopping" rather than
	// lying that it's confirmed stopped. A real SIGKILL-as-absolute-
	// last-resort is Stage 15's concern; today's stub Executor simply
	// can't distinguish "graceful" from "forceful" at all, which is
	// honestly reflected by leaving this ambiguous rather than
	// pretending certainty we don't have.
	_ = m.executor.Stop(srv)
}

func (m *Manager) mustGet(id string) Server {
	srv, _ := m.Get(id)
	return srv
}

// ForceRestartStopSequence bypasses the "already stopping" guard and
// starts a fresh stop sequence even though one is already in flight.
// This exists specifically for the HTTP layer's force=true case
// (server_handlers.go) — it does not change what the sequence itself
// does (still broadcast → grace → stop → poll → escalate-only-as-
// last-resort), only whether a second call is allowed to begin one.
func (m *Manager) ForceRestartStopSequence(id string) error {
	d, ok := findDefinition(id)
	if !ok {
		return ErrNotFound
	}

	m.mu.Lock()
	rt := m.runtime[id]
	if rt.status == StatusStopped {
		m.mu.Unlock()
		return ErrNotRunning
	}
	rt.status = StatusStopping
	rt.stopRunning = true
	current := toServer(d, rt)
	m.mu.Unlock()

	go m.runStopSequence(id, current)
	return nil
}