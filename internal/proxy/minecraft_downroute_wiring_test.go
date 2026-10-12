package proxy

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMinecraftProxy_DownRouteWithoutWakeHandlerDisconnects covers the
// MINE-172 wiring fault: a login for a Down route with no sleep wake handler
// wired must not hang up silently. The client gets the wake-failed
// disconnect so it sees a message, and the operator gets a loud log.
func TestMinecraftProxy_DownRouteWithoutWakeHandlerDisconnects(t *testing.T) {
	p := NewMinecraftProxy(&Config{ListenAddr: "127.0.0.1:0"})
	// Deliberately no SleepWakeHandler wired.
	p.AddRoute("srv-sleep", "sleep.test.local", "127.0.0.1", 25565)
	p.SetRouteDown("sleep.test.local", true)

	clientConn, peer := net.Pipe()
	defer clientConn.Close()
	defer peer.Close()

	handshake := &HandshakePacket{ProtocolVersion: 767, NextState: 2}
	route := &Route{ServerID: "srv-sleep", Hostname: "sleep.test.local", Down: true}

	done := make(chan bool, 1)
	go func() {
		done <- p.handleDownRoute(clientConn, handshake, "sleep.test.local", route)
	}()

	require.NoError(t, peer.SetReadDeadline(time.Now().Add(5*time.Second)))
	buf := make([]byte, 4096)
	n, err := peer.Read(buf)
	require.NoError(t, err, "handler-less down route must attempt a disconnect, not hang up silently")
	require.Positive(t, n)
	assert.True(t, strings.Contains(string(buf[:n]), "could not wake"),
		"disconnect must carry the wake-failed message, got %q", string(buf[:n]))

	select {
	case relay := <-done:
		assert.False(t, relay, "handler-less down route must never relay")
	case <-time.After(5 * time.Second):
		t.Fatal("handleDownRoute did not return")
	}
}
