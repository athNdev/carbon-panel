package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeHealthOK(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","version":"test","uptime":"1s"}`))
	}))
	defer ts.Close()
	if err := probeHealth(ts.URL); err != nil {
		t.Fatalf("probeHealth: %v", err)
	}
}

func TestProbeHealthCodes(t *testing.T) {
	t.Parallel()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not-ready"}`))
	}))
	defer bad.Close()
	if err := probeHealth(bad.URL); err == nil {
		t.Fatal("expected error for 503")
	}

	lying := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"broken"}`))
	}))
	defer lying.Close()
	if err := probeHealth(lying.URL); err == nil {
		t.Fatal("expected error for body without ok")
	}

	if err := probeHealth("http://127.0.0.1:1/healthz"); err == nil {
		t.Fatal("expected error for unreachable host")
	}
}

func TestHealthcheckSubcommandExitCodes(t *testing.T) {
	t.Parallel()
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","version":"v","uptime":"1s"}`))
	}))
	defer ok.Close()

	var out, errOut bytes.Buffer
	if code := run([]string{"healthcheck", ok.URL}, &out, &errOut); code != 0 {
		t.Fatalf("expected exit 0, got %d (%s)", code, errOut.String())
	}

	if code := run([]string{"healthcheck", "http://127.0.0.1:1/healthz"}, &out, &errOut); code != 1 {
		t.Fatalf("expected exit 1 for unreachable, got %d", code)
	}
}
