package bedrockping

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// startFakeBedrockServer opens a real UDP socket and responds to any
// incoming packet with a real, correctly-framed Unconnected Pong
// containing statusLine — this exercises the actual network read/
// write path in Ping(), not just parsePong() in isolation.
func startFakeBedrockServer(t *testing.T, statusLine string) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("starting fake server: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	go func() {
		buf := make([]byte, 2048)
		for {
			_, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return // socket closed, test is over
			}
			var pong bytes.Buffer
			pong.WriteByte(0x1c)
			_ = binary.Write(&pong, binary.BigEndian, uint64(123456)) // echoed timestamp, unchecked by Ping
			_ = binary.Write(&pong, binary.BigEndian, uint64(987654)) // server GUID, unchecked by Ping
			pong.Write(magic)
			_ = binary.Write(&pong, binary.BigEndian, uint16(len(statusLine)))
			pong.WriteString(statusLine)
			_, _ = conn.WriteTo(pong.Bytes(), addr)
		}
	}()

	return conn.LocalAddr().String()
}

func TestPingParsesRealWorldStatusString(t *testing.T) {
	// Genuine example format from the verified RakNet spec, with a
	// nonzero player count so the "not just always zero" path is
	// actually exercised.
	addr := startFakeBedrockServer(t, "MCPE;Dedicated Server;800;1.26.45;3;10;13253860892328930865;Bedrock level;Survival;1;19132;19133;")

	status, err := Ping(addr, 2*time.Second)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if status.Players != 3 {
		t.Errorf("expected 3 players, got %d", status.Players)
	}
	if status.MaxPlayers != 10 {
		t.Errorf("expected max 10 players, got %d", status.MaxPlayers)
	}
	if status.Edition != "MCPE" {
		t.Errorf("expected edition MCPE, got %q", status.Edition)
	}
	if status.Version != "1.26.45" {
		t.Errorf("expected version 1.26.45, got %q", status.Version)
	}
}

func TestPingParsesZeroPlayers(t *testing.T) {
	// The exact real-world case that matters most for this feature:
	// an empty server should genuinely read as 0, not be confused
	// with "couldn't reach it" (which is a separate error return).
	addr := startFakeBedrockServer(t, "MCPE;Dedicated Server;800;1.26.45;0;10;13253860892328930865;Bedrock level;Survival;1;19132;19133;")

	status, err := Ping(addr, 2*time.Second)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if status.Players != 0 {
		t.Errorf("expected 0 players, got %d", status.Players)
	}
}

func TestPingTimesOutWhenNothingResponds(t *testing.T) {
	// A real closed UDP socket -- nothing listening at all -- proves
	// this returns an error rather than a fabricated zero-player
	// status when the server genuinely can't be reached.
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	addr := conn.LocalAddr().String()
	conn.Close() // nothing is listening here anymore

	_, err = Ping(addr, 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected an error when nothing responds, got nil")
	}
}

func TestParsePongRejectsWrongPacketID(t *testing.T) {
	bad := make([]byte, 40)
	bad[0] = 0x99 // not 0x1c
	if _, err := parsePong(bad); err == nil {
		t.Fatal("expected an error for a non-Pong packet id, got nil")
	}
}

func TestParsePongRejectsTooFewFields(t *testing.T) {
	var pong bytes.Buffer
	pong.WriteByte(0x1c)
	_ = binary.Write(&pong, binary.BigEndian, uint64(0))
	_ = binary.Write(&pong, binary.BigEndian, uint64(0))
	pong.Write(magic)
	statusLine := "MCPE;not enough fields"
	_ = binary.Write(&pong, binary.BigEndian, uint16(len(statusLine)))
	pong.WriteString(statusLine)

	if _, err := parsePong(pong.Bytes()); err == nil {
		t.Fatal("expected an error for too few semicolon fields, got nil")
	}
}
