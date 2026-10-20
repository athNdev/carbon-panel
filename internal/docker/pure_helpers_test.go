// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package docker

import (
	"testing"

	models "github.com/athNdev/carbon-panel/internal/db"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"github.com/moby/moby/api/types/container"
)

func TestParseDNSAddrs(t *testing.T) {
	addrs := parseDNSAddrs([]string{"8.8.8.8", "1.1.1.1"})
	if len(addrs) != 2 || addrs[0].String() != "8.8.8.8" || addrs[1].String() != "1.1.1.1" {
		t.Errorf("parseDNSAddrs = %v", addrs)
	}
	if got := parseDNSAddrs(nil); len(got) != 0 {
		t.Errorf("parseDNSAddrs(nil) = %v, want empty", got)
	}
	// Invalid entries are skipped, valid ones survive.
	addrs = parseDNSAddrs([]string{"not-an-ip", "9.9.9.9", " 8.8.4.4 "})
	if len(addrs) != 2 || addrs[0].String() != "9.9.9.9" || addrs[1].String() != "8.8.4.4" {
		t.Errorf("parseDNSAddrs with invalid = %v", addrs)
	}
	// IPv6 passes through.
	addrs = parseDNSAddrs([]string{"2001:4860:4860::8888"})
	if len(addrs) != 1 || addrs[0].String() != "2001:4860:4860::8888" {
		t.Errorf("parseDNSAddrs v6 = %v", addrs)
	}
}

func TestTranslateToHostPath(t *testing.T) {
	t.Setenv("CARBONPANEL_HOST_DATA_PATH", "")
	if got := TranslateToHostPath("/data/world"); got != "/data/world" {
		t.Errorf("no env = %q", got)
	}

	t.Setenv("CARBONPANEL_HOST_DATA_PATH", "/host/data")
	t.Setenv("CARBONPANEL_DATA_DIR", "/app/data")
	if got := TranslateToHostPath("/app/data/world"); got != "/host/data/world" {
		t.Errorf("translated = %q", got)
	}
	// Outside the container data dir: returned as-is.
	if got := TranslateToHostPath("/etc/passwd"); got != "/etc/passwd" {
		t.Errorf("outside = %q", got)
	}
}

func TestClientIsRemote(t *testing.T) {
	for _, host := range []string{"tcp://10.0.0.8:2376", "ssh://minecraft@node", "https://node.example"} {
		if (&Client{host: host}).IsRemote() != true {
			t.Errorf("IsRemote(%q) = false, want true", host)
		}
	}
	for _, host := range []string{"", "unix:///var/run/docker.sock", "npipe:////./pipe/docker_engine"} {
		if (&Client{host: host}).IsRemote() {
			t.Errorf("IsRemote(%q) = true, want false", host)
		}
	}
}

func TestExtractCurseForgeSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"https://www.curseforge.com/minecraft/modpacks/all-the-mods-9", "all-the-mods-9"},
		{"https://www.curseforge.com/minecraft/mc-mods/sodium", "sodium"},
		{"just-a-slug", "just-a-slug"},
		{"https://example.com/not-a-modpack", ""},
	}
	for _, tc := range cases {
		if got := extractCurseForgeSlug(tc.in); got != tc.want {
			t.Errorf("slug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExtractCurseForgeFileID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"https://www.curseforge.com/minecraft/modpacks/all-the-mods-9/files/1234567", "1234567"},
		{"no-file-here", ""},
	}
	for _, tc := range cases {
		if got := extractCurseForgeFileID(tc.in); got != tc.want {
			t.Errorf("fileID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestInspectToSummary(t *testing.T) {
	summary := inspectToSummary(container.InspectResponse{
		ID:    "abc123",
		Name:  "/carbon-panel-server-x",
		Image: "itzg/minecraft-server:stable",
		Config: &container.Config{
			Image:  "itzg/minecraft-server:stable",
			Labels: map[string]string{"carbon-panel.server.id": "srv-1"},
		},
		State: &container.State{Status: "running"},
	})
	if summary.ID != "abc123" || summary.Image != "itzg/minecraft-server:stable" {
		t.Errorf("summary = %+v", summary)
	}
	if len(summary.Names) != 1 || summary.Names[0] != "/carbon-panel-server-x" {
		t.Errorf("names = %v", summary.Names)
	}
	if string(summary.State) != "running" || summary.Status != "running" {
		t.Errorf("state/status = %q/%q", summary.State, summary.Status)
	}
	if summary.Labels["carbon-panel.server.id"] != "srv-1" {
		t.Errorf("labels = %v", summary.Labels)
	}

	// Nil config/state must not panic and yields zero values.
	bare := inspectToSummary(container.InspectResponse{ID: "x"})
	if bare.ID != "x" || len(bare.Names) != 0 {
		t.Errorf("bare summary = %+v", bare)
	}
}

func TestMapContainerState(t *testing.T) {
	cases := []struct {
		state *container.State
		want  models.ServerStatus
	}{
		{nil, models.StatusError},
		{&container.State{Status: "running"}, models.StatusRunning},
		{&container.State{Status: "running", Health: &container.Health{Status: "healthy"}}, models.StatusRunning},
		{&container.State{Status: "running", Health: &container.Health{Status: "starting"}}, models.StatusStarting},
		{&container.State{Status: "running", Health: &container.Health{Status: "unhealthy"}}, models.StatusUnhealthy},
		{&container.State{Status: "restarting"}, models.StatusStarting},
		{&container.State{Status: "exited"}, models.StatusStopped},
		{&container.State{Status: "dead"}, models.StatusStopped},
		{&container.State{Status: "created"}, models.StatusStopped},
		{&container.State{Status: "paused"}, models.StatusPaused},
		{&container.State{Status: "weird"}, models.StatusError},
	}
	for _, tc := range cases {
		if got := mapContainerState(tc.state); got != tc.want {
			t.Errorf("mapContainerState(%+v) = %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestValidateServerPorts(t *testing.T) {
	valid := &models.Server{ID: "s", Port: 25565}
	if err := validateServerPorts(valid); err != nil {
		t.Errorf("valid ports rejected: %v", err)
	}

	for name, mutate := range map[string]func(*models.Server){
		"zero game port":      func(s *models.Server) { s.Port = 0 },
		"oversize game port":  func(s *models.Server) { s.Port = 70000 },
		"negative game port":  func(s *models.Server) { s.Port = -1 },
		"rcon overflow":       func(s *models.Server) { s.Port = 65530 },
		"bad additional cont": func(s *models.Server) { s.AdditionalPorts = portList(0, 25566) },
		"bad additional host": func(s *models.Server) { s.AdditionalPorts = portList(25566, 99999) },
		"oversize additional": func(s *models.Server) { s.AdditionalPorts = portList(70000, 25566) },
	} {
		srv := &models.Server{ID: "s", Port: 25565}
		mutate(srv)
		if err := validateServerPorts(srv); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// Proxy servers use the default internal port regardless of host port.
	proxy := &models.Server{ID: "p", Port: 25565, ProxyHostname: "mc.example.com"}
	if err := validateServerPorts(proxy); err != nil {
		t.Errorf("proxy ports rejected: %v", err)
	}
}

func portList(containerPort, hostPort int32) []*v1.AdditionalPort {
	return []*v1.AdditionalPort{{Name: "x", ContainerPort: containerPort, HostPort: hostPort, Protocol: "tcp"}}
}


func TestIsProxyServer(t *testing.T) {
	c := &Client{config: ClientConfig{ProxyEnabled: false}}

	// When proxy is disabled globally, nothing is treated as a proxied server
	srv := &models.Server{ID: "1", Port: 25566, ProxyHostname: "mc.example.com", ProxyPort: 25565}
	if c.isProxyServer(srv) {
		t.Errorf("expected isProxyServer=false when ProxyEnabled=false")
	}

	c.SetProxyEnabled(true)

	// Direct port server with no proxy port
	directSrv := &models.Server{ID: "2", Port: 25566, ProxyHostname: "mc.example.com", ProxyPort: 0}
	if c.isProxyServer(directSrv) {
		t.Errorf("expected isProxyServer=false when ProxyPort=0")
	}

	// Server without hostname
	noHostSrv := &models.Server{ID: "3", Port: 25566, ProxyHostname: "", ProxyPort: 25565}
	if c.isProxyServer(noHostSrv) {
		t.Errorf("expected isProxyServer=false when ProxyHostname is empty")
	}

	// Proxied server with listener assigned
	proxiedSrv := &models.Server{ID: "4", Port: 25565, ProxyHostname: "mc.example.com", ProxyPort: 25565}
	if !c.isProxyServer(proxiedSrv) {
		t.Errorf("expected isProxyServer=true when ProxyEnabled, hostname set, and ProxyPort > 0")
	}

	// Internal proxied server with Port 0
	internalProxiedSrv := &models.Server{ID: "5", Port: 0, ProxyHostname: "mc.example.com", ProxyPort: 0}
	if !c.isProxyServer(internalProxiedSrv) {
		t.Errorf("expected isProxyServer=true when Port=0 and ProxyHostname set")
	}
}
