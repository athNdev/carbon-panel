package httpapi

import (
	"net/http"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/audit"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/rbac"
	cloudv1connect "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1/cloudv1connect"
)

// routes registers health, readiness, metrics and all 12 Connect services.
// The interceptor chain is auth -> rbac -> audit -> handler: connect runs
// interceptors outermost-first, so auth resolves the principal before rbac
// evaluates policy, and the audit interceptor records mutating RPCs.
func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)
	s.mux.Handle("GET /metrics", s.metrics.Handler())

	chain := connect.WithInterceptors(
		AuthInterceptor(AuthOptions{Verifier: s.opts.Verifier, Store: s.opts.Store}),
		rbac.Interceptor(s.opts.Engine, rbac.Options{}),
		audit.Interceptor(s.opts.Audits, audit.Options{}),
	)

	mount := func(path string, h http.Handler) { s.mux.Handle(path, h) }
	svcs := s.opts.Services

	path, h := cloudv1connect.NewSystemServiceHandler(svcs.System, chain)
	mount(path, h)
	path, h = cloudv1connect.NewOrgServiceHandler(svcs.Org, chain)
	mount(path, h)
	path, h = cloudv1connect.NewRoleServiceHandler(svcs.Role, chain)
	mount(path, h)
	path, h = cloudv1connect.NewNodeTypeServiceHandler(svcs.NodeType, chain)
	mount(path, h)
	path, h = cloudv1connect.NewNodeServiceHandler(svcs.Node, chain)
	mount(path, h)
	path, h = cloudv1connect.NewApiKeyServiceHandler(svcs.APIKey, chain)
	mount(path, h)
	path, h = cloudv1connect.NewSessionServiceHandler(svcs.Session, chain)
	mount(path, h)
	path, h = cloudv1connect.NewAuditServiceHandler(svcs.Audit, chain)
	mount(path, h)
	path, h = cloudv1connect.NewProvisionServiceHandler(svcs.Provision, chain)
	mount(path, h)
	path, h = cloudv1connect.NewWorkloadServiceHandler(svcs.Workload, chain)
	mount(path, h)
	path, h = cloudv1connect.NewAgentServiceHandler(svcs.Agent, chain)
	mount(path, h)
	path, h = cloudv1connect.NewFileServiceHandler(svcs.File, chain)
	mount(path, h)
}
