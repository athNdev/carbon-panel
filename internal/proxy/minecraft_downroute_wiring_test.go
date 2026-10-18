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

// TestDownRoute_LateSleepWakeHandlerWakesServer covers the startup order in
// main.go where proxyManager.Start() runs before SetSleepWakeHandler is wired:
// a proxy started with no SleepWakeHandler must still invoke a handler attached
// afterwards via SetSleepWakeHandler when a client logs into a down route.
func TestDownRoute_LateSleepWakeHandlerWakesServer(t *testing.T) {
	oldBudget := downRouteBudget
	downRouteBudget = 400 * time.Millisecond
	t.Cleanup(func() { downRouteBudget = oldBudget })

	proxy := NewMinecraftProxy(&Config{ListenAddr: "127.0.0.1:0"})
	require.NoError(t, proxy.Start())
	t.Cleanup(func() { _ = proxy.Stop() })

	proxy.AddRoute("srv-late", "down.test.local", "", 25565)
	proxy.SetRouteDown("down.test.local", true)

	var woke atomic.Int32
	proxy.SetSleepWakeHandler(func(ctx context.Context, serverID string) error {
		if serverID == "srv-late" {
			woke.Add(1)
		}
		return nil
	})

	conn, err := net.DialTimeout("tcp", proxy.listener.Addr().String(), 2*time.Second)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	require.NoError(t, WriteHandshakePacket(conn, &HandshakePacket{
		ProtocolVersion: 767,
		ServerAddress:   "down.test.local",
		ServerPort:      25565,
		NextState:       2,
	}))

	assert.Contains(t, readLoginDisconnect(t, conn), "still waking up")
	assert.Equal(t, int32(1), woke.Load(), "late-wired SleepWakeHandler must be invoked on login")
}
