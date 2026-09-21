// Package httpapi is the cloudcontrold HTTP server: health/readiness,
// metrics, the Connect-RPC handler set, and the interceptor chain.
//
// Chain order (outermost first): auth (bearer -> principal) -> rbac
// (fail-closed) -> audit (mutating RPCs only) -> handler.
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/audit"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/auth"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/obs"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/rbac"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/svc"
)

// RateLimit, when non-nil, wraps the served mux. It is the seam for the
// rate-limit lane (owner of cloud/internal/cloud/auth/ratelimit.go): that
// lane assigns this variable at startup to enforce token-bucket limits on
// the auth endpoints. Nil means no rate limiting.
var RateLimit func(http.Handler) http.Handler

// Options wires the server. Store, Services, Engine and Audits are
// required; New fails otherwise. A nil Verifier disables Clerk session
// JWTs (API keys still work when Store is set).
type Options struct {
	Store    *db.Store
	Services *svc.Services
	Verifier auth.Verifier
	Engine   *rbac.Engine
	Audits   audit.Store
	Metrics  *obs.Metrics
	Logger   *slog.Logger
	Version  string
}

// Server serves health, readiness, metrics and all Connect-RPC services.
type Server struct {
	opts    Options
	mux     *http.ServeMux
	started time.Time
	metrics *obs.Metrics
}

// New builds the server and registers every route.
func New(o Options) (*Server, error) {
	if o.Store == nil {
		return nil, errors.New("httpapi: db store is required")
	}
	if o.Services == nil {
		return nil, errors.New("httpapi: services are required")
	}
	if o.Engine == nil {
		return nil, errors.New("httpapi: rbac engine is required")
	}
	if o.Audits == nil {
		return nil, errors.New("httpapi: audit store is required")
	}
	if o.Metrics == nil {
		o.Metrics = obs.NewMetrics()
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	s := &Server{opts: o, mux: http.NewServeMux(), started: time.Now().UTC(), metrics: o.Metrics}
	s.routes()
	return s, nil
}

// Handler returns the fully-wrapped mux (rate limiter outermost when set).
func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	if RateLimit != nil {
		h = RateLimit(h)
	}
	return h
}

func writeJSON(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body))
}

// handleHealthz reports liveness: always 200 when the process runs.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(s.started).Truncate(time.Second).String()
	writeJSON(w, http.StatusOK, `{"status":"ok","version":`+quote(s.opts.Version)+`,"uptime":`+quote(uptime)+`}`)
}

// handleReadyz reports readiness: 200 only when the database pings.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(s.started).Truncate(time.Second).String()
	if err := pingDB(r.Context(), s.opts.Store); err != nil {
		s.opts.Logger.Warn("readyz: db ping failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, `{"status":"not-ready","version":`+quote(s.opts.Version)+`,"uptime":`+quote(uptime)+`}`)
		return
	}
	writeJSON(w, http.StatusOK, `{"status":"ready","version":`+quote(s.opts.Version)+`,"uptime":`+quote(uptime)+`}`)
}

func pingDB(ctx context.Context, store *db.Store) error {
	sqlDB, err := store.DB().DB()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return sqlDB.PingContext(ctx)
}

func quote(s string) string {
	if s == "" {
		return `"dev"`
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
