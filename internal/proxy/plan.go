// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package proxy

import (
	"github.com/athNdev/carbon-panel/internal/db"
)

// PlanAction is the route mutation a plan asks the manager to perform.
type PlanAction int

const (
	PlanNone PlanAction = iota
	PlanUpsert
	PlanRemove
	PlanDown
)

// KnockAction is the direct-port wake listener change a plan asks for.
type KnockAction int

const (
	KnockNone KnockAction = iota
	KnockStart
	KnockStop
)

// Backend is the resolved target of a proxy route.
type Backend struct {
	Host string
	Port int
}

// PlanInput is everything route policy needs. Store and Docker lookups are
// performed by the manager before PlanFor is called, so PlanFor stays pure.
type PlanInput struct {
	Server       *db.Server
	Node         *db.Node
	Listener     *db.ProxyListener
	Hostname     string
	Backend      Backend
	BackendErr   error
	ListenerErr  error
	ProxyEnabled bool
	HasProxies   bool
}

// RoutePlan describes the complete route change for one server.
type RoutePlan struct {
	Action       PlanAction
	Knock        KnockAction
	Server       *db.Server
	ServerID     string
	ServerName   string
	Hostname     string
	Backend      Backend
	Hibernated   bool
	Down         bool
	ListenerPort int
	ListenerErr  error
	BackendErr   error
}

// PlanFor derives a route plan from already-observed state. It performs no I/O
// and does not mutate the server, node, listener or manager.
func PlanFor(in PlanInput) RoutePlan {
	plan := RoutePlan{}
	if in.Server == nil {
		return plan
	}
	plan.Server = in.Server
	plan.ServerID = in.Server.ID
	plan.ServerName = in.Server.Name
	plan.Hostname = in.Hostname

	// Direct-port wake listeners are independent of the HTTP-style route table.
	// A deeply-asleep server on the local node owns a knock listener; every
	// other direct-port state must release it.
	if isDirectPortServer(in.Server, in.ProxyEnabled) {
		if in.Server.Status == db.StatusDeepSleep && isLocalNode(in.Node) {
			plan.Knock = KnockStart
		} else {
			plan.Knock = KnockStop
		}
	}

	if !in.ProxyEnabled || !in.HasProxies {
		return plan
	}
	if in.ListenerErr != nil {
		plan.ListenerErr = in.ListenerErr
		return plan
	}
	if in.Listener == nil || !in.Listener.Enabled {
		return plan
	}
	plan.ListenerPort = in.Listener.Port

	switch in.Server.Status {
	case db.StatusRunning, db.StatusStarting, db.StatusPaused, db.StatusDeepSleep:
		if in.Server.ProxyHostname == "" {
			return plan
		}
		if in.BackendErr != nil {
			// A deeply-asleep backend has no address (stopped container, no
			// IP); retain a down route for MOTD and wake-on-login (MINE-121).
			if in.Server.Status == db.StatusDeepSleep {
				plan.Action = PlanDown
				plan.Down = true
				return plan
			}
			plan.BackendErr = in.BackendErr
			return plan
		}
		plan.Action = PlanUpsert
		plan.Backend = in.Backend
		plan.Hibernated = in.Server.Status == db.StatusPaused
		plan.Down = in.Server.Status == db.StatusDeepSleep
	case db.StatusStopped, db.StatusStopping, db.StatusError:
		plan.Action = PlanRemove
	}
	return plan
}

// isLocalNode reports whether node resolves to this controller's daemon. An
// unknown node keeps the historical local default; node resolution itself is
// covered by the dedicated locator work.
func isLocalNode(node *db.Node) bool {
	if node == nil {
		return true
	}
	return isLocalDockerNode(node)
}
