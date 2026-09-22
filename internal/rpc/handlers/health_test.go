package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHealthHandler covers the /healthz (no check) and /readyz (check fails)
// behaviour added in MINE-151.
func TestHealthHandler(t *testing.T) {
	started := time.Now().Add(-90 * time.Second)

	t.Run("liveness always ok", func(t *testing.T) {
		h := NewHealthHandler(started, nil)
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("invalid JSON body %q: %v", rec.Body.String(), err)
		}
		if body["status"] != "ok" {
			t.Errorf("status field = %v, want ok", body["status"])
		}
		if up, _ := body["uptimeSeconds"].(float64); up < 80 {
			t.Errorf("uptimeSeconds = %v, want >= 80", body["uptimeSeconds"])
		}
	})

	t.Run("readiness degraded when check fails", func(t *testing.T) {
		h := NewHealthHandler(started, func() error { return errors.New("db down") })
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("invalid JSON body: %v", err)
		}
		if body["status"] != "degraded" {
			t.Errorf("status field = %v, want degraded", body["status"])
		}
	})

	t.Run("readiness ok when check passes", func(t *testing.T) {
		h := NewHealthHandler(started, func() error { return nil })
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("rejects non-GET", func(t *testing.T) {
		h := NewHealthHandler(started, nil)
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})
}
