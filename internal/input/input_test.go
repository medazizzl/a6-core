package input

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"a6core/internal/state"
)

type fakeInjector struct {
	mu          sync.Mutex
	keyDowns    []string
	keyUps      []string
	moves       [][2]int32
	buttonDowns []string
	buttonUps   []string
	scrolls     []int32
}

func (f *fakeInjector) KeyDown(code string) error {
	if code == "TriggerError" {
		return &unknownFieldError{field: "code", value: code}
	}
	f.mu.Lock()
	f.keyDowns = append(f.keyDowns, code)
	f.mu.Unlock()
	return nil
}
func (f *fakeInjector) KeyUp(code string) error {
	f.mu.Lock()
	f.keyUps = append(f.keyUps, code)
	f.mu.Unlock()
	return nil
}
func (f *fakeInjector) MouseMove(dx, dy int32) error {
	f.mu.Lock()
	f.moves = append(f.moves, [2]int32{dx, dy})
	f.mu.Unlock()
	return nil
}
func (f *fakeInjector) MouseButtonDown(button string) error {
	f.mu.Lock()
	f.buttonDowns = append(f.buttonDowns, button)
	f.mu.Unlock()
	return nil
}
func (f *fakeInjector) MouseButtonUp(button string) error {
	f.mu.Lock()
	f.buttonUps = append(f.buttonUps, button)
	f.mu.Unlock()
	return nil
}
func (f *fakeInjector) Scroll(dy int32) error {
	f.mu.Lock()
	f.scrolls = append(f.scrolls, dy)
	f.mu.Unlock()
	return nil
}

func testDevice(name string) state.Device {
	return state.Device{ID: "dev-" + name, Name: name}
}

func fakeValidator(known map[string]state.Device) Validator {
	return func(key string) (state.Device, bool) {
		d, ok := known[key]
		return d, ok
	}
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

func readOne(t *testing.T, conn *websocket.Conn, ctx context.Context) outboundEvent {
	t.Helper()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var ev outboundEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return ev
}

func TestInputRequiresValidToken(t *testing.T) {
	fi := &fakeInjector{}
	mgr := NewManager(fi)
	srv := httptest.NewServer(Handler(mgr, fakeValidator(map[string]state.Device{"good": testDevice("phone")})))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, wsURL(srv.URL)+"?token=wrong", nil)
	if err == nil {
		t.Fatal("expected dial to fail with an invalid token")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %v", resp)
	}
}

func TestFirstConnectionGetsControlGranted(t *testing.T) {
	fi := &fakeInjector{}
	mgr := NewManager(fi)
	srv := httptest.NewServer(Handler(mgr, fakeValidator(map[string]state.Device{"good": testDevice("phone")})))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL)+"?token=good", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ev := readOne(t, conn, ctx)
	if ev.Event != "control_granted" {
		t.Fatalf("expected control_granted, got %q", ev.Event)
	}
}

func TestSecondConnectionTakesControlFirstGetsNotified(t *testing.T) {
	fi := &fakeInjector{}
	mgr := NewManager(fi)
	known := map[string]state.Device{
		"first":  testDevice("First Phone"),
		"second": testDevice("Second Phone"),
	}
	srv := httptest.NewServer(Handler(mgr, fakeValidator(known)))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	first, _, err := websocket.Dial(ctx, wsURL(srv.URL)+"?token=first", nil)
	if err != nil {
		t.Fatalf("first Dial: %v", err)
	}
	defer first.Close(websocket.StatusNormalClosure, "")
	if ev := readOne(t, first, ctx); ev.Event != "control_granted" {
		t.Fatalf("expected first connection control_granted, got %q", ev.Event)
	}

	second, _, err := websocket.Dial(ctx, wsURL(srv.URL)+"?token=second", nil)
	if err != nil {
		t.Fatalf("second Dial: %v", err)
	}
	defer second.Close(websocket.StatusNormalClosure, "")
	if ev := readOne(t, second, ctx); ev.Event != "control_granted" {
		t.Fatalf("expected second connection control_granted, got %q", ev.Event)
	}

	if ev := readOne(t, first, ctx); ev.Event != "control_taken" {
		t.Fatalf("expected first connection to receive control_taken, got %q", ev.Event)
	}
}

func TestKeyEventDispatchedToInjector(t *testing.T) {
	fi := &fakeInjector{}
	mgr := NewManager(fi)
	srv := httptest.NewServer(Handler(mgr, fakeValidator(map[string]state.Device{"good": testDevice("phone")})))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL)+"?token=good", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	readOne(t, conn, ctx)

	msg := `{"type":"key","code":"ArrowUp","action":"down"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		fi.mu.Lock()
		got := len(fi.keyDowns)
		fi.mu.Unlock()
		if got == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected exactly one KeyDown call to reach the injector")
}

func TestPointerMoveAccumulatesFractionalDeltas(t *testing.T) {
	fi := &fakeInjector{}
	mgr := NewManager(fi)
	srv := httptest.NewServer(Handler(mgr, fakeValidator(map[string]state.Device{"good": testDevice("phone")})))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL)+"?token=good", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	readOne(t, conn, ctx)

	for i := 0; i < 5; i++ {
		msg := `{"type":"pointer_move","dx":0.4,"dy":0}`
		if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		fi.mu.Lock()
		moves := append([][2]int32(nil), fi.moves...)
		fi.mu.Unlock()
		var totalX int32
		for _, m := range moves {
			totalX += m[0]
		}
		if totalX >= 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected accumulated fractional pointer_move deltas to eventually inject real motion")
}

func TestPointerScrollDispatch(t *testing.T) {
	fi := &fakeInjector{}
	mgr := NewManager(fi)
	srv := httptest.NewServer(Handler(mgr, fakeValidator(map[string]state.Device{"good": testDevice("phone")})))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL)+"?token=good", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	readOne(t, conn, ctx)

	msg := `{"type":"pointer_scroll","dx":0,"dy":-5}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		fi.mu.Lock()
		got := len(fi.scrolls)
		fi.mu.Unlock()
		if got == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected exactly one Scroll call to reach the injector")
}

func TestMalformedJSONReturnsErrorButKeepsConnectionOpen(t *testing.T) {
	fi := &fakeInjector{}
	mgr := NewManager(fi)
	srv := httptest.NewServer(Handler(mgr, fakeValidator(map[string]state.Device{"good": testDevice("phone")})))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL)+"?token=good", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	readOne(t, conn, ctx)

	if err := conn.Write(ctx, websocket.MessageText, []byte("not valid json")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	ev := readOne(t, conn, ctx)
	if ev.Event != "error" {
		t.Fatalf("expected an error event for malformed JSON, got %q", ev.Event)
	}

	valid := `{"type":"key","code":"KeyA","action":"down"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(valid)); err != nil {
		t.Fatalf("Write after malformed message: %v", err)
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		fi.mu.Lock()
		got := len(fi.keyDowns)
		fi.mu.Unlock()
		if got == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("connection should still process valid messages after an earlier malformed one")
}

func TestUnknownKeyCodeReturnsErrorEvent(t *testing.T) {
	fi := &fakeInjector{}
	mgr := NewManager(fi)
	srv := httptest.NewServer(Handler(mgr, fakeValidator(map[string]state.Device{"good": testDevice("phone")})))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL)+"?token=good", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	readOne(t, conn, ctx)

	msg := `{"type":"key","code":"TriggerError","action":"down"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	ev := readOne(t, conn, ctx)
	if ev.Event != "error" {
		t.Fatalf("expected an error event for an injector failure, got %q", ev.Event)
	}
}
