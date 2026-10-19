// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package ws

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestCheckSameOrigin_Unit(t *testing.T) {
	tests := []struct {
		name       string
		origin     string
		host       string
		xfHost     string
		xfProto    string
		tls        bool
		wantOrigin bool
	}{
		{
			name:       "no origin header allowed (CLI, native tools)",
			origin:     "",
			host:       "carbon.example.com:8080",
			wantOrigin: true,
		},
		{
			name:       "exact match with port",
			origin:     "http://carbon.example.com:8080",
			host:       "carbon.example.com:8080",
			wantOrigin: true,
		},
		{
			name:       "exact match case insensitive",
			origin:     "HTTP://Carbon.Example.COM:8080",
			host:       "carbon.example.com:8080",
			wantOrigin: true,
		},
		{
			name:       "cross-site origin rejected",
			origin:     "http://evil-attacker.com",
			host:       "carbon.example.com:8080",
			wantOrigin: false,
		},
		{
			name:       "subdomain prefix attack rejected",
			origin:     "http://carbon.example.com.evil.com",
			host:       "carbon.example.com",
			wantOrigin: false,
		},
		{
			name:       "invalid origin url rejected",
			origin:     "://invalid-url",
			host:       "carbon.example.com",
			wantOrigin: false,
		},
		{
			name:       "non-http scheme rejected",
			origin:     "javascript:alert(1)",
			host:       "carbon.example.com",
			wantOrigin: false,
		},
		{
			name:       "default http port 80 normalization",
			origin:     "http://carbon.example.com",
			host:       "carbon.example.com:80",
			wantOrigin: true,
		},
		{
			name:       "default https port 443 normalization",
			origin:     "https://carbon.example.com",
			host:       "carbon.example.com:443",
			tls:        true,
			wantOrigin: true,
		},
		{
			name:       "reverse proxy X-Forwarded-Host matching",
			origin:     "https://panel.production.com",
			host:       "10.0.1.5:8080",
			xfHost:     "panel.production.com",
			xfProto:    "https",
			wantOrigin: true,
		},
		{
			name:       "reverse proxy X-Forwarded-Host mismatch rejected",
			origin:     "https://evil.production.com",
			host:       "10.0.1.5:8080",
			xfHost:     "panel.production.com",
			xfProto:    "https",
			wantOrigin: false,
		},
		{
			name:       "reverse proxy multi-hop X-Forwarded-Host uses client host",
			origin:     "https://panel.production.com",
			host:       "10.0.1.5:8080",
			xfHost:     "panel.production.com, proxy2.internal",
			xfProto:    "https",
			wantOrigin: true,
		},
		{
			name:       "local development loopback match (127.0.0.1 to localhost)",
			origin:     "http://localhost:5173",
			host:       "127.0.0.1:8080",
			wantOrigin: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/ws", nil)
			req.Host = tt.host
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.xfHost != "" {
				req.Header.Set("X-Forwarded-Host", tt.xfHost)
			}
			if tt.xfProto != "" {
				req.Header.Set("X-Forwarded-Proto", tt.xfProto)
			}
			if tt.tls {
				req.TLS = &tls.ConnectionState{}
			}

			got := checkSameOrigin(req)
			if got != tt.wantOrigin {
				t.Errorf("checkSameOrigin() = %v, want %v", got, tt.wantOrigin)
			}
		})
	}
}

func TestServeHTTP_CSWSH_OriginValidation(t *testing.T) {
	hub, token := newHandshakeTestHub(t, false)
	ts := httptest.NewServer(hub)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?token=" + token

	// 1. Same-origin connects successfully
	validOrigin := ts.URL
	conn, res, err := websocket.DefaultDialer.Dial(wsURL, http.Header{
		"Origin": []string{validOrigin},
	})
	if err != nil {
		t.Fatalf("same-origin dial failed: %v", err)
	}
	_ = conn.Close()

	// 2. Cross-origin request is rejected with 403 Forbidden
	evilOrigin := "http://malicious-website.example.com"
	_, res, err = websocket.DefaultDialer.Dial(wsURL, http.Header{
		"Origin": []string{evilOrigin},
	})
	if err == nil {
		t.Fatal("expected cross-origin dial to fail, but it succeeded")
	}
	if res == nil || res.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for CSWSH attempt, got %v", res)
	}

	// 3. Custom check origin can override
	hub.SetCheckOrigin(func(r *http.Request) bool {
		return true // Allow all for this custom override
	})
	conn, _, err = websocket.DefaultDialer.Dial(wsURL, http.Header{
		"Origin": []string{evilOrigin},
	})
	if err != nil {
		t.Fatalf("dial failed with custom check origin override: %v", err)
	}
	_ = conn.Close()
}
