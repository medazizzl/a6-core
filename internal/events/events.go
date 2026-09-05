// Package events implements the WebSocket event stream from the
// frozen spec §7: GET /v1/events, connection auth, the app-level
// ping/pong heartbeat, and broadcast of discrete events to every
// connected, authenticated client.
package events

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"a6core/internal/auth"
	"a6core/internal/state"
)

const (
	defaultPingInterval = 30 * time.Second
	defaultStaleAfter   = 60 * time.Second
	sendBufferSize      = 16 // small on purpose: this hardware can't afford deep per-client queues
)

// Event is the single envelope every message uses, matching the
// frozen spec's connected/status.tick examples exactly.
type Event struct {
	Event string `json:"event"`
	Data  any    `json:"data,omitempty"`
}

func marshalEvent(e Event) []byte {
	b, err := json.Marshal(e)
	if err != nil {
		log.Printf("events: BUG could not marshal event %q: %v", e.Event, err)
		return nil
	}
	return b
}

// Validator mirrors auth.ValidateDeviceKey's signature exactly.
// Wave 4 wires the real one in; tests here use a fake.
type Validator func(key string) (state.Device, bool)

type Client struct {
	conn      *websocket.Conn
	device    state.Device
	send      chan []byte
	lastPong  time.Time
	mu        sync.Mutex // guards lastPong only
	closed    chan struct{}
	closeOnce sync.Once
}

func (c *Client) close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.conn.Close(websocket.StatusNormalClosure, "")
	})
}

func (c *Client) touchPong() {
	c.mu.Lock()
	c.lastPong = time.Now()
	c.mu.Unlock()
}

func (c *Client) sinceLastPong() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Since(c.lastPong)
}

// enqueue is a non-blocking send: a client that can't keep up gets a
// dropped message, never a stalled hub.
func (c *Client) enqueue(msg []byte) {
	if msg == nil {
		return
	}
	select {
	case c.send <- msg:
	default:
		log.Printf("events: dropping message for a slow client (device %s)", c.device.ID)
	}
}

type Hub struct {
	mu         sync.RWMutex
	clients    map[*Client]struct{}
	pingEvery  time.Duration
	staleAfter time.Duration
}

func NewHub() *Hub {
	return NewHubWithIntervals(defaultPingInterval, defaultStaleAfter)
}

// NewHubWithIntervals exists so tests can use short, fast intervals
// without changing the real heartbeat logic under test — same
// injectable-duration pattern as servers.go's grace/maxWait.
func NewHubWithIntervals(pingEvery, staleAfter time.Duration) *Hub {
	return &Hub{
		clients:    make(map[*Client]struct{}),
		pingEvery:  pingEvery,
		staleAfter: staleAfter,
	}
}

func (h *Hub) register(c *Client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

// Publish broadcasts event to every currently connected client.
func (h *Hub) Publish(event Event) {
	msg := marshalEvent(event)
	if msg == nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		c.enqueue(msg)
	}
}

func (h *Hub) checkHeartbeats() {
	ping := marshalEvent(Event{Event: "ping"})

	h.mu.RLock()
	var stale []*Client
	for c := range h.clients {
		if c.sinceLastPong() > h.staleAfter {
			stale = append(stale, c)
			continue
		}
		c.enqueue(ping)
	}
	h.mu.RUnlock()

	for _, c := range stale {
		log.Printf("events: closing stale connection (device %s, no pong within %s)", c.device.ID, h.staleAfter)
		c.close()
	}
}

// Run drives the heartbeat ticker until ctx is cancelled. Started as
// its own goroutine from main.go (Wave 4), tied to the same shutdown
// context as everything else, so it stops cleanly on Ctrl+C rather
// than leaking.
func (h *Hub) Run(ctx context.Context) {
	ticker := time.NewTicker(h.pingEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.checkHeartbeats()
		}
	}
}

// Handler returns the GET /v1/events HTTP handler. Note: Accept's
// default same-origin check only applies when a request carries an
// Origin header at all — a native mobile client or a plain Go
// WebSocket dial (as used in this package's own tests) sends none,
// so this needs no special options for this project's real clients.
func Handler(hub *Hub, validate Validator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := auth.ExtractDeviceKey(r)
		device, ok := validate(key)
		if !ok {
			http.Error(w, `{"error":"invalid_token","message":"device key not recognized or revoked"}`, http.StatusUnauthorized)
			return
		}

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			log.Printf("events: upgrade failed: %v", err)
			return
		}

		client := &Client{
			conn:     conn,
			device:   device,
			send:     make(chan []byte, sendBufferSize),
			lastPong: time.Now(),
			closed:   make(chan struct{}),
		}
		hub.register(client)
		defer hub.unregister(client)

		client.enqueue(marshalEvent(Event{Event: "connected", Data: map[string]string{"api_version": "1.0"}}))

		go readLoop(client)
		writeLoop(client)
	}
}

// readLoop only cares about {"event":"pong"} — the application-level
// heartbeat reply. Anything else is ignored; any read error
// (including a client-initiated close) ends the connection.
func readLoop(c *Client) {
	defer c.close()
	ctx := context.Background()
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		var msg Event
		if json.Unmarshal(data, &msg) == nil && msg.Event == "pong" {
			c.touchPong()
		}
	}
}

// writeLoop is the ONLY goroutine that ever writes to c.conn —
// required because concurrent writes to one WebSocket connection
// aren't safe. Every outbound message funnels through c.send.
func writeLoop(c *Client) {
	ctx := context.Background()
	for {
		select {
		case <-c.closed:
			return
		case msg := <-c.send:
			if err := c.conn.Write(ctx, websocket.MessageText, msg); err != nil {
				c.close()
				return
			}
		}
	}
}

// --- Wave 3 additions ---

// StatusProvider supplies the live numbers for status.tick, kept as
// a plain function type (not a direct internal/telemetry dependency)
// so this package doesn't need to import telemetry at all — main.go
// (Wave 4) supplies the real implementation as a closure, same
// stubbed-seam pattern as every other stubbed-seam in this project.
type StatusProvider func() StatusTick

// StatusTick matches the frozen spec's Status schema (§8), minus
// `network` — a known, explicit gap, not an oversight (see Wave 3
// design notes). Fields telemetry marks "not ready" or "unknown"
// (cpu_percent before two samples exist, temperature_c on hardware
// with no confirmed sensor) are omitted via omitempty rather than
// sent as a misleading zero.
type StatusTick struct {
	Mode          string  `json:"mode"`
	UptimeSeconds uint64  `json:"uptime_seconds"`
	CPUPercent    float64 `json:"cpu_percent,omitempty"`
	RAMUsedMB     uint64  `json:"ram_used_mb"`
	RAMTotalMB    uint64  `json:"ram_total_mb"`
	SwapUsedMB    uint64  `json:"swap_used_mb"`
	TemperatureC  float64 `json:"temperature_c,omitempty"`
	DiskFreeGB    uint64  `json:"disk_free_gb"`
	Timestamp     string  `json:"timestamp"`
}

const defaultStatusTickInterval = 5 * time.Second

// RunStatusTicks publishes a status.tick event on a fixed interval
// until ctx is cancelled — one shared tick for every connected
// client, independent of individual heartbeats. Started as its own
// goroutine from main.go, alongside Hub.Run and telemetry.Sampler.Run.
func (h *Hub) RunStatusTicks(ctx context.Context, provider StatusProvider, interval time.Duration) {
	if interval <= 0 {
		interval = defaultStatusTickInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.Publish(Event{Event: "status.tick", Data: provider()})
		}
	}
}