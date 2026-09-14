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

// Config holds proxy configuration
type Config struct {
	ListenAddr    string // Address to listen on (e.g., ":25565" or ":8080")
	Logger        *logger.Logger
	ProxyProtocol bool // Whether PROXY protocol v2 support is enabled on this listener
	WakeHandler   WakeHandler // Callback to unpause hibernated container upon player connect (MINE-18)
}
