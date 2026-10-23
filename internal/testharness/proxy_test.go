// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package testharness

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestProxy(t *testing.T, upstream http.Handler, rules ...Rule) (*httptest.Server, *Proxy) {
	t.Helper()
	server := httptest.NewServer(upstream)
	proxy, err := NewProxy(server.URL, rules)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proxy.Close(); server.Close() })
	return server, proxy
}

func TestDropBeforeDoesNotReachDaemon(t *testing.T) {
	var mutations atomic.Int32
	_, proxy := newTestProxy(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { mutations.Add(1); w.WriteHeader(http.StatusCreated) }), Rule{Method: http.MethodPost, PathPrefix: "/v1.56/containers/create", Kind: DropBefore})
	resp, err := (&http.Client{}).Post(proxy.URL()+"/v1.56/containers/create", "application/json", strings.NewReader(`{}`))
	if err == nil {
		if resp != nil {
			resp.Body.Close()
		}
		t.Fatal("request unexpectedly received a response")
	}
	if got := mutations.Load(); got != 0 {
		t.Fatalf("upstream mutations = %d, want 0", got)
	}
}

func TestDropAfterMutationHasAmbiguousOutcome(t *testing.T) {
	var mutations atomic.Int32
	_, proxy := newTestProxy(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mutations.Add(1)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"Id":"created"}`)
	}), Rule{Method: http.MethodPost, PathPrefix: "/v1.56/containers/create", Kind: DropAfter})
	resp, err := (&http.Client{}).Post(proxy.URL()+"/v1.56/containers/create", "application/json", strings.NewReader(`{}`))
	if err == nil {
		if resp != nil {
			resp.Body.Close()
		}
		t.Fatal("response loss should be observable by caller")
	}
	if got := mutations.Load(); got != 1 {
		t.Fatalf("upstream mutation count = %d, want 1 despite caller error", got)
	}
}

func TestDelayBeforeHonorsCallerTimeoutBeforeForward(t *testing.T) {
	var requests atomic.Int32
	_, proxy := newTestProxy(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(http.StatusOK) }), Rule{Kind: DelayBefore, Delay: 250 * time.Millisecond})
	client := &http.Client{Timeout: 40 * time.Millisecond}
	resp, err := client.Get(proxy.URL() + "/v1.56/info")
	if err == nil {
		if resp != nil {
			resp.Body.Close()
		}
		t.Fatal("expected client timeout")
	}
	time.Sleep(280 * time.Millisecond)
	if got := requests.Load(); got != 0 {
		t.Fatalf("upstream requests = %d after caller timeout, want 0", got)
	}
}

func TestDelayAfterMutationTimeoutIsAmbiguous(t *testing.T) {
	var mutations atomic.Int32
	_, proxy := newTestProxy(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { mutations.Add(1); w.WriteHeader(http.StatusCreated) }), Rule{Kind: DelayAfter, Delay: 250 * time.Millisecond})
	client := &http.Client{Timeout: 40 * time.Millisecond}
	resp, err := client.Post(proxy.URL()+"/v1.56/containers/create", "application/json", strings.NewReader(`{}`))
	if err == nil {
		if resp != nil {
			resp.Body.Close()
		}
		t.Fatal("expected client timeout")
	}
	if got := mutations.Load(); got != 1 {
		t.Fatalf("upstream mutation count = %d, want 1", got)
	}
}

func TestPullErrorStreamAppendsDockerErrorFrame(t *testing.T) {
	_, proxy := newTestProxy(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"Downloading"}`+"\n")
	}), Rule{Method: http.MethodPost, PathPrefix: "/v1.56/images/create", Kind: PullErrorStream, Error: "injected registry failure"})
	resp, err := http.Post(proxy.URL()+"/v1.56/images/create?fromImage=fixture", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), `"status":"Downloading"`) || !strings.Contains(string(body), `"errorDetail":{"message":"injected registry failure"}`) {
		t.Fatalf("body is not a Docker JSON progress/error stream: %s", body)
	}
	if resp.ContentLength >= 0 {
		t.Fatalf("injected response should be streamed/chunked, Content-Length=%d", resp.ContentLength)
	}
}

func TestDisconnectOnceAllowsReconnect(t *testing.T) {
	var eventsRequests atomic.Int32
	_, proxy := newTestProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		eventsRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`+"\n")
	}), Rule{Method: http.MethodGet, PathPrefix: "/v1.56/events", Kind: DisconnectOnce})
	client := &http.Client{Timeout: time.Second}
	first, err := client.Get(proxy.URL() + "/v1.56/events")
	if err == nil {
		if first != nil {
			first.Body.Close()
		}
		t.Fatal("first events stream should be disconnected")
	}
	second, err := client.Get(proxy.URL() + "/v1.56/events")
	if err != nil {
		t.Fatalf("reconnect failed: %v", err)
	}
	defer second.Body.Close()
	body, _ := io.ReadAll(second.Body)
	if !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("reconnected stream body = %s", body)
	}
	if got := eventsRequests.Load(); got != 1 {
		t.Fatalf("daemon events requests = %d, want 1 (only reconnect forwarded)", got)
	}
}

func TestProxyRejectsInvalidFaultConfiguration(t *testing.T) {
	if _, err := NewProxy("http://127.0.0.1:1", []Rule{{Kind: "unknown"}}); err == nil {
		t.Fatal("unknown fault kind accepted")
	}
	if _, err := NewProxy("http://127.0.0.1:1", []Rule{{Kind: PullErrorStream, PathPrefix: "/v1.56/containers/create"}}); err == nil {
		t.Fatal("pull stream fault accepted on non-pull path")
	}
}
