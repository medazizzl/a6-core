package events

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"a6core/internal/state"
)

func fakeValidator(validKey string, device state.Device) Validator {
	return func(key string) (state.Device, bool) {
		if key == validKey {
			return device, true
		}
		return state.Device{}, false
	}
}

func testDevice() state.Device {
	return state.Device{ID: "test-device-id", Name: "Test Device"}
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

func TestConnectRequiresValidToken(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(Handler(hub, fakeValidator("good-key", testDevice())))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, wsURL(srv.URL)+"?token=wrong-key", nil)
	if err == nil {
		t.Fatal("expected dial to fail with an invalid token")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		status := -1
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("expected HTTP 401, got %d", status)
	}
}

func TestConnectSendsConnectedEvent(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(Handler(hub, fakeValidator("good-key", testDevice())))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL)+"?token=good-key", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Event != "connected" {
		t.Fatalf("expected event %q, got %q", "connected", got.Event)
	}
	dataMap, ok := got.Data.(map[string]any)
	if !ok || dataMap["api_version"] != "1.0" {
		t.Fatalf("unexpected connected payload: %+v", got.Data)
	}
}

func TestPublishReachesConnectedClient(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(Handler(hub, fakeValidator("good-key", testDevice())))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL)+"?token=good-key", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("reading connected event: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	hub.Publish(Event{Event: "mode.changed", Data: map[string]string{"from": "tv", "to": "server"}})

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Event != "mode.changed" {
		t.Fatalf("expected mode.changed, got %q", got.Event)
	}
}

func TestMultipleClientsAllReceiveBroadcast(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(Handler(hub, fakeValidator("good-key", testDevice())))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var conns []*websocket.Conn
	for i := 0; i < 3; i++ {
		conn, _, err := websocket.Dial(ctx, wsURL(srv.URL)+"?token=good-key", nil)
		if err != nil {
			t.Fatalf("Dial %d: %v", i, err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		if _, _, err := conn.Read(ctx); err != nil {
			t.Fatalf("reading connected event %d: %v", i, err)
		}
		conns = append(conns, conn)
	}

	time.Sleep(20 * time.Millisecond)
	hub.Publish(Event{Event: "system.warning", Data: map[string]string{"message": "test"}})

	for i, conn := range conns {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("client %d Read: %v", i, err)
		}
		var got Event
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("client %d unmarshal: %v", i, err)
		}
		if got.Event != "system.warning" {
			t.Fatalf("client %d: expected system.warning, got %q", i, got.Event)
		}
	}
}

func TestPongKeepsConnectionAlive(t *testing.T) {
	hub := NewHubWithIntervals(20*time.Millisecond, 200*time.Millisecond)
	srv := httptest.NewServer(Handler(hub, fakeValidator("good-key", testDevice())))
	defer srv.Close()
	go hub.Run(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL)+"?token=good-key", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("reading connected event: %v", err)
	}

	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		readCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		_, data, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			continue
		}
		var got Event
		if json.Unmarshal(data, &got) == nil && got.Event == "ping" {
			pong, _ := json.Marshal(Event{Event: "pong"})
			_ = conn.Write(ctx, websocket.MessageText, pong)
		}
	}

	pingCtx, cancel2 := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel2()
	if _, _, err := conn.Read(pingCtx); err != nil && err != context.DeadlineExceeded {
		t.Fatalf("connection appears closed when it should still be alive: %v", err)
	}
}

func TestStaleConnectionClosedWithoutPong(t *testing.T) {
	hub := NewHubWithIntervals(20*time.Millisecond, 60*time.Millisecond)
	srv := httptest.NewServer(Handler(hub, fakeValidator("good-key", testDevice())))
	defer srv.Close()
	go hub.Run(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL)+"?token=good-key", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("reading connected event: %v", err)
	}

	deadline := time.Now().Add(250 * time.Millisecond)
	closed := false
	for time.Now().Before(deadline) {
		readCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
		_, _, err := conn.Read(readCtx)
		cancel()
		if err != nil && err != context.DeadlineExceeded {
			closed = true
			break
		}
	}
	if !closed {
		t.Fatal("expected the server to close a stale connection that never sent a pong")
	}
}