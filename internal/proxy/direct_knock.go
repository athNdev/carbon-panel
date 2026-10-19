package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

type directKnockListener struct {
	serverID string
	port     int
	listener net.Listener
	closed   bool
	mu       sync.Mutex
}

func (dk *directKnockListener) close() {
	dk.mu.Lock()
	defer dk.mu.Unlock()
	if !dk.closed && dk.listener != nil {
		dk.closed = true
		_ = dk.listener.Close()
	}
}

// DirectKnockManager manages direct port TCP listeners for deeply-asleep servers
// that do not have a proxy hostname (i.e. direct-port servers).
type DirectKnockManager struct {
	mu          sync.Mutex
	knocks      map[string]*directKnockListener // serverID -> listener
	wakeHandler WakeHandler
	logger      *logger.Logger
}

// NewDirectKnockManager creates a new DirectKnockManager.
func NewDirectKnockManager(wakeHandler WakeHandler, log *logger.Logger) *DirectKnockManager {
	return &DirectKnockManager{
		knocks:      make(map[string]*directKnockListener),
		wakeHandler: wakeHandler,
		logger:      log,
	}
}

// SetWakeHandler updates the wake callback used when a client attempts to join.
func (m *DirectKnockManager) SetWakeHandler(h WakeHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wakeHandler = h
}

// StartKnock starts a TCP listener on the server's port.
func (m *DirectKnockManager) StartKnock(server *db.Server) error {
	if server == nil || server.Port <= 0 {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// If already listening on this port for this server, keep it
	if existing, ok := m.knocks[server.ID]; ok && existing != nil && !existing.closed {
		if existing.port == server.Port {
			return nil
		}
		existing.close()
		delete(m.knocks, server.ID)
	}

	addr := fmt.Sprintf(":%d", server.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		if m.logger != nil {
			m.logger.Warn("Direct knock: failed to bind port %d for server %s: %v", server.Port, server.Name, err)
		}
		return err
	}

	dk := &directKnockListener{
		serverID: server.ID,
		port:     server.Port,
		listener: listener,
	}
	m.knocks[server.ID] = dk

	if m.logger != nil {
		m.logger.Info("Direct knock: listening on port %d for deeply-asleep server %s (%s)", server.Port, server.Name, server.ID)
	}

	go m.serve(dk, server.Name)
	return nil
}

func (m *DirectKnockManager) serve(dk *directKnockListener, serverName string) {
	for {
		conn, err := dk.listener.Accept()
		if err != nil {
			return // listener was closed
		}

		go m.handleConn(conn, dk, serverName)
	}
}

func (m *DirectKnockManager) handleConn(conn net.Conn, dk *directKnockListener, serverName string) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	handshake, err := ReadHandshakePacket(conn)
	if err != nil {
		return
	}

	if handshake.NextState == 1 {
		// Server List Ping: return asleep MOTD
		_ = serveStatusResponse(conn, handshake.ProtocolVersion, deepSleepMOTD)
		return
	}

	if handshake.NextState == 2 {
		// Login intent: player wants to join!
		if m.logger != nil {
			m.logger.Info("Direct knock: login intent on port %d for server %s; closing listener and waking", dk.port, serverName)
		}

		// Drain client's Login Start packet if present so kernel socket receive buffer is clean
		_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		if loginLen, err := ReadVarInt(conn); err == nil && loginLen > 0 && loginLen < 4096 {
			loginData := make([]byte, loginLen)
			_, _ = io.ReadFull(conn, loginData)
		}

		// Close listener immediately so the host port is freed for Docker!
		m.StopKnock(dk.serverID)

		// Send friendly disconnect message informing the player
		_ = sendLoginDisconnect(conn, "§b[Carbon] §aServer is booting up from deep sleep!\n§7Please wait a moment and reconnect.")

		// Trigger wake asynchronously
		m.mu.Lock()
		handler := m.wakeHandler
		m.mu.Unlock()

		if handler != nil {
			go func() {
				wakeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer cancel()
				if err := handler(wakeCtx, dk.serverID); err != nil && m.logger != nil {
					m.logger.Error("Direct knock: failed to wake server %s: %v", serverName, err)
				}
			}()
		}
	}
}

// StopKnock stops the direct knock listener for a specific server.
func (m *DirectKnockManager) StopKnock(serverID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if dk, ok := m.knocks[serverID]; ok && dk != nil {
		dk.close()
		delete(m.knocks, serverID)
		if m.logger != nil {
			m.logger.Info("Direct knock: stopped listener for server %s (port %d)", serverID, dk.port)
		}
	}
}

// StopAll stops all active direct knock listeners.
func (m *DirectKnockManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, dk := range m.knocks {
		if dk != nil {
			dk.close()
		}
		delete(m.knocks, id)
	}
}

// HasKnock reports whether a direct knock listener is active for serverID.
func (m *DirectKnockManager) HasKnock(serverID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	dk, ok := m.knocks[serverID]
	return ok && dk != nil && !dk.closed
}
