package proxy

import (
	"context"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

// Proxier is the interface for all proxy types (TCP, UDP, Minecraft, HTTP)
type Proxier interface {
	Start() error
	Stop() error
	AddRoute(serverID, hostname, backendHost string, backendPort int)
	RemoveRoute(hostname string)
	UpdateRoute(hostname, backendHost string, backendPort int)
	SetRouteHibernated(hostname string, hibernated bool)
	GetRoutes() map[string]*Route
	IsRunning() bool
}

// Route represents a routing rule from hostname to backend server
type Route struct {
	ServerID    string
	Hostname    string
	BackendHost string
	BackendPort int
	Active      bool
	Hibernated  bool
}

// WakeHandler defines a callback for waking/unfreezing a hibernated server container
type WakeHandler func(ctx context.Context, serverID string) error

// ActivityHandler defines a callback fired when a client shows real login
// intent (handshake NextState=2) for a server. Status pings are excluded so
// scanners and server-list refreshes can never hold a server awake (MINE-120).
type ActivityHandler func(serverID string)

// Config holds proxy configuration
type Config struct {
	ListenAddr      string // Address to listen on (e.g., ":25565" or ":8080")
	Logger          *logger.Logger
	ProxyProtocol   bool            // Whether PROXY protocol v2 support is enabled on this listener
	WakeHandler     WakeHandler     // Callback to unpause hibernated container upon player connect (MINE-18)
	ActivityHandler ActivityHandler // Callback to reset idle tracking upon login intent (MINE-120)
}
