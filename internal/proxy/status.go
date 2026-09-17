package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"
)

// Deep-sleep MOTDs served from the proxy while the backend uses zero RAM.
// The client never touches Docker on this path (MINE-121, mc-router parity).
const (
	asleepMOTD  = "Server is asleep - join to wake it up!"
	loadingMOTD = "Server is waking up - join to connect!"
)

// statusVersion mirrors the version block of a Server List Ping response.
type statusVersion struct {
	Name     string `json:"name"`
	Protocol int    `json:"protocol"`
}

// statusPlayers mirrors the players block of a Server List Ping response.
type statusPlayers struct {
	Max    int `json:"max"`
	Online int `json:"online"`
}

// statusDescription mirrors a plain-text description component.
type statusDescription struct {
	Text string `json:"text"`
}

// statusPayload is the JSON body of a Status Response packet.
type statusPayload struct {
	Version     statusVersion     `json:"version"`
	Players     statusPlayers     `json:"players"`
	Description statusDescription `json:"description"`
}

// serveStatusResponse answers one server-list ping on an already-accepted
// connection: it consumes the Status Request packet, writes a Status Response
// with the given MOTD, echoes an optional Ping/Pong, then returns (the caller
// closes the connection). The backend is never dialed.
func serveStatusResponse(conn net.Conn, protocolVersion VarInt, motd string) error {
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}

	// Consume the Status Request packet (length + id 0x00, empty body).
	length, err := ReadVarInt(conn)
	if err != nil {
		return fmt.Errorf("failed to read status request length: %w", err)
	}
	if length < 1 || length > 255 {
		return fmt.Errorf("invalid status request length: %d", length)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		return fmt.Errorf("failed to read status request: %w", err)
	}

	body, err := json.Marshal(statusPayload{
		Version:     statusVersion{Name: "carbon-panel", Protocol: int(protocolVersion)},
		Players:     statusPlayers{Max: 20, Online: 0},
		Description: statusDescription{Text: motd},
	})
	if err != nil {
		return fmt.Errorf("failed to marshal status payload: %w", err)
	}

	var out []byte
	buf := appendVarInt(nil, 0x00)             // packet id
	buf = appendVarInt(buf, VarInt(len(body))) // string length
	buf = append(buf, body...)
	out = appendVarInt(out, VarInt(len(buf))) // packet length
	out = append(out, buf...)
	if _, err := conn.Write(out); err != nil {
		return fmt.Errorf("failed to write status response: %w", err)
	}

	// Optional Ping/Pong round: the client may verify latency before
	// disconnecting. Best effort with a short deadline.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	pingLen, err := ReadVarInt(conn)
	if err != nil {
		return nil // client went away; response already delivered
	}
	if pingLen < 1 || pingLen > 32 {
		return nil
	}
	pingData := make([]byte, pingLen)
	if _, err := io.ReadFull(conn, pingData); err != nil {
		return nil
	}
	if len(pingData) < 9 || pingData[0] != 0x01 {
		return nil // not a ping request; nothing more to do
	}
	// Pong: packet id 0x01 + echoed 8-byte payload, length-prefixed.
	pongBody := []byte{0x01}
	pongBody = append(pongBody, pingData[1:9]...)
	pong := appendVarInt(nil, VarInt(len(pongBody)))
	pong = append(pong, pongBody...)
	_, _ = conn.Write(pong)
	return nil
}

// Hold-timeout disconnect messages: a hung-up client sees why instead of a
// bare connection reset (MINE-123, lazymc-Kick parity).
const (
	holdTimeoutMessage = "Server is still waking up - please join again in a few seconds!"
	wakeFailedMessage  = "Server could not wake up - please try again shortly!"
)

// sendLoginDisconnect writes a Login Disconnect packet (id 0x00 + JSON chat)
// on a connection stuck in the login phase.
func sendLoginDisconnect(conn net.Conn, message string) error {
	body, err := json.Marshal(statusDescription{Text: message})
	if err != nil {
		return err
	}

	var pkt []byte
	pkt = appendVarInt(pkt, 0x00) // packet id
	pkt = appendVarInt(pkt, VarInt(len(body)))
	pkt = append(pkt, body...)
	out := appendVarInt(nil, VarInt(len(pkt)))
	out = append(out, pkt...)

	if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	_, err = conn.Write(out)
	return err
}

// appendVarInt encodes v and appends it to b.
func appendVarInt(b []byte, v VarInt) []byte {
	for {
		if (v & ^0x7F) == 0 {
			return append(b, byte(v))
		}
		b = append(b, byte((v&0x7F)|0x80))
		v >>= 7
	}
}

// slpHealthCheck dials host:port and performs a minimal Server List Ping
// round-trip (handshake + status request + status response header). Success
// means the JVM answers protocol traffic — the held client may be relayed.
// protocolVersion is echoed from the waiting client's own handshake.
func slpHealthCheck(host string, port int, protocolVersion VarInt, timeout time.Duration) error {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	deadline := time.Now().Add(timeout)
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}

	// Handshake with NextState=1 (status).
	hs := &HandshakePacket{
		ProtocolVersion: protocolVersion,
		ServerAddress:   "localhost",
		ServerPort:      uint16(port),
		NextState:       1,
	}
	if err := WriteHandshakePacket(conn, hs); err != nil {
		return fmt.Errorf("health check handshake failed: %w", err)
	}

	// Status Request (length 1, id 0x00).
	if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
		return fmt.Errorf("health check status request failed: %w", err)
	}

	// Status Response header: length + id 0x00. Body is not parsed — any
	// well-formed response proves the server is up.
	respLen, err := ReadVarInt(conn)
	if err != nil {
		return fmt.Errorf("health check response length failed: %w", err)
	}
	if respLen < 1 || respLen > 1<<20 {
		return fmt.Errorf("health check invalid response length: %d", respLen)
	}
	resp := make([]byte, respLen)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("health check response read failed: %w", err)
	}
	if len(resp) == 0 || resp[0] != 0x00 {
		return fmt.Errorf("health check unexpected packet id %d", resp[0])
	}
	return nil
}
