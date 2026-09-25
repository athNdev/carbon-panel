// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package rpc

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders_DefaultSet(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	securityHeaders(next).ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if got := res.Header.Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options = %q, want SAMEORIGIN", got)
	}
	if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := res.Header.Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Errorf("Referrer-Policy = %q, want strict-origin-when-cross-origin", got)
	}
	csp := res.Header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "frame-ancestors 'self'", "object-src 'none'", "connect-src", "ws:", "wss:"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q (got %q)", want, csp)
		}
	}
	if got := res.Header.Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS must not be set on plaintext requests, got %q", got)
	}
}

func TestSecurityHeaders_HSTSOnTLS(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	newTLSRequest := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "https://panel.local/healthz", nil)
		req.TLS = &tls.ConnectionState{}
		return req
	}

	// Direct TLS.
	rec := httptest.NewRecorder()
	securityHeaders(next).ServeHTTP(rec, newTLSRequest())
	if got := rec.Result().Header.Get("Strict-Transport-Security"); !strings.HasPrefix(got, "max-age=") {
		t.Errorf("HSTS missing on TLS request, got %q", got)
	}

	// Behind a TLS-terminating proxy.
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec = httptest.NewRecorder()
	securityHeaders(next).ServeHTTP(rec, req)
	if got := rec.Result().Header.Get("Strict-Transport-Security"); !strings.HasPrefix(got, "max-age=") {
		t.Errorf("HSTS missing behind TLS-terminating proxy, got %q", got)
	}
}

func TestSecurityHeaders_NoDuplicatesWhenInnerSets(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	securityHeaders(next).ServeHTTP(rec, req)

	if values := rec.Result().Header.Values("X-Frame-Options"); len(values) != 1 {
		t.Errorf("expected exactly one X-Frame-Options value, got %q", values)
	}
}
