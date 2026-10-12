package minecraft

import (
	"bytes"
	"context"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

// fakeSLPServer speaks enough of the server list ping protocol for Ping:
// handshake + status request -> JSON status, ping -> pong echo.
func startFakeSLPServer(t *testing.T, statusJSON string) (string, int) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))

		readPacket := func() ([]byte, error) {
			n, err := readVarInt(conn)
			if err != nil {
				return nil, err
			}
			data := make([]byte, n)
			_, err = io.ReadFull(conn, data)
			return data, err
		}
		writePacket := func(payload []byte) error {
			var buf bytes.Buffer
			writeVarInt(&buf, int32(len(payload)))
			buf.Write(payload)
			_, err := conn.Write(buf.Bytes())
			return err
		}

		// Handshake + status request (contents unchecked).
		if _, err := readPacket(); err != nil {
			return
		}
		if _, err := readPacket(); err != nil {
			return
		}

		// Status response: packet ID 0x00 + JSON string.
		var status bytes.Buffer
		writeVarInt(&status, 0x00)
		writeString(&status, statusJSON)
		if err := writePacket(status.Bytes()); err != nil {
			return
		}

		// Ping: packet ID 0x01 + int64 payload; echo it back as pong.
		ping, err := readPacket()
		if err != nil {
			return
		}
		var pong bytes.Buffer
		writeVarInt(&pong, 0x01)
		pong.Write(ping[1:])
		_ = writePacket(pong.Bytes())
	}()

	addr := ln.Addr().String()
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	return host, port
}

func TestSLPPing_EndToEnd(t *testing.T) {
	statusJSON := `{"version":{"name":"1.20.1","protocol":763},` +
		`"players":{"max":20,"online":2,"sample":[{"name":"alice","id":"uuid-1"},{"name":"bob","id":"uuid-2"}]},` +
		`"description":{"text":"Hello "}}`
	host, port := startFakeSLPServer(t, statusJSON)

	client := NewSLPClient(5 * time.Second)
	result, err := client.Ping(context.Background(), host, port, "1.20.1")
	if err != nil {
		t.Fatalf("ping failed: %v", err)
	}
	if result.Version.Name != "1.20.1" || result.Version.Protocol != 763 {
		t.Errorf("version = %+v", result.Version)
	}
	if result.Players.Online != 2 || result.Players.Max != 20 {
		t.Errorf("players = %+v", result.Players)
	}
	if len(result.PlayerNames) != 2 || result.PlayerNames[0] != "alice" {
		t.Errorf("names = %v", result.PlayerNames)
	}
	if result.MOTD != "Hello" {
		t.Errorf("motd = %q", result.MOTD)
	}
	if result.LatencyMs < 0 || result.LatencyMs > 5000 {
		t.Errorf("latency = %d", result.LatencyMs)
	}
}

func TestSLPPing_StringDescription(t *testing.T) {
	host, port := startFakeSLPServer(t, `{"version":{"name":"x","protocol":1},"players":{"max":1,"online":0},"description":"plain motd"}`)

	client := NewSLPClient(5 * time.Second)
	result, err := client.Ping(context.Background(), host, port, "1.20.1")
	if err != nil {
		t.Fatalf("ping failed: %v", err)
	}
	if result.MOTD != "plain motd" {
		t.Errorf("motd = %q", result.MOTD)
	}
}

func TestSLPPing_ConnectionRefused(t *testing.T) {
	client := NewSLPClient(2 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := client.Ping(ctx, "127.0.0.1", 1, "1.20.1"); err == nil {
		t.Error("refused connection succeeded")
	}
}

func TestSLPPing_BadStatusJSON(t *testing.T) {
	host, port := startFakeSLPServer(t, `not json`)

	client := NewSLPClient(5 * time.Second)
	if _, err := client.Ping(context.Background(), host, port, "1.20.1"); err == nil {
		t.Error("invalid JSON accepted")
	}
}
