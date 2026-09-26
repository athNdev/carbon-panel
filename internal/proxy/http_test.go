// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package proxy

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

func newHTTPProxyTest(t *testing.T) *HTTPProxy {
	t.Helper()
	return NewHTTPProxy(&Config{Logger: logger.New()})
}

func TestHTTPProxy_RouteCRUD(t *testing.T) {
	p := newHTTPProxyTest(t)

	p.AddRoute("srv-1", "Example.COM:25565", "10.0.0.2", 25566)
	routes := p.GetRoutes()
	route, ok := routes["example.com"]
	if !ok {
		t.Fatal("route not found under normalized hostname")
	}
	if route.BackendHost != "10.0.0.2" || route.BackendPort != 25566 || !route.Active {
		t.Errorf("route = %+v", route)
	}

	// GetRoutes returns copies: mutating them must not affect the table.
	route.BackendPort = 1
	if p.GetRoutes()["example.com"].BackendPort != 25566 {
		t.Error("GetRoutes leaks internal pointers")
	}

	p.UpdateRoute("example.com", "10.0.0.3", 25567)
	if got := p.GetRoutes()["example.com"]; got.BackendHost != "10.0.0.3" || got.BackendPort != 25567 {
		t.Errorf("updated route = %+v", got)
	}
	// Updating a missing route is a safe no-op.
	p.UpdateRoute("missing.example", "10.0.0.9", 1)

	p.SetRouteHibernated("example.com", true)
	p.SetRouteDown("example.com", true)
	got := p.GetRoutes()["example.com"]
	if !got.Hibernated || !got.Down {
		t.Errorf("flags = %+v", got)
	}
	p.SetRouteHibernated("missing.example", true)
	p.SetRouteDown("missing.example", true)

	p.RemoveRoute("example.com")
	if _, ok := p.GetRoutes()["example.com"]; ok {
		t.Error("route not removed")
	}
	p.RemoveRoute("missing.example")
}

func TestHTTPProxy_ReverseProxiesToBackend(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("backend:" + r.URL.Path))
	}))
	defer backend.Close()

	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(backend.URL, "http://"))
	if err != nil {
		t.Fatalf("bad test backend URL: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("bad test backend port: %v", err)
	}

	p := newHTTPProxyTest(t)
	p.AddRoute("srv-1", "play.example.com", host, port)

	req := httptest.NewRequest(http.MethodGet, "http://play.example.com/status", nil)
	req.Host = "play.example.com"
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(body) != "backend:/status" {
		t.Errorf("proxied = %d %q", res.StatusCode, body)
	}
}

func TestHTTPProxy_UnknownHostIsBadGateway(t *testing.T) {
	p := newHTTPProxyTest(t)

	for _, host := range []string{"unknown.example.com", ""} {
		req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		if rec.Result().StatusCode != http.StatusBadGateway {
			t.Errorf("host %q status = %d, want 502", host, rec.Result().StatusCode)
		}
	}
}

func TestHTTPProxy_InactiveRouteIsBadGateway(t *testing.T) {
	p := newHTTPProxyTest(t)
	p.AddRoute("srv-1", "off.example.com", "10.0.0.2", 25565)
	p.routes["off.example.com"].Active = false

	req := httptest.NewRequest(http.MethodGet, "http://off.example.com/", nil)
	req.Host = "off.example.com"
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Result().StatusCode != http.StatusBadGateway {
		t.Errorf("inactive route status = %d, want 502", rec.Result().StatusCode)
	}
}

func TestIsWebSocketRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if isWebSocketRequest(req) {
		t.Error("plain request detected as websocket")
	}
	req.Header.Set("Upgrade", "websocket")
	if !isWebSocketRequest(req) {
		t.Error("upgrade request not detected")
	}
	req.Header.Set("Upgrade", "WebSocket")
	if !isWebSocketRequest(req) {
		t.Error("mixed-case upgrade not detected")
	}
}

func TestHTTPProxy_StartStopRunning(t *testing.T) {
	p := NewHTTPProxy(&Config{Logger: logger.New(), ListenAddr: "127.0.0.1:0"})
	if p.IsRunning() {
		t.Fatal("running before Start")
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if !p.IsRunning() {
		t.Fatal("not running after Start")
	}
	if err := p.Start(); err == nil {
		t.Error("second Start succeeded, want already-running error")
	}
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if p.IsRunning() {
		t.Fatal("running after Stop")
	}
	if err := p.Stop(); err != nil {
		t.Fatalf("second Stop failed: %v", err)
	}
}
