package proxy

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMinecraftProxy_HibernationWake(t *testing.T) {
	// Start a mock backend server
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer backendListener.Close()

	backendPort := backendListener.Addr().(*net.TCPAddr).Port

	var backendAccepted atomic.Bool
	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		backendAccepted.Store(true)

		// Read handshake packet
		pkt, err := ReadHandshakePacket(conn)
		if err == nil && pkt != nil {
			_ = WriteHandshakePacket(conn, pkt)
		}
	}()

	var wakeCalled atomic.Bool
	wakeHandler := func(ctx context.Context, serverID string) error {
		if serverID == "srv-sleep" {
			wakeCalled.Store(true)
		}
		return nil
	}

	proxy := NewMinecraftProxy(&Config{
		ListenAddr:  "127.0.0.1:0",
		WakeHandler: wakeHandler,
	})
	err = proxy.Start()
	require.NoError(t, err)
	defer proxy.Stop()

	// Wait for listener to bind
	time.Sleep(50 * time.Millisecond)

	proxy.AddRoute("srv-sleep", "sleep.test.local", "127.0.0.1", backendPort)
	proxy.SetRouteHibernated("sleep.test.local", true)

	// Verify route is initially hibernated
	routes := proxy.GetRoutes()
	require.True(t, routes["sleep.test.local"].Hibernated)

	// Connect client to proxy
	proxyAddr := proxy.listener.Addr().String()
	clientConn, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	require.NoError(t, err)
	defer clientConn.Close()

	// Build and write a Minecraft handshake packet with login intent.
	// NOTE (MINE-118): status pings (NextState=1) must NOT wake a hibernated
	// server — only real logins (NextState=2) may. Waking on any handshake
	// is the pause/knock loop from the DiscoPanel report.
	handshake := &HandshakePacket{
		ProtocolVersion: 763, // 1.20.1
		ServerAddress:   "sleep.test.local",
		ServerPort:      25565,
		NextState:       2, // Login
	}
	err = WriteHandshakePacket(clientConn, handshake)
	require.NoError(t, err)

	// Wait for proxy to handle handshake and wake server
	time.Sleep(200 * time.Millisecond)

	assert.True(t, wakeCalled.Load(), "WakeHandler must be invoked for hibernated server")
	assert.True(t, backendAccepted.Load(), "Backend must accept connection after wake")

	// Verify route is no longer marked hibernated
	routes = proxy.GetRoutes()
	assert.False(t, routes["sleep.test.local"].Hibernated)
}

func TestMinecraftProxy_HibernatedStatusPingDoesNotWake(t *testing.T) {
	// A status ping (server-list refresh, scanner, bot) against a hibernated
	// route must neither invoke the WakeHandler nor dial the backend, and the
	// route must stay hibernated (MINE-118).
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = backendListener.Close() }()

	backendPort := backendListener.Addr().(*net.TCPAddr).Port

	var backendAccepted atomic.Bool
	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		backendAccepted.Store(true)
	}()

	var wakeCalled atomic.Bool
	proxy := NewMinecraftProxy(&Config{
		ListenAddr: "127.0.0.1:0",
		WakeHandler: func(ctx context.Context, serverID string) error {
			wakeCalled.Store(true)
			return nil
		},
	})
	err = proxy.Start()
	require.NoError(t, err)
	defer func() { _ = proxy.Stop() }()

	time.Sleep(50 * time.Millisecond)

	proxy.AddRoute("srv-sleep", "sleep.test.local", "127.0.0.1", backendPort)
	proxy.SetRouteHibernated("sleep.test.local", true)

	proxyAddr := proxy.listener.Addr().String()
	clientConn, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	require.NoError(t, err)
	defer func() { _ = clientConn.Close() }()

	err = WriteHandshakePacket(clientConn, &HandshakePacket{
		ProtocolVersion: 763, // 1.20.1
		ServerAddress:   "sleep.test.local",
		ServerPort:      25565,
		NextState:       1, // Status
	})
	require.NoError(t, err)

	// Give the proxy time to (incorrectly) wake or dial, then assert stillness.
	time.Sleep(300 * time.Millisecond)

	assert.False(t, wakeCalled.Load(), "WakeHandler must NOT fire on status ping")
	assert.False(t, backendAccepted.Load(), "Backend must NOT be dialed on status ping")

	routes := proxy.GetRoutes()
	require.Contains(t, routes, "sleep.test.local")
	assert.True(t, routes["sleep.test.local"].Hibernated, "Route must stay hibernated after status ping")
}
