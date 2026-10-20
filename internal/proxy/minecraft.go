// SPDX-License-Identifier: MIT AND MPL-2.0
// This file includes DiscoPanel material under the MIT License,
// Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
// and still applies to that inherited material.
// Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
// If a copy of the MPL was not distributed with this file, You can obtain
// one at https://mozilla.org/MPL/2.0/.

package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

// MinecraftProxy handles Minecraft protocol proxying with handshake parsing for hostname-based routing
type MinecraftProxy struct {
	listener          net.Listener
	routes            map[string]*Route
	routesMutex       sync.RWMutex
	logger            *logger.Logger
	listenAddr        string
	proxyProtocol     bool
	wakeHandler       WakeHandler
	activityHandler   ActivityHandler
	sleepWakeHandler  WakeHandler
	acceptsTransfers  bool
	handshakes        *handshakeLimiter
	hibernatedMOTD    string
	hibernatedFavicon string
	// waking tracks hostnames with a recent boot attempt so pings during
	// boot serve the loading MOTD instead of the asleep one (MINE-121).
	waking       map[string]time.Time
	running      bool
	runningMutex sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewMinecraftProxy creates a new Minecraft proxy instance
func NewMinecraftProxy(cfg *Config) *MinecraftProxy {
	ctx, cancel := context.WithCancel(context.Background())
	log := cfg.Logger
	if log == nil {
		log = logger.New()
	}
	acceptsTransfers := true
	if cfg.AcceptsTransfers != nil {
		acceptsTransfers = *cfg.AcceptsTransfers
	}
	return &MinecraftProxy{
		routes:            make(map[string]*Route),
		logger:            log,
		listenAddr:        cfg.ListenAddr,
		proxyProtocol:     cfg.ProxyProtocol,
		wakeHandler:       cfg.WakeHandler,
		activityHandler:   cfg.ActivityHandler,
		sleepWakeHandler:  cfg.SleepWakeHandler,
		acceptsTransfers:  acceptsTransfers,
		handshakes:        newHandshakeLimiter(cfg.LoginRateLimit),
		hibernatedMOTD:    cfg.HibernatedMOTD,
		hibernatedFavicon: cfg.HibernatedFavicon,
		waking:            make(map[string]time.Time),
		ctx:               ctx,
		cancel:            cancel,
	}
}

// SetWakeHandler configures the wake callback for hibernated servers
func (p *MinecraftProxy) SetWakeHandler(h WakeHandler) {
	p.routesMutex.Lock()
	defer p.routesMutex.Unlock()
	p.wakeHandler = h
}

// SetActivityHandler configures the login-intent activity callback (MINE-120)
func (p *MinecraftProxy) SetActivityHandler(h ActivityHandler) {
	p.routesMutex.Lock()
	defer p.routesMutex.Unlock()
	p.activityHandler = h
}

// SetSleepWakeHandler configures the deep-sleep boot callback (MINE-121)
func (p *MinecraftProxy) SetSleepWakeHandler(h WakeHandler) {
	p.routesMutex.Lock()
	defer p.routesMutex.Unlock()
	p.sleepWakeHandler = h
}

// SetRouteDown marks a route's backend as deeply asleep (or back). Clearing
// the flag also clears any recorded boot attempt for the hostname.
func (p *MinecraftProxy) SetRouteDown(hostname string, down bool) {
	p.routesMutex.Lock()
	defer p.routesMutex.Unlock()

	hostname = strings.ToLower(strings.Split(hostname, ":")[0])
	if route, exists := p.routes[hostname]; exists {
		route.Down = down
		if !down {
			delete(p.waking, hostname)
		}
		p.logger.Info("Set route down: hostname=%s down=%v", hostname, down)
	}
}

// SetRouteHibernated enables or disables hibernation state for a route
func (p *MinecraftProxy) SetRouteHibernated(hostname string, hibernated bool) {
	p.routesMutex.Lock()
	defer p.routesMutex.Unlock()

	hostname = strings.ToLower(strings.Split(hostname, ":")[0])
	if route, exists := p.routes[hostname]; exists {
		route.Hibernated = hibernated
		p.logger.Info("Set route hibernated: hostname=%s hibernated=%v", hostname, hibernated)
	}
}

// AddRoute adds a new routing rule
func (p *MinecraftProxy) AddRoute(serverID, hostname, backendHost string, backendPort int) {
	p.routesMutex.Lock()
	defer p.routesMutex.Unlock()

	hostname = strings.ToLower(strings.Split(hostname, ":")[0])

	p.routes[hostname] = &Route{
		ServerID:    serverID,
		Hostname:    hostname,
		BackendHost: backendHost,
		BackendPort: backendPort,
		Active:      true,
	}

	p.logger.Info("Added route: hostname=%s backend=%s:%d", hostname, backendHost, backendPort)
}

// RemoveRoute removes a routing rule
func (p *MinecraftProxy) RemoveRoute(hostname string) {
	p.routesMutex.Lock()
	defer p.routesMutex.Unlock()

	hostname = strings.ToLower(strings.Split(hostname, ":")[0])
	delete(p.routes, hostname)

	p.logger.Info("Removed route: hostname=%s", hostname)
}

// UpdateRoute updates the backend for a route
func (p *MinecraftProxy) UpdateRoute(hostname, backendHost string, backendPort int) {
	p.routesMutex.Lock()
	defer p.routesMutex.Unlock()

	hostname = strings.ToLower(strings.Split(hostname, ":")[0])
	if route, exists := p.routes[hostname]; exists {
		route.BackendHost = backendHost
		route.BackendPort = backendPort
		p.logger.Info("Updated route: hostname=%s backend=%s:%d", hostname, backendHost, backendPort)
	}
}

// SetRouteActive enables or disables a route
func (p *MinecraftProxy) SetRouteActive(hostname string, active bool) {
	p.routesMutex.Lock()
	defer p.routesMutex.Unlock()

	hostname = strings.ToLower(strings.Split(hostname, ":")[0])
	if route, exists := p.routes[hostname]; exists {
		route.Active = active
		p.logger.Info("Set route active: hostname=%s active=%v", hostname, active)
	}
}

// Start starts the proxy server
func (p *MinecraftProxy) Start() error {
	p.runningMutex.Lock()
	defer p.runningMutex.Unlock()

	if p.running {
		return fmt.Errorf("proxy already running")
	}

	listener, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", p.listenAddr, err)
	}

	p.listener = listener
	p.running = true

	go p.acceptLoop()

	p.logger.Info("Minecraft proxy started on %s", p.listenAddr)
	return nil
}

// Stop stops the proxy server
func (p *MinecraftProxy) Stop() error {
	p.runningMutex.Lock()
	defer p.runningMutex.Unlock()

	if !p.running {
		return nil
	}

	p.cancel()
	p.running = false

	if p.listener != nil {
		if err := p.listener.Close(); err != nil {
			return fmt.Errorf("failed to close listener: %w", err)
		}
	}

	p.logger.Info("Minecraft proxy stopped")
	return nil
}

// acceptLoop accepts incoming connections
func (p *MinecraftProxy) acceptLoop() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.ctx.Done():
				return
			default:
				p.logger.Error("Failed to accept connection: %v", err)
				continue
			}
		}

		go p.handleConnection(conn)
	}
}

// handleConnection handles a single client connection with Minecraft protocol parsing
func (p *MinecraftProxy) handleConnection(clientConn net.Conn) {
	defer clientConn.Close()

	p.logger.Debug("Attempting to route incoming Minecraft connection!")

	// Set initial timeout for handshake and PROXY protocol header
	clientConn.SetReadDeadline(time.Now().Add(10 * time.Second))

	// Detect and parse PROXY protocol v2 header from upstream load balancers (MINE-16)
	wrappedConn, pxyHdr, err := WrapConnWithProxyProtocol(clientConn)
	if err != nil {
		p.logger.Debug("Failed to parse PROXY protocol v2 from %s: %v", clientConn.RemoteAddr(), err)
		return
	}
	clientConn = wrappedConn
	if pxyHdr != nil && pxyHdr.SrcAddr != nil {
		p.logger.Debug("Preserved real player IP via PROXY protocol v2: %s", clientConn.RemoteAddr())
	}

	// Read the handshake packet
	handshake, err := ReadHandshakePacket(clientConn)
	if err != nil {
		p.logger.Debug("Failed to read handshake from %s: %v", clientConn.RemoteAddr(), err)
		return
	}

	// Extract hostname from the handshake
	p.logger.Debug("Extracting hostname from: %s", handshake.ServerAddress)
	hostname := strings.ToLower(strings.Split(handshake.ServerAddress, ":")[0])
	if idx := strings.IndexByte(hostname, 0); idx != -1 {
		hostname = hostname[:idx]
		p.logger.Debug("Null byte(s) detected, trimmed suffix null termination: %s", hostname)
	}

	// Find the route
	p.routesMutex.RLock()
	route, exists := p.routes[hostname]
	p.routesMutex.RUnlock()

	if !exists || !route.Active {
		p.logger.Debug("No active route found for hostname: %s", hostname)
		p.routesMutex.RLock()
		p.logger.Debug("Available routes:")
		for r := range p.routes {
			p.logger.Debug("%s", r)
		}
		p.routesMutex.RUnlock()
		return
	}

	// Per-IP handshake budget is shared by status and login and is checked
	// before activity, wake, or dial. A connection that already reached play
	// is not revisited (MINE-204).
	if !p.handshakes.allow(clientIP(clientConn.RemoteAddr())) {
		p.logger.Debug("Handshake rate limit for %s", clientConn.RemoteAddr())
		if isLoginIntent(handshake.NextState) {
			_ = sendLoginDisconnect(clientConn, "Please reconnect in a moment.")
		}
		return
	}

	// Transfer (NextState 3) is login intent unless the operator disabled it.
	// Reject before activity, wake, and dial (MINE-203).
	if handshake.NextState == 3 && !p.acceptsTransfers {
		p.logger.Debug("Rejecting transfer handshake for %s: accepts_transfers is false", hostname)
		_ = sendLoginDisconnect(clientConn, "Transfers are not accepted by this server.")
		return
	}

	// Login intent resets idle tracking (MINE-120). Status pings are
	// deliberately excluded: server-list refreshes and scanners must neither
	// wake hibernated servers (MINE-118) nor keep running ones from sleeping.
	// Transfer follows the same path as login (MINE-203).
	if isLoginIntent(handshake.NextState) && p.activityHandler != nil {
		p.activityHandler(route.ServerID)
	}

	// Wake hibernated server container only on real login intent (MINE-118).
	// Status pings (NextState=1: server-list refreshes, scanners, bots) must
	// never wake or dial a hibernated backend — waking on any handshake is
	// the pause/knock wake loop from the DiscoPanel report. A ping gets the
	// cached sleeping MOTD and the connection ends without a backend dial
	// (MINE-206).
	if route.Hibernated {
		if !isLoginIntent(handshake.NextState) {
			p.logger.Debug("Serving sleeping MOTD for hibernated server %s (host %s): staying asleep", route.ServerID, hostname)
			motd := p.hibernatedMOTD
			if motd == "" {
				motd = hibernatedMOTD
			}
			if err := serveStatusResponseWithFavicon(clientConn, handshake.ProtocolVersion, motd, p.hibernatedFavicon); err != nil {
				p.logger.Debug("Failed to serve hibernated MOTD for host %s: %v", hostname, err)
			}
			return
		}
		if p.wakeHandler == nil {
			return
		}
		p.logger.Info("Login intent for hibernated server %s (host %s), waking via cgroup freezer...", route.ServerID, hostname)
		wakeCtx, wakeCancel := context.WithTimeout(p.ctx, 5*time.Second)
		if err := p.wakeHandler(wakeCtx, route.ServerID); err != nil {
			wakeCancel()
			p.logger.Error("Failed to wake hibernated server %s: %v", route.ServerID, err)
			return
		}
		wakeCancel()

		p.routesMutex.Lock()
		if r, ok := p.routes[hostname]; ok {
			r.Hibernated = false
		}
		p.routesMutex.Unlock()
	}

	// Deeply-asleep backends (MINE-121): pings get a cached MOTD and hang
	// up; logins boot the container and are held here until the SLP health
	// gate passes, then fall through to the normal relay below.
	if route.Down {
		if !p.handleDownRoute(clientConn, handshake, hostname, route) {
			return
		}
		// Boot + health gate passed: re-read the refreshed route (the sleep
		// manager re-resolved the backend IP after start) and relay.
		p.routesMutex.RLock()
		if r, ok := p.routes[hostname]; ok {
			route = r
		}
		p.routesMutex.RUnlock()
	}

	// Connect to backend (with quick retries to allow socket bind right after unfreeze)
	backendAddr := net.JoinHostPort(route.BackendHost, strconv.Itoa(route.BackendPort))
	backendConn, err := dialBackendWithRetry(p.ctx, backendAddr, backendDialAttempts, backendDialTimeout, backendDialBackoff)
	if err != nil {
		p.logger.Error("Failed to connect to backend %s: %v", backendAddr, err)
		return
	}
	defer backendConn.Close()

	// Modify handshake packet to use backend's expected hostname
	// For Forge servers, we need to preserve any FML data in the address field
	addressParts := strings.Split(handshake.ServerAddress, "\x00")
	if len(addressParts) > 1 {
		// Forge client detected - preserve all FML protocol data
		originalHost := addressParts[0]
		addressParts[0] = "localhost"

		if len(addressParts) >= 2 {
			fmlVersion := addressParts[1]
			p.logger.Debug("Forge handshake detected - FML version: %s, original host: %s", fmlVersion, originalHost)

			if len(addressParts) > 2 {
				p.logger.Debug("Additional FML data segments: %d", len(addressParts)-2)
			}
		}

		handshake.ServerAddress = strings.Join(addressParts, "\x00")
	} else {
		handshake.ServerAddress = "localhost"
	}
	handshake.ServerPort = uint16(route.BackendPort)

	// If PROXY protocol forwarding is enabled for backend, write PROXY v2 header first
	if p.proxyProtocol {
		if err := WriteProxyV2Header(backendConn, clientConn.RemoteAddr(), backendConn.RemoteAddr()); err != nil {
			p.logger.Error("Failed to write PROXY protocol v2 header to backend: %v", err)
			return
		}
	}

	// Forward the modified handshake to the backend
	if err := WriteHandshakePacket(backendConn, handshake); err != nil {
		p.logger.Error("Failed to write handshake to backend: %v", err)
		return
	}

	// Clear timeouts for proxying
	clientConn.SetReadDeadline(time.Time{})
	backendConn.SetReadDeadline(time.Time{})

	// Start bidirectional proxying
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(backendConn, clientConn)
		backendConn.Close()
	}()

	go func() {
		defer wg.Done()
		io.Copy(clientConn, backendConn)
		clientConn.Close()
	}()

	wg.Wait()
}

// downRouteBudget bounds the client hold so a held client never stares at a
// dead connection past the ~30s Java client timeout (MINE-121). A var (not
// const) so tests can shrink the hold without waiting it out.
var downRouteBudget = 25 * time.Second

// wakeBootBudget bounds the background container boot triggered by a login on a
// down route. It must outlive downRouteBudget: cold/first-boot starts routinely
// take longer than the 25s client hold, and cancelling the wake when the client
// hold expires aborts an otherwise healthy boot (MINE-172).
var wakeBootBudget = 5 * time.Minute

// loadingMOTDWindow is how long after a boot attempt pings serve the loading
// MOTD before falling back to the asleep one.
const loadingMOTDWindow = 90 * time.Second

// handleDownRoute serves or boots a deeply-asleep backend (MINE-121).
// Status pings receive a cached MOTD (asleep vs loading) and hang up without
// touching Docker. Logins trigger a boot via SleepWakeHandler and are held
// until the SLP health gate passes. Returns true when the caller may relay.
func (p *MinecraftProxy) handleDownRoute(clientConn net.Conn, handshake *HandshakePacket, hostname string, route *Route) bool {
	if !isLoginIntent(handshake.NextState) {
		p.routesMutex.RLock()
		since, booting := p.waking[hostname]
		p.routesMutex.RUnlock()

		motd := asleepMOTD
		if booting && time.Since(since) < loadingMOTDWindow {
			motd = loadingMOTD
		}
		if err := serveStatusResponse(clientConn, handshake.ProtocolVersion, motd); err != nil {
			p.logger.Debug("Failed to serve down-route MOTD for host %s: %v", hostname, err)
		}
		return false
	}

	p.routesMutex.RLock()
	handler := p.sleepWakeHandler
	p.routesMutex.RUnlock()

	if handler == nil {
		p.logger.Error("Login for down route %s (server %s) refused: no sleep wake handler wired", hostname, route.ServerID)
		_ = sendLoginDisconnect(clientConn, wakeFailedMessage)
		return false
	}

	deadline := time.Now().Add(downRouteBudget)

	p.routesMutex.Lock()
	p.waking[hostname] = time.Now()
	p.routesMutex.Unlock()

	p.logger.Info("Login intent for deeply-asleep server %s (host %s), booting...", route.ServerID, hostname)
	// Run the boot on its own budget so a slow cold start continues in the
	// background after the 25s client hold expires. WakeServer is idempotent,
	// so a second login while the boot is still running joins the same wake.
	serverID := route.ServerID
	go func() {
		wakeCtx, wakeCancel := context.WithTimeout(p.ctx, wakeBootBudget)
		defer wakeCancel()
		if err := handler(wakeCtx, serverID); err != nil {
			p.logger.Error("Failed to boot deeply-asleep server %s: %v", serverID, err)
			p.routesMutex.Lock()
			delete(p.waking, hostname)
			p.routesMutex.Unlock()
		}
	}()

	// Hold the client until the refreshed backend answers SLP (JVM-level
	// readiness, not just TCP-open). Concurrent joiners share the single
	// boot: the sleep manager's wake is idempotent.
	for time.Now().Before(deadline) {
		p.routesMutex.RLock()
		r, ok := p.routes[hostname]
		var host string
		var port int
		var down bool
		if ok {
			host, port, down = r.BackendHost, r.BackendPort, r.Down
		}
		p.routesMutex.RUnlock()

		if !ok {
			p.logger.Debug("Down route %s vanished during boot hold; hanging up", hostname)
			return false
		}
		if !down && host != "" {
			if err := slpHealthCheck(host, port, handshake.ProtocolVersion, 3*time.Second); err == nil {
				return true
			}
		}
		select {
		case <-p.ctx.Done():
			return false
		case <-time.After(500 * time.Millisecond):
		}
	}

	p.logger.Info("Boot hold for host %s exceeded budget; disconnecting with message (client retries, pings show loading MOTD)", hostname)
	_ = sendLoginDisconnect(clientConn, holdTimeoutMessage)
	return false
}

// GetRoutes returns a copy of all current routes
func (p *MinecraftProxy) GetRoutes() map[string]*Route {
	p.routesMutex.RLock()
	defer p.routesMutex.RUnlock()

	routes := make(map[string]*Route)
	for k, v := range p.routes {
		routeCopy := *v
		routes[k] = &routeCopy
	}
	return routes
}

// IsRunning returns whether the proxy is running
func (p *MinecraftProxy) IsRunning() bool {
	p.runningMutex.RLock()
	defer p.runningMutex.RUnlock()
	return p.running
}

// Backend dial retry budget. The retries exist to tolerate a socket that has
// not finished binding right after a container unfreezes.
const (
	backendDialAttempts = 5
	backendDialTimeout  = 2 * time.Second
	backendDialBackoff  = 100 * time.Millisecond
)

// dialBackendWithRetry dials addr, retrying on failure.
//
// Both the wait between attempts and the attempt loop itself abort as soon as
// ctx is done, so a proxy shutdown does not leave a per-connection goroutine
// parked for the full retry budget.
//
// NOTE: this deliberately does not attempt to detect a client that has already
// disconnected. Go offers no non-consuming liveness probe for a net.Conn — any
// check requires reading, which would consume a pipelined client byte and
// corrupt the protocol stream. The residual cost of a departed client is one
// goroutine and one backend socket held for at most the retry budget; the relay
// then closes them immediately.
func dialBackendWithRetry(ctx context.Context, addr string, attempts int, dialTimeout, backoff time.Duration) (net.Conn, error) {
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("dial %s aborted: %w", addr, ctx.Err())
		default:
		}

		conn, err := net.DialTimeout("tcp", addr, dialTimeout)
		if err == nil {
			return conn, nil
		}
		lastErr = err

		if attempt == attempts-1 {
			break
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("dial %s aborted: %w", addr, ctx.Err())
		case <-timer.C:
		}
	}
	return nil, lastErr
}
