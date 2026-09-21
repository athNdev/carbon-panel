package rpc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/auth"
	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/rbac"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// testUnmappedProcedure is never registered in rbac.PublicProcedures,
// rbac.AuthenticatedOnlyProcedures, or rbac.ProcedurePermissions - it stands
// in for "a real procedure someone forgot to map."
const testUnmappedProcedure = "/carbonpanel.v1.TestService/Unmapped"

func newTestStore(t *testing.T) *storage.Store {
	t.Helper()
	// A plain ":memory:" DSN gives every pooled connection its own,
	// separate empty database, and MaxConnections:1 deadlocks
	// database/sql's pool wait once the casbin gorm-adapter (used by
	// rbac.NewEnforcer/SeedDefaultPolicies) needs a second connection while
	// the first is still checked out. A uniquely-named shared-cache DSN
	// gives every connection in the pool the same in-memory database while
	// keeping each test isolated from the others.
	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Path:           fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName),
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	}
	store, err := storage.NewSQLiteStore(cfg)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// registerEcho mounts a trivial handler for `procedure`, protected by the
// given Server's authInterceptor, using v1.GetAuthStatusRequest/Response as
// a stand-in message pair (the body content is irrelevant to every test
// here - only routing/auth behavior is under test).
func registerEcho(mux *http.ServeMux, srv *Server, procedure string) {
	opts := []connect.HandlerOption{connect.WithInterceptors(srv.authInterceptor())}
	handler := connect.NewUnaryHandler(
		procedure,
		func(_ context.Context, _ *connect.Request[v1.GetAuthStatusRequest]) (*connect.Response[v1.GetAuthStatusResponse], error) {
			return connect.NewResponse(&v1.GetAuthStatusResponse{}), nil
		},
		opts...,
	)
	mux.Handle(procedure, handler)
}

func callEcho(t *testing.T, ts *httptest.Server, procedure, bearer string) *connect.Error {
	t.Helper()
	client := connect.NewClient[v1.GetAuthStatusRequest, v1.GetAuthStatusResponse](
		http.DefaultClient,
		ts.URL+procedure,
	)
	req := connect.NewRequest(&v1.GetAuthStatusRequest{})
	if bearer != "" {
		req.Header().Set("Authorization", "Bearer "+bearer)
	}
	_, err := client.CallUnary(context.Background(), req)
	if err == nil {
		return nil
	}
	connErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected a *connect.Error, got %T: %v", err, err)
	}
	return connErr
}

// buildHarness constructs a minimal *Server (only the fields
// authInterceptor touches) plus an httptest server. When enforcer is nil,
// the caller is expected to have already decided that on purpose (to
// exercise the nil-enforcer fail-closed path).
func buildHarness(t *testing.T, anonymousAccess, noAuth bool, enforcerOverride *rbac.Enforcer, useOverride bool) (*Server, *httptest.Server) {
	t.Helper()
	store := newTestStore(t)
	log := logger.New()

	var enforcer *rbac.Enforcer
	if useOverride {
		enforcer = enforcerOverride
	} else {
		e, err := rbac.NewEnforcer(store.DB())
		if err != nil {
			t.Fatalf("failed to create enforcer: %v", err)
		}
		if err := e.SeedDefaultPolicies(anonymousAccess); err != nil {
			t.Fatalf("failed to seed default policies: %v", err)
		}
		enforcer = e
	}

	authCfg := &config.AuthConfig{
		Local:           config.LocalConfig{Enabled: !noAuth},
		OIDC:            config.OIDCConfig{Enabled: false},
		AnonymousAccess: anonymousAccess,
		// The harness deliberately exercises the "no auth provider" mode, so it
		// must opt in; without this the manager fails closed (MINE-150) and the
		// synthetic-admin assertions would (correctly) fail.
		AllowNoAuth: noAuth,
	}
	// AuthenticateFromHeader uses the *enforcer passed into NewManager only
	// for password/session bookkeeping, not for the RBAC check itself, so a
	// nil enforcer here is fine even when srv.enforcer below is also nil.
	authManager, err := auth.NewManager(store, enforcer, authCfg)
	if err != nil {
		t.Fatalf("failed to create auth manager: %v", err)
	}

	srv := &Server{
		store:       store,
		authManager: authManager,
		enforcer:    enforcer,
		log:         log,
	}

	mux := http.NewServeMux()
	registerEcho(mux, srv, "/carbonpanel.v1.AuthService/GetAuthStatus")  // public
	registerEcho(mux, srv, "/carbonpanel.v1.AuthService/GetCurrentUser") // authenticated-only
	registerEcho(mux, srv, "/carbonpanel.v1.ServerService/ListServers")  // permissioned: servers/read
	registerEcho(mux, srv, "/carbonpanel.v1.ServerService/DeleteServer") // permissioned: servers/delete
	registerEcho(mux, srv, testUnmappedProcedure)                        // in no map at all

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return srv, ts
}

// --- Bug 1 regression tests: fail-closed default for unmapped procedures ---

func TestAuthInterceptor_UnmappedProcedure_DeniedByDefault(t *testing.T) {
	_, ts := buildHarness(t, true /* anonymousAccess */, false, nil, false)

	// Anonymous access means this request authenticates fine, but the
	// procedure isn't in PublicProcedures, AuthenticatedOnlyProcedures, or
	// ProcedurePermissions. Before the fix this fell through to
	// `return next(ctx, req)` and was allowed; now it must be denied.
	connErr := callEcho(t, ts, testUnmappedProcedure, "")
	if connErr == nil {
		t.Fatal("expected unmapped procedure to be denied, got success")
	}
	if connErr.Code() != connect.CodePermissionDenied {
		t.Errorf("expected CodePermissionDenied, got %v (%v)", connErr.Code(), connErr)
	}
}

func TestAuthInterceptor_UnmappedProcedure_DeniedEvenForAdminBypass(t *testing.T) {
	// Disabling both Local and OIDC auth makes AuthenticateFromHeader
	// synthesize an all-powerful "admin" user for every request. Even that
	// caller must not reach an unmapped procedure, because the deny happens
	// before any resource/action check.
	_, ts := buildHarness(t, false, true /* noAuth */, nil, false)

	connErr := callEcho(t, ts, testUnmappedProcedure, "")
	if connErr == nil {
		t.Fatal("expected unmapped procedure to be denied even for the no-auth admin bypass user, got success")
	}
	if connErr.Code() != connect.CodePermissionDenied {
		t.Errorf("expected CodePermissionDenied, got %v (%v)", connErr.Code(), connErr)
	}
}

func TestAuthInterceptor_NilEnforcer_DeniesPermissionedProcedure(t *testing.T) {
	// A procedure that IS in ProcedurePermissions must also fail closed if
	// the RBAC enforcer failed to initialize, instead of silently skipping
	// the permission check (the pre-fix behavior).
	_, ts := buildHarness(t, false, true /* noAuth */, nil, true /* useOverride: nil enforcer */)

	connErr := callEcho(t, ts, "/carbonpanel.v1.ServerService/ListServers", "")
	if connErr == nil {
		t.Fatal("expected permissioned procedure to be denied when enforcer is nil, got success")
	}
	if connErr.Code() != connect.CodePermissionDenied {
		t.Errorf("expected CodePermissionDenied, got %v (%v)", connErr.Code(), connErr)
	}
}

// --- Regression tests: legitimate flows still work ---

func TestAuthInterceptor_PublicProcedure_AllowedWithoutAuth(t *testing.T) {
	_, ts := buildHarness(t, false, false, nil, false)

	connErr := callEcho(t, ts, "/carbonpanel.v1.AuthService/GetAuthStatus", "")
	if connErr != nil {
		t.Fatalf("expected public procedure to succeed without auth, got %v", connErr)
	}
}

func TestAuthInterceptor_AuthenticatedOnlyProcedure_AllowedForAnonymousUser(t *testing.T) {
	_, ts := buildHarness(t, true /* anonymousAccess */, false, nil, false)

	connErr := callEcho(t, ts, "/carbonpanel.v1.AuthService/GetCurrentUser", "")
	if connErr != nil {
		t.Fatalf("expected authenticated-only procedure to succeed for anonymous user, got %v", connErr)
	}
}

func TestAuthInterceptor_AuthenticatedOnlyProcedure_DeniedWithoutAuth(t *testing.T) {
	_, ts := buildHarness(t, false, false, nil, false)

	connErr := callEcho(t, ts, "/carbonpanel.v1.AuthService/GetCurrentUser", "")
	if connErr == nil {
		t.Fatal("expected authenticated-only procedure to be denied without any auth, got success")
	}
	if connErr.Code() != connect.CodeUnauthenticated {
		t.Errorf("expected CodeUnauthenticated, got %v (%v)", connErr.Code(), connErr)
	}
}

func TestAuthInterceptor_PermissionedProcedure_AllowedForRoleWithPermission(t *testing.T) {
	_, ts := buildHarness(t, true /* anonymousAccess */, false, nil, false)

	// The default "anonymous" role is seeded with servers/read - ListServers
	// requires exactly that, so this must succeed.
	connErr := callEcho(t, ts, "/carbonpanel.v1.ServerService/ListServers", "")
	if connErr != nil {
		t.Fatalf("expected read-permissioned procedure to succeed for anonymous role, got %v", connErr)
	}
}

func TestAuthInterceptor_PermissionedProcedure_DeniedForRoleWithoutPermission(t *testing.T) {
	_, ts := buildHarness(t, true /* anonymousAccess */, false, nil, false)

	// The default "anonymous" role only has servers/read, not servers/delete.
	connErr := callEcho(t, ts, "/carbonpanel.v1.ServerService/DeleteServer", "")
	if connErr == nil {
		t.Fatal("expected delete-permissioned procedure to be denied for anonymous role, got success")
	}
	if connErr.Code() != connect.CodePermissionDenied {
		t.Errorf("expected CodePermissionDenied, got %v (%v)", connErr.Code(), connErr)
	}
}

func TestAuthInterceptor_PermissionedProcedure_AllowedForAdminBypass(t *testing.T) {
	// No-auth mode (both Local and OIDC disabled) synthesizes an "admin"
	// user with wildcard permissions; a mapped, permissioned procedure must
	// still work end-to-end for it. The harness opts in via AllowNoAuth.
	_, ts := buildHarness(t, false, true /* noAuth */, nil, false)

	connErr := callEcho(t, ts, "/carbonpanel.v1.ServerService/DeleteServer", "")
	if connErr != nil {
		t.Fatalf("expected delete-permissioned procedure to succeed for admin bypass user, got %v", connErr)
	}
}

// TestAuthInterceptor_NoAuthProviderWithoutOptIn_FailsClosed covers MINE-150:
// with local auth and OIDC both disabled and no explicit opt-in, an
// unauthenticated request must be rejected instead of being granted admin.
func TestAuthInterceptor_NoAuthProviderWithoutOptIn_FailsClosed(t *testing.T) {
	store := newTestStore(t)
	log := logger.New()

	authManager, err := auth.NewManager(store, nil, &config.AuthConfig{
		Local:       config.LocalConfig{Enabled: false},
		OIDC:        config.OIDCConfig{Enabled: false},
		AllowNoAuth: false,
	})
	if err != nil {
		t.Fatalf("failed to create auth manager: %v", err)
	}

	srv := &Server{store: store, authManager: authManager, log: log}
	mux := http.NewServeMux()
	registerEcho(mux, srv, "/carbonpanel.v1.ServerService/ListServers")
	ts := httptest.NewServer(mux)
	defer ts.Close()

	connErr := callEcho(t, ts, "/carbonpanel.v1.ServerService/ListServers", "")
	if connErr == nil {
		t.Fatal("expected unauthenticated request to be rejected when no auth provider is enabled")
	}
	if connErr.Code() != connect.CodeUnauthenticated {
		t.Errorf("expected CodeUnauthenticated, got %v (%v)", connErr.Code(), connErr)
	}
}
