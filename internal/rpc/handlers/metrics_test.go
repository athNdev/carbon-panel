package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/metrics"
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

type dummyPoolStatsProvider struct {
	stats docker.PoolStats
}

func (d *dummyPoolStatsProvider) Stats() docker.PoolStats {
	return d.stats
}

type dummyHibernationStatsProvider struct {
	hibernated uint64
	woken      uint64
}

func (d *dummyHibernationStatsProvider) TotalHibernated() uint64 {
	return d.hibernated
}

func (d *dummyHibernationStatsProvider) TotalWoken() uint64 {
	return d.woken
}

func TestMetricsHostPoolAndHibernation(t *testing.T) {
	h := NewMetricsHandler(nil, nil, nil, nil, nil)
	h.SetDockerPool(&dummyPoolStatsProvider{
		stats: docker.PoolStats{
			TotalRequests: 10,
			CacheHits:     8,
			CacheMisses:   2,
			Fallbacks:     1,
			CreateErrors:  0,
		},
	})
	h.SetHibernationManager(&dummyHibernationStatsProvider{
		hibernated: 5,
		woken:      4,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v2/metrics/host", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	expectedSubstrings := []string{
		"carbon_docker_pool_requests_total 10",
		"carbon_docker_pool_cache_hits_total 8",
		"carbon_docker_pool_cache_misses_total 2",
		"carbon_docker_pool_fallbacks_total 1",
		"carbon_docker_pool_errors_total 0",
		"carbon_servers_hibernated_total 5",
		"carbon_servers_woken_total 4",
	}
	for _, substr := range expectedSubstrings {
		if !strings.Contains(body, substr) {
			t.Errorf("expected body to contain %q, but got:\n%s", substr, body)
		}
	}

	// Verify server-scoped endpoint does not include host metrics
	reqServer := httptest.NewRequest(http.MethodGet, "/api/v2/metrics/servers/s1", nil)
	recServer := httptest.NewRecorder()
	h.ServeHTTP(recServer, reqServer)
	serverBody := recServer.Body.String()
	for _, substr := range expectedSubstrings {
		if strings.Contains(serverBody, substr) {
			t.Errorf("server endpoint should not contain host stat %q", substr)
		}
	}
}
