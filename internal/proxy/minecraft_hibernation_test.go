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

	// Build and write a Minecraft handshake packet
	handshake := &HandshakePacket{
		ProtocolVersion: 763, // 1.20.1
		ServerAddress:   "sleep.test.local",
		ServerPort:      25565,
		NextState:       1, // Status
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
