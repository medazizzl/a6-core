// Package input implements the /v1/input WebSocket handler from the
// Stage 17 blueprint §3-4: a dedicated, one-owner-at-a-time transport
// for phone-as-remote control, separate from the broadcast-oriented
// /v1/events. Ownership is last-connected-wins; the pre-empted
// connection receives control_taken, the new one receives
// control_granted (blueprint v1.1 addition).
package input

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"sync"

	"github.com/coder/websocket"

	"a6core/internal/auth"
	"a6core/internal/state"
)

// Injector is the seam between this package's transport/ownership
// logic and whatever actually moves the mouse or presses a key.
// internal/inputbridge.Device already satisfies this exactly, with
// no wrapper needed. Tests use a fake, so this package's own suite
// never touches /dev/uinput.
type Injector interface {
	KeyDown(code string) error
	KeyUp(code string) error
	MouseMove(dx, dy int32) error
	MouseButtonDown(button string) error
	MouseButtonUp(button string) error
	Scroll(dy int32) error
}

// Validator mirrors auth.ValidateDeviceKey's signature, same pattern
// events.Handler already uses — main.go wires the real one in later.
type Validator func(key string) (state.Device, bool)

type inboundEnvelope struct {
	Type   string  `json:"type"`
	Code   string  `json:"code,omitempty"`
	Action string  `json:"action,omitempty"`
	DX     float64 `json:"dx,omitempty"`
	DY     float64 `json:"dy,omitempty"`
	Button string  `json:"button,omitempty"`
}

type outboundEvent struct {
	Event string `json:"event"`
	Data  any    `json:"data,omitempty"`
}

func marshalOutbound(e outboundEvent) []byte {
	b, err := json.Marshal(e)
	if err != nil {
		log.Printf("input: BUG could not marshal outbound event %q: %v", e.Event, err)
		return nil
	}
	return b
}

const sendBufferSize = 16 // same rationale as events.Client: this hardware can't afford deep queues

// controller is one connected phone. accumX/accumY hold the
// fractional remainder of pointer_move deltas between messages —
// see package docs on why truncating each delta individually would
// silently drop most small motions.
//
// conn is this controller's OWN websocket connection. It must never
// be passed in from outside — the original Wave 2 bug was exactly
// that: the eviction call site closed the wrong connection because
// it had no way to know which conn actually belonged to the
// controller being evicted. Storing it here removes that entire
// class of mistake.
type controller struct {
	device    state.Device
	conn      *websocket.Conn
	send      chan []byte
	closed    chan struct{}
	closeOnce sync.Once
	accumX    float64
	accumY    float64
}

func (c *controller) enqueue(msg []byte) {
	if msg == nil {
		return
	}
	select {
	case c.send <- msg:
	default:
		log.Printf("input: dropping message for a slow client (device %s)", c.device.ID)
	}
}

// close signals this controller's writeLoop to shut down. It does
// NOT close the network connection directly — writeLoop owns that,
// and only after it has drained any already-enqueued messages (like
// control_taken), so a pending notification is never lost to a race
// between the close signal and the send channel.
func (c *controller) close() {
	c.closeOnce.Do(func() {
		close(c.closed)
	})
}

// Manager owns the single active controller. Guarded by its own
// mutex — separate from any other package's locking, same pattern as
// every other piece of live, non-persisted state in this project
// (apps.Manager, servers.Manager).
type Manager struct {
	mu       sync.Mutex
	injector Injector
	current  *controller
}

func NewManager(injector Injector) *Manager {
	return &Manager{injector: injector}
}

// takeControl installs newCtrl as the current controller, evicting
// and notifying whatever was there before. Returns the evicted
// controller (or nil) so the caller can close its connection AFTER
// releasing the lock — closing a websocket while holding this mutex
// would risk blocking every other request on a slow network write.
func (m *Manager) takeControl(newCtrl *controller) (evicted *controller) {
	m.mu.Lock()
	defer m.mu.Unlock()
	evicted = m.current
	m.current = newCtrl
	return evicted
}

func (m *Manager) releaseIfCurrent(ctrl *controller) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == ctrl {
		m.current = nil
	}
}

// Handler returns the GET /v1/input HTTP handler.
func Handler(mgr *Manager, validate Validator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := extractKeyForInput(r)
		device, ok := validate(key)
		if !ok {
			http.Error(w, `{"error":"invalid_token","message":"device key not recognized or revoked"}`, http.StatusUnauthorized)
			return
		}

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			log.Printf("input: upgrade failed: %v", err)
			return
		}

		ctrl := &controller{
			device: device,
			conn:   conn,
			send:   make(chan []byte, sendBufferSize),
			closed: make(chan struct{}),
		}

		evicted := mgr.takeControl(ctrl)
		if evicted != nil {
			evicted.enqueue(marshalOutbound(outboundEvent{Event: "control_taken", Data: map[string]string{
				"taken_by": device.Name,
			}}))
		}
		defer mgr.releaseIfCurrent(ctrl)

		go writeLoop(ctrl)
		if evicted != nil {
			evicted.close()
		}

		ctrl.enqueue(marshalOutbound(outboundEvent{Event: "control_granted"}))

		readLoop(r.Context(), mgr.injector, ctrl)
	}
}

// writeLoop is the ONLY goroutine that writes to conn — same
// single-writer requirement events.go documented, for the same
// reason: concurrent writes to one WebSocket connection aren't safe.
// It also owns actually closing the connection, so signaling close
// and the final drain-and-close always happen in the same goroutine,
// in the right order.
func writeLoop(ctrl *controller) {
	ctx := context.Background()
	for {
		select {
		case <-ctrl.closed:
			drainPending(ctx, ctrl)
			ctrl.conn.Close(websocket.StatusNormalClosure, "")
			return
		case msg := <-ctrl.send:
			if err := ctrl.conn.Write(ctx, websocket.MessageText, msg); err != nil {
				ctrl.close()
				return
			}
		}
	}
}

// drainPending flushes any messages already sitting in the send
// buffer (e.g. a control_taken notification enqueued right before
// close was called) before the connection actually closes. This is
// the fix for the original select-statement race: without it,
// whether the notification actually got sent depended on which
// select case fired first.
func drainPending(ctx context.Context, ctrl *controller) {
	for {
		select {
		case msg := <-ctrl.send:
			_ = ctrl.conn.Write(ctx, websocket.MessageText, msg)
		default:
			return
		}
	}
}

func readLoop(ctx context.Context, injector Injector, ctrl *controller) {
	defer ctrl.close()
	for {
		_, data, err := ctrl.conn.Read(ctx)
		if err != nil {
			return
		}
		var env inboundEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			ctrl.enqueue(marshalOutbound(outboundEvent{Event: "error", Data: map[string]string{
				"code": "invalid_json", "message": err.Error(),
			}}))
			continue
		}
		dispatch(injector, ctrl, env)
	}
}

// dispatch translates one decoded message into an Injector call.
// Any resulting error (e.g. keymap.go's ErrUnknownKeyCode) is
// reported back to the client as an error event rather than
// silently dropped — matches Wave 1's "loud, not silent" stance on
// unmapped keys.
func dispatch(injector Injector, ctrl *controller, env inboundEnvelope) {
	var err error
	switch env.Type {
	case "key":
		switch env.Action {
		case "down":
			err = injector.KeyDown(env.Code)
		case "up":
			err = injector.KeyUp(env.Code)
		default:
			err = errUnknownAction(env.Action)
		}
	case "pointer_move":
		dispatchPointerMove(injector, ctrl, env.DX, env.DY)
		return
	case "pointer_button":
		switch env.Action {
		case "down":
			err = injector.MouseButtonDown(env.Button)
		case "up":
			err = injector.MouseButtonUp(env.Button)
		default:
			err = errUnknownAction(env.Action)
		}
	case "pointer_scroll":
		// NOTE: horizontal scroll (env.DX) is accepted by the schema
		// but internal/inputbridge.Device (Wave 1) only exposes
		// vertical Scroll — a known, explicit gap, not a silent drop.
		err = injector.Scroll(int32(env.DY))
	default:
		err = errUnknownType(env.Type)
	}

	if err != nil {
		ctrl.enqueue(marshalOutbound(outboundEvent{Event: "error", Data: map[string]string{
			"code": "dispatch_failed", "message": err.Error(),
		}}))
	}
}

// dispatchPointerMove implements the fractional-accumulator design:
// each delta adds to a running remainder; only the whole-pixel part
// is ever injected, and the leftover fraction carries to the next
// message. Without this, a stream of small sub-1.0 deltas (a real,
// common shape for touch-drag gestures) would truncate to zero every
// single time and the touchpad would feel unresponsive or dead.
func dispatchPointerMove(injector Injector, ctrl *controller, dx, dy float64) {
	ctrl.accumX += dx
	ctrl.accumY += dy

	wholeX := math.Trunc(ctrl.accumX)
	wholeY := math.Trunc(ctrl.accumY)

	if wholeX == 0 && wholeY == 0 {
		return
	}

	ctrl.accumX -= wholeX
	ctrl.accumY -= wholeY

	if err := injector.MouseMove(int32(wholeX), int32(wholeY)); err != nil {
		ctrl.enqueue(marshalOutbound(outboundEvent{Event: "error", Data: map[string]string{
			"code": "dispatch_failed", "message": err.Error(),
		}}))
	}
}

func errUnknownAction(action string) error {
	return &unknownFieldError{field: "action", value: action}
}
func errUnknownType(t string) error {
	return &unknownFieldError{field: "type", value: t}
}

type unknownFieldError struct {
	field, value string
}

func (e *unknownFieldError) Error() string {
	return "input: unrecognized " + e.field + " " + `"` + e.value + `"`
}

// extractKeyForInput reuses the same shared extraction logic
// /v1/events now delegates to as well — see internal/auth's
// ExtractDeviceKey.
func extractKeyForInput(r *http.Request) string {
	return auth.ExtractDeviceKey(r)
}
