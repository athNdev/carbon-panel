package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

// MinecraftProxy handles Minecraft protocol proxying with handshake parsing for hostname-based routing
type MinecraftProxy struct {
	listener         net.Listener
	routes           map[string]*Route
	routesMutex      sync.RWMutex
	logger           *logger.Logger
	listenAddr       string
	proxyProtocol    bool
	wakeHandler      WakeHandler
	activityHandler  ActivityHandler
	sleepWakeHandler WakeHandler
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
	return &MinecraftProxy{
		routes:           make(map[string]*Route),
		logger:           log,
		listenAddr:       cfg.ListenAddr,
		proxyProtocol:    cfg.ProxyProtocol,
		wakeHandler:      cfg.WakeHandler,
		activityHandler:  cfg.ActivityHandler,
		sleepWakeHandler: cfg.SleepWakeHandler,
		waking:           make(map[string]time.Time),
		ctx:              ctx,
		cancel:           cancel,
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

	// Login intent resets idle tracking (MINE-120). Status pings are
	// deliberately excluded: server-list refreshes and scanners must neither
	// wake hibernated servers (MINE-118) nor keep running ones from sleeping.
	if handshake.NextState == 2 && p.activityHandler != nil {
		p.activityHandler(route.ServerID)
	}

	// Wake hibernated server container only on real login intent (MINE-118).
	// Status pings (NextState=1: server-list refreshes, scanners, bots) must
	// never wake or dial a hibernated backend — waking on any handshake is
	// the pause/knock wake loop from the DiscoPanel report. A pinged
	// hibernated route simply closes; the client sees the server as
	// unreachable, same as mc-router with an empty asleep MOTD.
	if route.Hibernated {
		if handshake.NextState != 2 {
			p.logger.Debug("Ignoring status ping for hibernated server %s (host %s): staying asleep", route.ServerID, hostname)
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
	backendAddr := net.JoinHostPort(route.BackendHost, fmt.Sprintf("%d", route.BackendPort))
	var backendConn net.Conn
	var dialErr error
	for attempt := 0; attempt < 5; attempt++ {
		backendConn, dialErr = net.DialTimeout("tcp", backendAddr, 2*time.Second)
		if dialErr == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if dialErr != nil {
		p.logger.Error("Failed to connect to backend %s: %v", backendAddr, dialErr)
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

// downRouteBudget bounds the whole login-to-boot hold so a held client never
// stares at a dead connection past the ~30s Java client timeout (MINE-121).
// A var (not const) so tests can shrink the hold without waiting it out.
var downRouteBudget = 25 * time.Second

// loadingMOTDWindow is how long after a boot attempt pings serve the loading
// MOTD before falling back to the asleep one.
const loadingMOTDWindow = 90 * time.Second

// handleDownRoute serves or boots a deeply-asleep backend (MINE-121).
// Status pings receive a cached MOTD (asleep vs loading) and hang up without
// touching Docker. Logins trigger a boot via SleepWakeHandler and are held
// until the SLP health gate passes. Returns true when the caller may relay.
func (p *MinecraftProxy) handleDownRoute(clientConn net.Conn, handshake *HandshakePacket, hostname string, route *Route) bool {
	if handshake.NextState != 2 {
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

	if p.sleepWakeHandler == nil {
		p.logger.Debug("Login for down route %s with no sleep wake handler; hanging up", hostname)
		return false
	}

	deadline := time.Now().Add(downRouteBudget)
	wakeCtx, wakeCancel := context.WithTimeout(p.ctx, downRouteBudget)
	defer wakeCancel()

	p.routesMutex.Lock()
	p.waking[hostname] = time.Now()
	p.routesMutex.Unlock()

	p.logger.Info("Login intent for deeply-asleep server %s (host %s), booting...", route.ServerID, hostname)
	if err := p.sleepWakeHandler(wakeCtx, route.ServerID); err != nil {
		p.logger.Error("Failed to boot deeply-asleep server %s: %v", route.ServerID, err)
		p.routesMutex.Lock()
		delete(p.waking, hostname)
		p.routesMutex.Unlock()
		_ = sendLoginDisconnect(clientConn, wakeFailedMessage)
		return false
	}

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
