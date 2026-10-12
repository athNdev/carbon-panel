// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

// startRawBackend runs a minimal TCP server that answers one HTTP upgrade
// with a canned 101 response followed by a payload, then closes.
func startRawBackend(t *testing.T) (host string, port int) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("backend listen failed: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	addr := ln.Addr().String()
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ = strconv.Atoi(portStr)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Consume the forwarded upgrade request headers.
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil || line == "\r\n" {
				break
			}
		}
		conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"))
		conn.Write([]byte("backend-hello"))
		time.Sleep(200 * time.Millisecond)
	}()
	return host, port
}

func TestHTTPProxy_WebSocketPassthrough(t *testing.T) {
	backendHost, backendPort := startRawBackend(t)

	p := NewHTTPProxy(&Config{Logger: logger.New()})
	p.AddRoute("srv-1", "ws.example.com", backendHost, backendPort)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen failed: %v", err)
	}
	defer ln.Close()
	go http.Serve(ln, p) //nolint:errcheck

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial proxy failed: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	fmt.Fprintf(conn, "GET /ws HTTP/1.1\r\nHost: ws.example.com\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n")

	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("no handshake response: %v", err)
	}
	if !strings.Contains(status, "101") {
		t.Fatalf("handshake status = %q, want 101", status)
	}
	// Drain headers, then expect the backend payload through the tunnel.
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("header read failed: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}
	buf := make([]byte, len("backend-hello"))
	if _, err := io.ReadFull(reader, buf); err != nil {
		t.Fatalf("payload read failed: %v", err)
	}
	if string(buf) != "backend-hello" {
		t.Errorf("tunneled payload = %q", buf)
	}
}

func TestHTTPProxy_WebSocketBackendDown(t *testing.T) {
	p := NewHTTPProxy(&Config{Logger: logger.New()})
	// Port 1 is never open: backend dial fails fast.
	p.AddRoute("srv-1", "ws.example.com", "127.0.0.1", 1)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen failed: %v", err)
	}
	defer ln.Close()
	go http.Serve(ln, p) //nolint:errcheck

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial proxy failed: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	fmt.Fprintf(conn, "GET /ws HTTP/1.1\r\nHost: ws.example.com\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")

	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("no response: %v", err)
	}
	if !strings.Contains(status, "502") {
		t.Errorf("status = %q, want 502", status)
	}
}

func TestHTTPProxy_WebSocketNoHijackIs500(t *testing.T) {
	p := NewHTTPProxy(&Config{Logger: logger.New()})
	p.AddRoute("srv-1", "ws.example.com", "127.0.0.1", 1)

	// httptest.ResponseRecorder does not implement http.Hijacker.
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Host = "ws.example.com"
	req.Header.Set("Upgrade", "websocket")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Result().StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Result().StatusCode)
	}
}
