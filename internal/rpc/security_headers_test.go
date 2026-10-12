// Merged in the MINE-172 integration: this file existed independently on
// both lines of development, so the union of both test sets is kept.

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

// --- tests present only on the pre-merge line of development ---

func TestSecurityHeadersPresent(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	rec := httptest.NewRecorder()
	securityHeaders(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil))

	if got := rec.Result().StatusCode; got != http.StatusTeapot {
		t.Fatalf("status=%d: inner handler must still run", got)
	}
	h := rec.Result().Header
	if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options=%q", got)
	}
	if got := h.Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("X-Frame-Options=%q (docs iframe needs same-origin)", got)
	}
	if got := h.Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("Referrer-Policy=%q", got)
	}
	csp := h.Get("Content-Security-Policy")
	for _, want := range []string{
		"default-src 'self'",
		"object-src 'none'",
		"frame-ancestors 'self'",
		"connect-src 'self' ws: wss:",
		"img-src 'self' data: https:",
	} {
		if !strings.Contains(csp, want) {
			t.Fatalf("CSP %q missing %q", csp, want)
		}
	}
}

func TestSecurityHeadersOnErrorPaths(t *testing.T) {
	// Headers must be stamped before the inner handler runs, so even
	// early 4xx/5xx responses carry them.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	})
	rec := httptest.NewRecorder()
	securityHeaders(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ws", nil))
	if got := rec.Result().Header.Get("Content-Security-Policy"); got == "" {
		t.Fatal("CSP missing on error response")
	}
}
