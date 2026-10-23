// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package testharness contains opt-in Docker API fault-injection helpers.
package testharness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// FaultKind describes where the proxy interrupts a Docker API exchange.
type FaultKind string

const (
	// DropBefore closes the client connection without sending a request upstream.
	DropBefore FaultKind = "drop-before"
	// DelayBefore waits before forwarding; a client deadline can cancel the request
	// before the daemon sees it.
	DelayBefore FaultKind = "delay-before"
	// DropAfter forwards the full request, consumes the daemon response, then drops
	// the client connection. Mutations may have committed and the result is ambiguous.
	DropAfter FaultKind = "drop-after"
	// DelayAfter forwards the request, waits for the daemon response, then delays
	// delivery. A client timeout at this point is also an ambiguous result.
	DelayAfter FaultKind = "delay-after"
	// PullErrorStream appends a Docker JSON progress/error frame to an image-pull
	// stream while preserving the HTTP status and streaming response shape.
	PullErrorStream FaultKind = "pull-error-stream"
	// DisconnectOnce drops the first matching streaming request. Later requests pass.
	DisconnectOnce FaultKind = "disconnect-once"
)

// Rule matches a request and applies Kind at most Times times. A non-positive
// Times means once. PathPrefix should generally include Docker's /vX.Y prefix.
type Rule struct {
	Method     string
	PathPrefix string
	Kind       FaultKind
	Times      int
	Delay      time.Duration
	Error      string
}

type faultState struct {
	rule  Rule
	count int
}

// Proxy forwards Docker Engine HTTP requests to Target and injects configured
// faults. It does not understand or rewrite Docker API bodies except for the
// explicit pull error stream rule.
type Proxy struct {
	Target    *url.URL
	Listener  net.Listener
	server    *http.Server
	transport *http.Transport

	mu    sync.Mutex
	rules []faultState
}

// NewProxy binds a loopback listener and starts a protocol-level HTTP proxy.
// Target must be an HTTP(S) Docker Engine API endpoint (not a CLI context name).
func NewProxy(target string, rules []Rule) (*Proxy, error) {
	u, err := url.Parse(target)
	if err == nil && u.Scheme == "tcp" {
		// Docker context endpoints commonly use tcp:// for plain Engine HTTP.
		u.Scheme = "http"
	}
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid Docker API target %q: expected tcp, http, or https URL", target)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for fault proxy: %w", err)
	}
	p := &Proxy{Target: u, Listener: ln, transport: &http.Transport{Proxy: nil, ForceAttemptHTTP2: false}}
	for _, rule := range rules {
		if err := validateRule(rule); err != nil {
			_ = ln.Close()
			return nil, err
		}
		p.rules = append(p.rules, faultState{rule: rule})
	}
	p.server = &http.Server{Handler: http.HandlerFunc(p.serveHTTP), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = p.server.Serve(ln) }()
	return p, nil
}

func validateRule(r Rule) error {
	switch r.Kind {
	case DropBefore, DelayBefore, DropAfter, DelayAfter, PullErrorStream, DisconnectOnce:
	default:
		return fmt.Errorf("unknown Docker fault kind %q", r.Kind)
	}
	if r.Kind == PullErrorStream && !strings.Contains(r.PathPrefix, "/images/create") {
		return errors.New("pull-error-stream requires a path prefix containing /images/create")
	}
	if (r.Kind == DelayBefore || r.Kind == DelayAfter) && r.Delay <= 0 {
		return fmt.Errorf("fault %q requires a positive delay", r.Kind)
	}
	return nil
}

// URL is the loopback URL for the proxy.
func (p *Proxy) URL() string { return "http://" + p.Listener.Addr().String() }

// Close stops the listener and releases transport connections.
func (p *Proxy) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := p.server.Shutdown(ctx)
	p.transport.CloseIdleConnections()
	return err
}

func (p *Proxy) match(r *http.Request) *Rule {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.rules {
		state := &p.rules[i]
		if state.rule.Method != "" && state.rule.Method != r.Method {
			continue
		}
		if state.rule.PathPrefix != "" && !strings.HasPrefix(r.URL.Path, state.rule.PathPrefix) {
			continue
		}
		limit := state.rule.Times
		if limit <= 0 {
			limit = 1
		}
		if state.count >= limit {
			continue
		}
		state.count++
		copy := state.rule
		return &copy
	}
	return nil
}

func (p *Proxy) serveHTTP(w http.ResponseWriter, r *http.Request) {
	rule := p.match(r)
	if rule != nil {
		switch rule.Kind {
		case DropBefore, DisconnectOnce:
			dropConnection(w)
			return
		case DelayBefore:
			if !waitRequest(r, rule.Delay) {
				dropConnection(w)
				return
			}
		}
	}
	upstreamReq := r.Clone(r.Context())
	upstreamReq.URL = cloneURL(r.URL)
	upstreamReq.URL.Scheme = p.Target.Scheme
	upstreamReq.URL.Host = p.Target.Host
	upstreamReq.Host = p.Target.Host
	if p.Target.Path != "" && p.Target.Path != "/" {
		upstreamReq.URL.Path = strings.TrimRight(p.Target.Path, "/") + "/" + strings.TrimLeft(r.URL.Path, "/")
	}
	stripHopHeaders(upstreamReq.Header)
	resp, err := p.transport.RoundTrip(upstreamReq)
	if err != nil {
		dropConnection(w)
		return
	}
	defer resp.Body.Close()
	if rule != nil && (rule.Kind == DropAfter || rule.Kind == DelayAfter) {
		_, _ = io.Copy(io.Discard, resp.Body)
		if rule.Kind == DelayAfter {
			_ = waitRequest(r, rule.Delay)
		}
		dropConnection(w)
		return
	}
	copyHeaders(w.Header(), resp.Header)
	if rule != nil && rule.Kind == PullErrorStream {
		w.Header().Del("Content-Length")
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.StatusCode)
	if rule != nil && rule.Kind == PullErrorStream {
		if _, err := io.Copy(w, resp.Body); err != nil {
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		message := rule.Error
		if message == "" {
			message = "injected image pull stream failure"
		}
		frame, _ := json.Marshal(map[string]any{"error": message, "errorDetail": map[string]any{"message": message}})
		_, _ = w.Write(append(frame, '\n'))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		return
	}
	_, _ = io.Copy(w, resp.Body)
}

func waitRequest(r *http.Request, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-r.Context().Done():
		return false
	case <-timer.C:
		return r.Context().Err() == nil
	}
}

func dropConnection(w http.ResponseWriter) {
	h, ok := w.(http.Hijacker)
	if !ok {
		return
	}
	conn, _, err := h.Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

func cloneURL(u *url.URL) *url.URL { c := *u; return &c }

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
	stripHopHeaders(dst)
}

func stripHopHeaders(h http.Header) {
	for _, key := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		h.Del(key)
	}
}
