// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package proxy

import (
	"context"
	"errors"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

func TestPlanFor(t *testing.T) {
	local := &db.Node{ID: "local", IsLocal: true}
	listener := &db.ProxyListener{ID: "l1", Port: 25565, Enabled: true}

	server := func(status db.ServerStatus) *db.Server {
		return &db.Server{
			ID:              "srv-1",
			Name:            "survival",
			NodeID:          "local",
			Status:          status,
			ProxyHostname:   "survival.mc.test",
			ProxyListenerID: "l1",
			ProxyPort:       25565,
			Port:            25565,
		}
	}
	base := func(status db.ServerStatus) PlanInput {
		return PlanInput{
			Server:       server(status),
			Node:         local,
			Listener:     listener,
			Hostname:     "survival.mc.test",
			Backend:      Backend{Host: "172.17.0.4", Port: 25565},
			ProxyEnabled: true,
			HasProxies:   true,
		}
	}

	tests := []struct {
		name string
		in   PlanInput
		want RoutePlan
	}{
		{
			name: "nil server",
			in:   PlanInput{},
			want: RoutePlan{},
		},
		{
			name: "running local upserts",
			in:   base(db.StatusRunning),
			want: RoutePlan{Action: PlanUpsert, ServerID: "srv-1", ServerName: "survival", Hostname: "survival.mc.test", Backend: Backend{Host: "172.17.0.4", Port: 25565}, ListenerPort: 25565},
		},
		{
			name: "starting local upserts",
			in:   base(db.StatusStarting),
			want: RoutePlan{Action: PlanUpsert, ServerID: "srv-1", ServerName: "survival", Hostname: "survival.mc.test", Backend: Backend{Host: "172.17.0.4", Port: 25565}, ListenerPort: 25565},
		},
		{
			name: "paused local marks hibernated",
			in:   base(db.StatusPaused),
			want: RoutePlan{Action: PlanUpsert, ServerID: "srv-1", ServerName: "survival", Hostname: "survival.mc.test", Backend: Backend{Host: "172.17.0.4", Port: 25565}, Hibernated: true, ListenerPort: 25565},
		},
		{
			name: "deep sleep with backend marks down",
			in:   base(db.StatusDeepSleep),
			want: RoutePlan{Action: PlanUpsert, ServerID: "srv-1", ServerName: "survival", Hostname: "survival.mc.test", Backend: Backend{Host: "172.17.0.4", Port: 25565}, Down: true, ListenerPort: 25565},
		},
		{
			name: "deep sleep without backend retains down route",
			in: func() PlanInput {
				in := base(db.StatusDeepSleep)
				in.Backend = Backend{}
				in.BackendErr = errors.New("no container")
				return in
			}(),
			want: RoutePlan{Action: PlanDown, ServerID: "srv-1", ServerName: "survival", Hostname: "survival.mc.test", Down: true, ListenerPort: 25565},
		},
		{
			name: "running backend failure surfaces",
			in: func() PlanInput {
				in := base(db.StatusRunning)
				in.BackendErr = errors.New("inspect failed")
				return in
			}(),
			want: RoutePlan{ServerID: "srv-1", ServerName: "survival", Hostname: "survival.mc.test", ListenerPort: 25565, BackendErr: errors.New("inspect failed")},
		},
		{
			name: "stopped removes",
			in:   base(db.StatusStopped),
			want: RoutePlan{Action: PlanRemove, ServerID: "srv-1", ServerName: "survival", Hostname: "survival.mc.test", ListenerPort: 25565},
		},
		{
			name: "stopping removes",
			in:   base(db.StatusStopping),
			want: RoutePlan{Action: PlanRemove, ServerID: "srv-1", ServerName: "survival", Hostname: "survival.mc.test", ListenerPort: 25565},
		},
		{
			name: "error removes",
			in:   base(db.StatusError),
			want: RoutePlan{Action: PlanRemove, ServerID: "srv-1", ServerName: "survival", Hostname: "survival.mc.test", ListenerPort: 25565},
		},
		{
			name: "creating has no route action",
			in:   base(db.StatusCreating),
			want: RoutePlan{ServerID: "srv-1", ServerName: "survival", Hostname: "survival.mc.test", ListenerPort: 25565},
		},
		{
			name: "unhealthy has no route action",
			in:   base(db.StatusUnhealthy),
			want: RoutePlan{ServerID: "srv-1", ServerName: "survival", Hostname: "survival.mc.test", ListenerPort: 25565},
		},
		{
			name: "disabled listener is a no-op",
			in: func() PlanInput {
				in := base(db.StatusRunning)
				in.Listener = &db.ProxyListener{ID: "l1", Port: 25565, Enabled: false}
				return in
			}(),
			want: RoutePlan{ServerID: "srv-1", ServerName: "survival", Hostname: "survival.mc.test"},
		},
		{
			name: "listener lookup failure surfaces",
			in: func() PlanInput {
				in := base(db.StatusRunning)
				in.Listener = nil
				in.ListenerErr = errors.New("listener unavailable")
				return in
			}(),
			want: RoutePlan{ServerID: "srv-1", ServerName: "survival", Hostname: "survival.mc.test", ListenerErr: errors.New("listener unavailable")},
		},
		{
			name: "empty proxy hostname has no route action but owns a knock listener",
			in: func() PlanInput {
				in := base(db.StatusRunning)
				in.Server.ProxyHostname = ""
				return in
			}(),
			want: RoutePlan{Knock: KnockStop, ServerID: "srv-1", ServerName: "survival", Hostname: "survival.mc.test", ListenerPort: 25565},
		},
		{
			name: "no proxies leaves policy inert",
			in: func() PlanInput {
				in := base(db.StatusRunning)
				in.HasProxies = false
				return in
			}(),
			want: RoutePlan{ServerID: "srv-1", ServerName: "survival", Hostname: "survival.mc.test"},
		},
		{
			name: "proxy disabled leaves policy inert",
			in: func() PlanInput {
				in := base(db.StatusRunning)
				in.ProxyEnabled = false
				return in
			}(),
			want: RoutePlan{ServerID: "srv-1", ServerName: "survival", Hostname: "survival.mc.test", Knock: KnockStop},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PlanFor(tt.in)
			if got.Action != tt.want.Action || got.Knock != tt.want.Knock {
				t.Fatalf("action/knock = %v/%v, want %v/%v", got.Action, got.Knock, tt.want.Action, tt.want.Knock)
			}
			if got.ServerID != tt.want.ServerID || got.ServerName != tt.want.ServerName || got.Hostname != tt.want.Hostname {
				t.Fatalf("identity = %q/%q/%q, want %q/%q/%q", got.ServerID, got.ServerName, got.Hostname, tt.want.ServerID, tt.want.ServerName, tt.want.Hostname)
			}
			if got.Backend != tt.want.Backend {
				t.Fatalf("backend = %+v, want %+v", got.Backend, tt.want.Backend)
			}
			if got.Hibernated != tt.want.Hibernated || got.Down != tt.want.Down {
				t.Fatalf("flags = hibernated:%v down:%v, want %v/%v", got.Hibernated, got.Down, tt.want.Hibernated, tt.want.Down)
			}
			if got.ListenerPort != tt.want.ListenerPort {
				t.Fatalf("listener port = %d, want %d", got.ListenerPort, tt.want.ListenerPort)
			}
			if (got.ListenerErr != nil) != (tt.want.ListenerErr != nil) {
				t.Fatalf("listener error = %v, want %v", got.ListenerErr, tt.want.ListenerErr)
			}
			if (got.BackendErr != nil) != (tt.want.BackendErr != nil) {
				t.Fatalf("backend error = %v, want %v", got.BackendErr, tt.want.BackendErr)
			}
		})
	}
}

func TestPlanFor_DirectPortKnock(t *testing.T) {
	server := &db.Server{ID: "srv", Name: "direct", NodeID: "local", Status: db.StatusDeepSleep, Port: 25565}
	local := &db.Node{ID: "local", IsLocal: true}
	remote := &db.Node{ID: "remote", Host: "tcp://10.0.0.5:2376"}

	tests := []struct {
		name string
		in   PlanInput
		want KnockAction
	}{
		{
			name: "deep sleep local starts knock",
			in:   PlanInput{Server: server, Node: local, ProxyEnabled: false, HasProxies: false},
			want: KnockStart,
		},
		{
			name: "deep sleep remote stops knock",
			in:   PlanInput{Server: server, Node: remote, ProxyEnabled: false, HasProxies: false},
			want: KnockStop,
		},
		{
			name: "awake local stops knock",
			in:   PlanInput{Server: &db.Server{ID: "srv", NodeID: "local", Status: db.StatusRunning, Port: 25565}, Node: local, ProxyEnabled: false},
			want: KnockStop,
		},
		{
			name: "proxied server does not own a knock listener",
			in:   PlanInput{Server: &db.Server{ID: "srv", NodeID: "local", Status: db.StatusDeepSleep, Port: 25565, ProxyHostname: "x", ProxyListenerID: "l", ProxyPort: 25565}, Node: local, ProxyEnabled: true},
			want: KnockNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PlanFor(tt.in).Knock; got != tt.want {
				t.Fatalf("knock = %v, want %v", got, tt.want)
			}
		})
	}
}

type recordingProxier struct {
	routes     map[string]*Route
	hibernated map[string]bool
	down       map[string]bool
}

func newRecordingProxier() *recordingProxier {
	return &recordingProxier{
		routes:     make(map[string]*Route),
		hibernated: make(map[string]bool),
		down:       make(map[string]bool),
	}
}

func (p *recordingProxier) Start() error { return nil }
func (p *recordingProxier) Stop() error  { return nil }
func (p *recordingProxier) AddRoute(serverID, hostname, backendHost string, backendPort int) {
	p.routes[hostname] = &Route{ServerID: serverID, Hostname: hostname, BackendHost: backendHost, BackendPort: backendPort}
}
func (p *recordingProxier) RemoveRoute(hostname string) { delete(p.routes, hostname) }
func (p *recordingProxier) UpdateRoute(hostname, backendHost string, backendPort int) {
	if r, ok := p.routes[hostname]; ok {
		r.BackendHost = backendHost
		r.BackendPort = backendPort
	}
}
func (p *recordingProxier) SetRouteHibernated(hostname string, hibernated bool) {
	p.hibernated[hostname] = hibernated
}
func (p *recordingProxier) SetRouteDown(hostname string, down bool) { p.down[hostname] = down }
func (p *recordingProxier) GetRoutes() map[string]*Route            { return p.routes }
func (p *recordingProxier) IsRunning() bool                         { return true }

func TestApply(t *testing.T) {
	proxier := newRecordingProxier()
	m := &Manager{
		proxies:         map[int]Proxier{25565: proxier},
		config:          &config.ProxyConfig{Enabled: true},
		logger:          logger.New(),
		serverHostnames: make(map[string]string),
	}
	ctx := context.Background()

	upsert := RoutePlan{
		Action:       PlanUpsert,
		ServerID:     "srv-1",
		ServerName:   "survival",
		Hostname:     "survival.mc.test",
		Backend:      Backend{Host: "172.17.0.4", Port: 25565},
		Hibernated:   true,
		ListenerPort: 25565,
	}
	if err := m.Apply(ctx, upsert); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	route, ok := proxier.routes["survival.mc.test"]
	if !ok || route.BackendHost != "172.17.0.4" || route.BackendPort != 25565 {
		t.Fatalf("route = %+v", route)
	}
	if !proxier.hibernated["survival.mc.test"] {
		t.Fatal("upsert did not set hibernated")
	}
	if m.serverHostnames["srv-1"] != "survival.mc.test" {
		t.Fatal("hostname cache not updated")
	}

	down := RoutePlan{
		Action:       PlanDown,
		ServerID:     "srv-1",
		ServerName:   "survival",
		Hostname:     "survival.mc.test",
		ListenerPort: 25565,
	}
	if err := m.Apply(ctx, down); err != nil {
		t.Fatalf("down: %v", err)
	}
	if !proxier.down["survival.mc.test"] {
		t.Fatal("down plan did not mark route down")
	}
	if proxier.hibernated["survival.mc.test"] {
		t.Fatal("down plan should clear hibernated")
	}

	remove := RoutePlan{
		Action:       PlanRemove,
		ServerID:     "srv-1",
		ServerName:   "survival",
		Hostname:     "survival.mc.test",
		ListenerPort: 25565,
	}
	if err := m.Apply(ctx, remove); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok := proxier.routes["survival.mc.test"]; ok {
		t.Fatal("remove plan left the route in place")
	}
}
