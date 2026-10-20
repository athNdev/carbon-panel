package proxy

import (
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHandshakeRateLimitSharesStatusAndLogin(t *testing.T) {
	backend := acceptBackend(t)
	defer backend.Close()

	proxy := NewMinecraftProxy(&Config{
		ListenAddr:     "127.0.0.1:0",
		LoginRateLimit: time.Hour,
	})
	require.NoError(t, proxy.Start())
	defer proxy.Stop()

	proxy.AddRoute("srv", "play.example", "127.0.0.1", backend.Addr().(*net.TCPAddr).Port)
	addr := proxy.listener.Addr().String()

	sendHandshake(t, addr, 1)
	require.Eventually(t, func() bool { return backend.accepted.Load() == 1 }, 5*time.Second, 10*time.Millisecond)

	sendHandshake(t, addr, 2)
	time.Sleep(300 * time.Millisecond)
	if got := backend.accepted.Load(); got != 1 {
		t.Fatalf("login after a status ping dialed the backend (%d accepts)", got)
	}
}

func TestHandshakeRateLimitExpires(t *testing.T) {
	backend := acceptBackend(t)
	defer backend.Close()

	proxy := NewMinecraftProxy(&Config{
		ListenAddr:     "127.0.0.1:0",
		LoginRateLimit: 40 * time.Millisecond,
	})
	require.NoError(t, proxy.Start())
	defer proxy.Stop()

	proxy.AddRoute("srv", "play.example", "127.0.0.1", backend.Addr().(*net.TCPAddr).Port)
	addr := proxy.listener.Addr().String()

	sendHandshake(t, addr, 1)
	require.Eventually(t, func() bool { return backend.accepted.Load() == 1 }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	sendHandshake(t, addr, 2)
	require.Eventually(t, func() bool { return backend.accepted.Load() == 2 }, 5*time.Second, 10*time.Millisecond)
}

type countingBackend struct {
	net.Listener
	accepted atomic.Int32
}

func acceptBackend(t *testing.T) *countingBackend {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	b := &countingBackend{Listener: ln}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			b.accepted.Add(1)
			go func(c net.Conn) {
				defer c.Close()
				pkt, err := ReadHandshakePacket(c)
				if err == nil && pkt != nil {
					_ = WriteHandshakePacket(c, pkt)
				}
			}(conn)
		}
	}()
	return b
}

func sendHandshake(t *testing.T, addr string, nextState int) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	err = WriteHandshakePacket(conn, &HandshakePacket{
		ProtocolVersion: 763,
		ServerAddress:   "play.example",
		ServerPort:      25565,
		NextState:       VarInt(nextState),
	})
	if err != nil {
		t.Fatal(err)
	}
}
