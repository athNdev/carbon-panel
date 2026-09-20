package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/metrics"
	storage "github.com/athNdev/carbon-panel/internal/db"
)

func TestMetricsHostEmpty(t *testing.T) {
	h := NewMetricsHandler(nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/metrics/host", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("expected OpenMetrics content type, got %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "carbon_server_cpu_percent") {
		t.Fatalf("missing HELP lines in:\n%s", rec.Body.String())
	}
}

func TestMetricsServerNotFound(t *testing.T) {
	h := NewMetricsHandler(nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/metrics/servers/nope", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestWriteExpositionLabels(t *testing.T) {
	rec := httptest.NewRecorder()
	all := map[string]*metrics.ServerMetrics{
		"s1": {ServerID: "s1", CPUPercent: 12.5, MemoryUsage: 1024, PlayersOnline: 3, TPS: 20},
	}
	writeExposition(rec, all, map[string]storage.Server{}, "s1")
	body := rec.Body.String()
	for _, want := range []string{`server="s1"`, "12.5", "1024", " 3\n", "carbon_server_tps"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
}
