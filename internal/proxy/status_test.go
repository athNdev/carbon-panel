package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errWakeFailedTest = errors.New("no container for test")

// startDownRouteProxy starts a proxy with a single down (deeply-asleep) route.
// The backend listener is returned unaccepted: any dial to it proves the
// proxy incorrectly touched a down backend.
func startDownRouteProxy(t *testing.T, wakeHandler WakeHandler) (*MinecraftProxy, int, *atomic.Bool, *atomic.Bool) {
	t.Helper()

	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = backendListener.Close() })
	backendPort := backendListener.Addr().(*net.TCPAddr).Port

	var backendDialed atomic.Bool
	go func() {
		for {
			conn, err := backendListener.Accept()
			if err != nil {
				return
			}
			backendDialed.Store(true)
			_ = conn.Close()
		}
	}()

	var wakeCalled atomic.Bool
	var handler WakeHandler
	if wakeHandler != nil {
		handler = wakeHandler
	} else {
		handler = func(ctx context.Context, serverID string) error {
			wakeCalled.Store(true)
			return nil
		}
	}

	proxy := NewMinecraftProxy(&Config{
		ListenAddr:       "127.0.0.1:0",
		SleepWakeHandler: handler,
	})
	require.NoError(t, proxy.Start())
	t.Cleanup(func() { _ = proxy.Stop() })

	time.Sleep(50 * time.Millisecond)
	proxy.AddRoute("srv-down", "down.test.local", "127.0.0.1", backendPort)
	proxy.SetRouteDown("down.test.local", true)

	return proxy, backendPort, &wakeCalled, &backendDialed
}

func dialDownRoute(t *testing.T, proxy *MinecraftProxy, nextState VarInt) net.Conn {
	t.Helper()

	proxyAddr := proxy.listener.Addr().String()
	conn, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	require.NoError(t, err)

	require.NoError(t, WriteHandshakePacket(conn, &HandshakePacket{
		ProtocolVersion: 763,
		ServerAddress:   "down.test.local",
		ServerPort:      25565,
		NextState:       nextState,
	}))
	return conn
}

// readStatusPayload consumes a Status Request + response like a game client
// and returns the decoded JSON payload.
func readStatusPayload(t *testing.T, conn net.Conn) statusPayload {
	t.Helper()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	// Status Request.
	_, err := conn.Write([]byte{0x01, 0x00})
	require.NoError(t, err)

	respLen, err := ReadVarInt(conn)
	require.NoError(t, err)
	require.Greater(t, int(respLen), 1)
	resp := make([]byte, respLen)
	_, err = io.ReadFull(conn, resp)
	require.NoError(t, err)
	require.Equal(t, byte(0x00), resp[0])

	// Body: packet id + varint string length + JSON.
	body := resp[1:]
	require.NotEmpty(t, body)
	// Skip packet id varint (0x00, single byte).
	jsonBytes := body[1:]
	// Skip string-length varint.
	_, n := decodeVarIntlen(jsonBytes)
	jsonBytes = jsonBytes[n:]

	var payload statusPayload
	require.NoError(t, json.Unmarshal(jsonBytes, &payload))
	return payload
}

// decodeVarIntlen returns the value and encoded length of a varint prefix.
func decodeVarIntlen(b []byte) (int, int) {
	val := 0
	shift := 0
	for i, c := range b {
		val |= int(c&0x7F) << shift
		if c&0x80 == 0 {
			return val, i + 1
		}
		shift += 7
	}
	return val, len(b)
}

func TestDownRoute_StatusPingGetsAsleepMOTD(t *testing.T) {
	proxy, _, wakeCalled, backendDialed := startDownRouteProxy(t, nil)

	conn := dialDownRoute(t, proxy, 1)
	defer func() { _ = conn.Close() }()

	payload := readStatusPayload(t, conn)
	assert.Equal(t, asleepMOTD, payload.Description.Text)
	assert.Equal(t, 0, payload.Players.Online)

	time.Sleep(200 * time.Millisecond)
	assert.False(t, wakeCalled.Load(), "Status ping must NOT boot a down backend")
	assert.False(t, backendDialed.Load(), "Status ping must NOT dial a down backend")

	routes := proxy.GetRoutes()
	require.Contains(t, routes, "down.test.local")
	assert.True(t, routes["down.test.local"].Down, "Route must stay down after status ping")
}

func TestDownRoute_StatusPingDuringBootGetsLoadingMOTD(t *testing.T) {
	proxy, _, _, _ := startDownRouteProxy(t, nil)

	// Simulate a recent boot attempt without clearing the down flag.
	proxy.routesMutex.Lock()
	proxy.waking["down.test.local"] = time.Now()
	proxy.routesMutex.Unlock()

	conn := dialDownRoute(t, proxy, 1)
	defer func() { _ = conn.Close() }()

	payload := readStatusPayload(t, conn)
	assert.Equal(t, loadingMOTD, payload.Description.Text)
}

func TestDownRoute_LoginWithoutHandlerHangsUp(t *testing.T) {
	proxy := NewMinecraftProxy(&Config{ListenAddr: "127.0.0.1:0"})
	require.NoError(t, proxy.Start())
	t.Cleanup(func() { _ = proxy.Stop() })
	time.Sleep(50 * time.Millisecond)

	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = backendListener.Close() })
	var backendDialed atomic.Bool
	go func() {
		for {
			conn, err := backendListener.Accept()
			if err != nil {
				return
			}
			backendDialed.Store(true)
			_ = conn.Close()
		}
	}()

	proxy.AddRoute("srv-down", "down.test.local", "127.0.0.1", backendListener.Addr().(*net.TCPAddr).Port)
	proxy.SetRouteDown("down.test.local", true)
	// No SleepWakeHandler set.

	conn := dialDownRoute(t, proxy, 2)
	defer func() { _ = conn.Close() }()

	// Proxy must hang up: the login handshake gets no backend and the
	// connection closes (read times out or EOFs).
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	one := make([]byte, 1)
	_, err = conn.Read(one)
	assert.Error(t, err, "Connection must hang up when no sleep wake handler is set")

	time.Sleep(200 * time.Millisecond)
	assert.False(t, backendDialed.Load(), "Down backend must NOT be dialed without a wake handler")
}

func TestDownRoute_LoginBootsAndRelays(t *testing.T) {
	// Full Tier-A path: login on a down route boots the backend (handler),
	// the proxy holds the client through the SLP health gate, then relays.
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = backendListener.Close() })
	backendPort := backendListener.Addr().(*net.TCPAddr).Port

	relayed := make(chan []byte, 1)
	go func() {
		// Connection 1: SLP health gate from the proxy.
		slpConn, err := backendListener.Accept()
		if err != nil {
			return
		}
		_ = slpConn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := ReadHandshakePacket(slpConn); err != nil {
			_ = slpConn.Close()
			return
		}
		req := make([]byte, 2)
		if _, err := io.ReadFull(slpConn, req); err != nil {
			_ = slpConn.Close()
			return
		}
		body, _ := json.Marshal(statusPayload{
			Version:     statusVersion{Name: "fake", Protocol: 763},
			Players:     statusPlayers{Max: 20, Online: 0},
			Description: statusDescription{Text: "up"},
		})
		resp := appendVarInt(nil, 0x00)
		resp = appendVarInt(resp, VarInt(len(body)))
		resp = append(resp, body...)
		out := appendVarInt(nil, VarInt(len(resp)))
		out = append(out, resp...)
		_, _ = slpConn.Write(out)
		_ = slpConn.Close()

		// Connection 2: the relayed client. Read the rewritten handshake,
		// then whatever the client sends next.
		relayConn, err := backendListener.Accept()
		if err != nil {
			return
		}
		_ = relayConn.SetDeadline(time.Now().Add(5 * time.Second))
		defer func() { _ = relayConn.Close() }()
		hs, err := ReadHandshakePacket(relayConn)
		if err != nil || hs.ServerAddress != "localhost" {
			return
		}
		extra := make([]byte, 2)
		if _, err := io.ReadFull(relayConn, extra); err != nil {
			return
		}
		relayed <- extra
	}()

	var proxyRef *MinecraftProxy
	var wakeCalled atomic.Bool
	proxy := NewMinecraftProxy(&Config{
		ListenAddr: "127.0.0.1:0",
		SleepWakeHandler: func(ctx context.Context, serverID string) error {
			wakeCalled.Store(true)
			// Simulate DeepSleepManager: container up, route refreshed.
			proxyRef.SetRouteDown("down.test.local", false)
			return nil
		},
	})
	proxyRef = proxy
	require.NoError(t, proxy.Start())
	t.Cleanup(func() { _ = proxy.Stop() })
	time.Sleep(50 * time.Millisecond)

	proxy.AddRoute("srv-down", "down.test.local", "127.0.0.1", backendPort)
	proxy.SetRouteDown("down.test.local", true)

	conn := dialDownRoute(t, proxy, 2)
	defer func() { _ = conn.Close() }()

	// Send post-handshake bytes (stands in for Login Start); the proxy
	// relays them once the health gate passes.
	_, err = conn.Write([]byte{0x68, 0x69})
	require.NoError(t, err)

	select {
	case got := <-relayed:
		assert.Equal(t, []byte{0x68, 0x69}, got, "Client bytes must be relayed after boot")
	case <-time.After(15 * time.Second):
		t.Fatal("Timed out waiting for relayed bytes after boot")
	}
	assert.True(t, wakeCalled.Load(), "Sleep wake handler must fire on login to down route")
}

// readLoginDisconnect consumes a Login Disconnect packet and returns the chat text.
func readLoginDisconnect(t *testing.T, conn net.Conn) string {
	t.Helper()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	length, err := ReadVarInt(conn)
	require.NoError(t, err)
	require.Greater(t, int(length), 1)
	data := make([]byte, length)
	_, err = io.ReadFull(conn, data)
	require.NoError(t, err)
	require.Equal(t, byte(0x00), data[0])

	rest := data[1:]
	_, n := decodeVarIntlen(rest)
	rest = rest[n:]

	var desc statusDescription
	require.NoError(t, json.Unmarshal(rest, &desc))
	return desc.Text
}

func startDownRouteWithHandler(t *testing.T, handler WakeHandler) *MinecraftProxy {
	t.Helper()

	proxy := NewMinecraftProxy(&Config{
		ListenAddr:       "127.0.0.1:0",
		SleepWakeHandler: handler,
	})
	require.NoError(t, proxy.Start())
	t.Cleanup(func() { _ = proxy.Stop() })
	time.Sleep(50 * time.Millisecond)

	// Backend is never dialed on these paths (route stays down), so a
	// discard address is fine.
	proxy.AddRoute("srv-down", "down.test.local", "127.0.0.1", 1)
	proxy.SetRouteDown("down.test.local", true)
	return proxy
}

func TestDownRoute_LoginHoldTimeoutSendsDisconnect(t *testing.T) {
	// Boot never completes: the held client must get a waking-up message
	// instead of a bare hangup (MINE-123).
	oldBudget := downRouteBudget
	downRouteBudget = 400 * time.Millisecond
	t.Cleanup(func() { downRouteBudget = oldBudget })

	proxy := startDownRouteWithHandler(t, func(ctx context.Context, serverID string) error {
		return nil // boot "in flight" forever; route stays down
	})

	conn := dialDownRoute(t, proxy, 2)
	defer func() { _ = conn.Close() }()

	assert.Contains(t, readLoginDisconnect(t, conn), "still waking up")
}

func TestDownRoute_LoginWakeFailureSendsDisconnect(t *testing.T) {
	// Boot fails outright: the client must get a wake-failed message.
	proxy := startDownRouteWithHandler(t, func(ctx context.Context, serverID string) error {
		return errWakeFailedTest
	})

	conn := dialDownRoute(t, proxy, 2)
	defer func() { _ = conn.Close() }()

	assert.Contains(t, readLoginDisconnect(t, conn), "could not wake up")
}
