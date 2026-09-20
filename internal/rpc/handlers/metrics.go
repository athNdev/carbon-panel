package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/athNdev/carbon-panel/internal/auth"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/metrics"
	"github.com/athNdev/carbon-panel/internal/rbac"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// MetricsHandler serves Prometheus OpenMetrics snapshots (MINE-144):
// GET /api/v2/metrics/host and /api/v2/metrics/servers/{id}.
type MetricsHandler struct {
	store       *storage.Store
	collector   *metrics.Collector
	authManager *auth.Manager
	enforcer    *rbac.Enforcer
	log         *logger.Logger
}

func NewMetricsHandler(store *storage.Store, collector *metrics.Collector, authManager *auth.Manager, enforcer *rbac.Enforcer, log *logger.Logger) http.Handler {
	h := &MetricsHandler{store: store, collector: collector, authManager: authManager, enforcer: enforcer, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/metrics/host", h.serveHost)
	mux.HandleFunc("/api/v2/metrics/servers/", h.serveServer)
	return mux
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

func writeExposition(w http.ResponseWriter, all map[string]*metrics.ServerMetrics, servers map[string]storage.Server, only string) {
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
	_, _ = w.Write([]byte(b.String()))
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
	writeExposition(w, all, servers, "")
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
	writeExposition(w, all, servers, id)
}
