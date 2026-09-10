// Package bedrockping implements enough of RakNet's offline ping/pong
// exchange to read a Bedrock Dedicated Server's real, live player
// count. This is genuinely the ONLY status mechanism Bedrock
// implements — verified before writing this, not assumed: unlike
// Java Edition (which supports the separate GameSpy4/UT3 query
// protocol via enable-query=true) or RCON (which Bedrock has no
// equivalent of at all), the same Unconnected Ping every Bedrock
// client sends to populate its own in-game server list is the whole
// interface. There is no richer per-player console to query — this
// is the honest ceiling of what "real stats" means for Bedrock, not
// a partial implementation of something bigger.
package bedrockping

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"
)

// magic is RakNet's fixed offline-message identifier, present in
// every unconnected ping/pong — same 16 bytes on every real Bedrock
// server, not something server-specific.
var magic = []byte{
	0x00, 0xff, 0xff, 0x00, 0xfe, 0xfe, 0xfe, 0xfe,
	0xfd, 0xfd, 0xfd, 0xfd, 0x12, 0x34, 0x56, 0x78,
}

// Status is what a real Unconnected Pong response actually contains
// — deliberately nothing more. Players/MaxPlayers are the only
// fields A6 Core surfaces to the app; MOTD/Version are kept here for
// completeness and easier debugging, not currently exposed further.
type Status struct {
	Edition    string
	MOTD       string
	Version    string
	Players    int
	MaxPlayers int
}

// Ping sends one real Unconnected Ping to addr (e.g. "127.0.0.1:19132")
// and parses the server's Unconnected Pong. Returns an error rather
// than a fabricated zero-player Status if the server doesn't respond
// within timeout, or sends something that doesn't parse as a real
// Bedrock status string — matching this whole project's "don't fake
// a reading" rule (same discipline as telemetry.Temperature() and
// telemetry.LocalNetwork()).
func Ping(addr string, timeout time.Duration) (Status, error) {
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return Status{}, fmt.Errorf("bedrockping: dial %s: %w", addr, err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return Status{}, fmt.Errorf("bedrockping: setting deadline: %w", err)
	}

	var req bytes.Buffer
	req.WriteByte(0x01) // Unconnected Ping
	_ = binary.Write(&req, binary.BigEndian, uint64(time.Now().UnixMilli()))
	req.Write(magic)
	_ = binary.Write(&req, binary.BigEndian, rand.Uint64()) // client GUID, arbitrary

	if _, err := conn.Write(req.Bytes()); err != nil {
		return Status{}, fmt.Errorf("bedrockping: sending ping: %w", err)
	}

	buf := make([]byte, 2048)
	n, err := conn.Read(buf)
	if err != nil {
		return Status{}, fmt.Errorf("bedrockping: no response from %s: %w", addr, err)
	}
	return parsePong(buf[:n])
}

// parsePong decodes an Unconnected Pong: id(1) + timestamp(8) +
// serverGUID(8) + magic(16) + a RakNet string (2-byte big-endian
// length prefix + UTF-8 data). The data field is the real,
// semicolon-delimited status line every Bedrock client parses to
// show a server in its list — see MFDGaming/MinecraftRakNetDocumentation
// for the verified byte layout this follows.
func parsePong(resp []byte) (Status, error) {
	const headerLen = 1 + 8 + 8 + 16 // id + timestamp + serverGUID + magic
	if len(resp) < headerLen+2 {
		return Status{}, fmt.Errorf("bedrockping: response too short (%d bytes)", len(resp))
	}
	if resp[0] != 0x1c {
		return Status{}, fmt.Errorf("bedrockping: unexpected packet id 0x%02x (expected Unconnected Pong 0x1c)", resp[0])
	}

	strLen := int(binary.BigEndian.Uint16(resp[headerLen : headerLen+2]))
	dataStart := headerLen + 2
	if len(resp) < dataStart+strLen {
		return Status{}, fmt.Errorf("bedrockping: truncated status string (want %d bytes, have %d)", strLen, len(resp)-dataStart)
	}
	data := string(resp[dataStart : dataStart+strLen])

	// Real example: MCPE;Dedicated Server;800;1.26.45;3;10;<id>;Bedrock level;Survival;1;19132;19133;
	fields := strings.Split(data, ";")
	if len(fields) < 6 {
		return Status{}, fmt.Errorf("bedrockping: status string has too few fields (%d): %q", len(fields), data)
	}

	players, err := strconv.Atoi(fields[4])
	if err != nil {
		return Status{}, fmt.Errorf("bedrockping: player count field wasn't a number: %q", fields[4])
	}
	maxPlayers, err := strconv.Atoi(fields[5])
	if err != nil {
		return Status{}, fmt.Errorf("bedrockping: max player count field wasn't a number: %q", fields[5])
	}

	return Status{
		Edition:    fields[0],
		MOTD:       fields[1],
		Version:    fields[3],
		Players:    players,
		MaxPlayers: maxPlayers,
	}, nil
}
