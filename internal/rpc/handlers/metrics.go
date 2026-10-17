// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/athNdev/carbon-panel/internal/auth"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/metrics"
	"github.com/athNdev/carbon-panel/internal/rbac"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// DockerPoolStatsProvider supplies Docker client pool metrics (PERF-5).
type DockerPoolStatsProvider interface {
	Stats() docker.PoolStats
}

// HibernationStatsProvider supplies server hibernation and wake counts (SCH-6).
type HibernationStatsProvider interface {
	TotalHibernated() uint64
	TotalWoken() uint64
}

// MetricsHandler serves Prometheus OpenMetrics snapshots (MINE-144, PERF-5, SCH-6):
// GET /api/v2/metrics/host and /api/v2/metrics/servers/{id}.
type MetricsHandler struct {
	store       *storage.Store
	collector   *metrics.Collector
	authManager *auth.Manager
	enforcer    *rbac.Enforcer
	log         *logger.Logger
	pool        DockerPoolStatsProvider
	hibernation HibernationStatsProvider
}

func NewMetricsHandler(store *storage.Store, collector *metrics.Collector, authManager *auth.Manager, enforcer *rbac.Enforcer, log *logger.Logger) *MetricsHandler {
	return &MetricsHandler{store: store, collector: collector, authManager: authManager, enforcer: enforcer, log: log}
}

// SetDockerPool configures the Docker client pool stats provider.
func (h *MetricsHandler) SetDockerPool(p DockerPoolStatsProvider) {
	h.pool = p
}

// SetHibernationManager configures the server hibernation stats provider.
func (h *MetricsHandler) SetHibernationManager(m HibernationStatsProvider) {
	h.hibernation = m
}

func (h *MetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/v2/metrics/servers/") {
		h.serveServer(w, r)
		return
	}
	if r.URL.Path == "/api/v2/metrics/host" {
		h.serveHost(w, r)
		return
	}
	http.NotFound(w, r)
}

func (h *MetricsHandler) authorize(r *http.Request) bool {
	if h.authManager == nil || !h.authManager.IsAnyAuthEnabled() {
		return true
	}
	user, err := h.authManager.AuthenticateFromHeader(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		return false
	}
	if h.enforcer == nil {
		return true
	}
	allowed, err := h.enforcer.Enforce(user.Roles, rbac.ResourceServers, rbac.ActionRead, "*")
	return err == nil && allowed
}

func esc(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "\"", "\\\"")
}

func (h *MetricsHandler) snapshot() (map[string]*metrics.ServerMetrics, map[string]storage.Server) {
	all := map[string]*metrics.ServerMetrics{}
	if h.collector != nil {
		all = h.collector.GetAllMetrics()
	}
	servers := map[string]storage.Server{}
	if h.store != nil {
		if list, err := h.store.ListServers(context.Background()); err == nil {
			for _, s := range list {
				if s != nil {
					servers[s.ID] = *s
				}
			}
		}
	}
	return all, servers
}

func (h *MetricsHandler) writeExpositionWithHostStats(w http.ResponseWriter, all map[string]*metrics.ServerMetrics, servers map[string]storage.Server, only string) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	var b strings.Builder
	b.WriteString("# HELP carbon_server_cpu_percent Server CPU usage percent.\n")
	b.WriteString("# TYPE carbon_server_cpu_percent gauge\n")
	b.WriteString("# HELP carbon_server_memory_mb Server memory usage in MB.\n")
	b.WriteString("# TYPE carbon_server_memory_mb gauge\n")
	b.WriteString("# HELP carbon_server_players_online Online player count.\n")
	b.WriteString("# TYPE carbon_server_players_online gauge\n")
	b.WriteString("# HELP carbon_server_tps Server ticks per second.\n")
	b.WriteString("# TYPE carbon_server_tps gauge\n")
	for id, m := range all {
		if only != "" && id != only {
			continue
		}
		name, status := "", ""
		if s, ok := servers[id]; ok {
			name, status = s.Name, string(s.Status)
		}
		labels := fmt.Sprintf(`server="%s",name="%s",status="%s"`, esc(id), esc(name), esc(status))
		fmt.Fprintf(&b, "carbon_server_cpu_percent{%s} %f\n", labels, m.CPUPercent)
		fmt.Fprintf(&b, "carbon_server_memory_mb{%s} %f\n", labels, m.MemoryUsage)
		fmt.Fprintf(&b, "carbon_server_players_online{%s} %d\n", labels, m.PlayersOnline)
		fmt.Fprintf(&b, "carbon_server_tps{%s} %f\n", labels, m.TPS)
	}

	if only == "" {
		if h != nil && h.pool != nil {
			stats := h.pool.Stats()
			b.WriteString("# HELP carbon_docker_pool_requests_total Total GetClient requests to the Docker client pool.\n")
			b.WriteString("# TYPE carbon_docker_pool_requests_total counter\n")
			fmt.Fprintf(&b, "carbon_docker_pool_requests_total %d\n", stats.TotalRequests)

			b.WriteString("# HELP carbon_docker_pool_cache_hits_total Total cache hits in the Docker client pool.\n")
			b.WriteString("# TYPE carbon_docker_pool_cache_hits_total counter\n")
			fmt.Fprintf(&b, "carbon_docker_pool_cache_hits_total %d\n", stats.CacheHits)

			b.WriteString("# HELP carbon_docker_pool_cache_misses_total Total cache misses in the Docker client pool.\n")
			b.WriteString("# TYPE carbon_docker_pool_cache_misses_total counter\n")
			fmt.Fprintf(&b, "carbon_docker_pool_cache_misses_total %d\n", stats.CacheMisses)

			b.WriteString("# HELP carbon_docker_pool_fallbacks_total Total fallback calls to the default Docker client.\n")
			b.WriteString("# TYPE carbon_docker_pool_fallbacks_total counter\n")
			fmt.Fprintf(&b, "carbon_docker_pool_fallbacks_total %d\n", stats.Fallbacks)

			b.WriteString("# HELP carbon_docker_pool_errors_total Total errors creating clients in the Docker client pool.\n")
			b.WriteString("# TYPE carbon_docker_pool_errors_total counter\n")
			fmt.Fprintf(&b, "carbon_docker_pool_errors_total %d\n", stats.CreateErrors)
		}
		if h != nil && h.hibernation != nil {
			b.WriteString("# HELP carbon_servers_hibernated_total Total number of server hibernation events.\n")
			b.WriteString("# TYPE carbon_servers_hibernated_total counter\n")
			fmt.Fprintf(&b, "carbon_servers_hibernated_total %d\n", h.hibernation.TotalHibernated())

			b.WriteString("# HELP carbon_servers_woken_total Total number of server wake events from hibernation.\n")
			b.WriteString("# TYPE carbon_servers_woken_total counter\n")
			fmt.Fprintf(&b, "carbon_servers_woken_total %d\n", h.hibernation.TotalWoken())
		}
	}

	_, _ = w.Write([]byte(b.String()))
}

func writeExposition(w http.ResponseWriter, all map[string]*metrics.ServerMetrics, servers map[string]storage.Server, only string) {
	(*MetricsHandler)(nil).writeExpositionWithHostStats(w, all, servers, only)
}

func (h *MetricsHandler) serveHost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.authorize(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	all, servers := h.snapshot()
	h.writeExpositionWithHostStats(w, all, servers, "")
}

func (h *MetricsHandler) serveServer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.authorize(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v2/metrics/servers/")
	if id == "" {
		http.Error(w, "missing server id", http.StatusBadRequest)
		return
	}
	all, servers := h.snapshot()
	if _, ok := all[id]; !ok {
		if _, ok := servers[id]; !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
	}
	h.writeExpositionWithHostStats(w, all, servers, id)
}
