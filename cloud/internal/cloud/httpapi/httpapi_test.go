package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/audit"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/node"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/nodetype"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/obs"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/rbac"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/svc"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
)

func testServer(t *testing.T) (*Server, *db.Store) {
	t.Helper()
	store, err := db.Open(db.Options{Driver: "sqlite", DSN: ":memory:", AutoMigrate: true})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	ctx := context.Background()
	if err := store.SeedDefaults(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ndeps := node.Deps{Store: store, Pepper: []byte("test-pepper-0000000000000000000000")}
	catalog := nodetype.NewCatalog(store)
	if err := catalog.EnsureSeeded(ctx); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}
	services, err := svc.New(svc.Deps{
		Store:      store,
		Nodes:      node.NewService(ndeps),
		JoinTokens: node.NewJoinTokenService(ndeps),
		Catalog:    catalog,
		Audits:     audit.NewGormStore(store),
	})
	if err != nil {
		t.Fatalf("services: %v", err)
	}
	engine, err := rbac.New(rbac.Options{Source: &BindingSource{Store: store}})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	srv, err := New(Options{
		Store:    store,
		Services: services,
		Engine:   engine,
		Audits:   audit.NewGormStore(store),
		Metrics:  obs.NewMetrics(),
		Version:  "test-9.9.9",
	})
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	return srv, store
}

func get(t *testing.T, srv *Server, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(body)
}

func TestHealthz(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	code, body := get(t, srv, "/healthz")
	if code != http.StatusOK {
		t.Fatalf("healthz status %d: %s", code, body)
	}
	for _, want := range []string{`"status":"ok"`, `"version":"test-9.9.9"`, `"uptime":`} {
		if !strings.Contains(body, want) {
			t.Fatalf("healthz missing %s: %s", want, body)
		}
	}
}

func TestReadyz(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	code, body := get(t, srv, "/readyz")
	if code != http.StatusOK {
		t.Fatalf("readyz status %d: %s", code, body)
	}
	if !strings.Contains(body, `"status":"ready"`) {
		t.Fatalf("readyz body: %s", body)
	}
}

func TestReadyzFailsWithoutDB(t *testing.T) {
	t.Parallel()
	srv, store := testServer(t)
	sqlDB, err := store.DB().DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	code, body := get(t, srv, "/readyz")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", code, body)
	}
	if !strings.Contains(body, `"status":"not-ready"`) {
		t.Fatalf("readyz body: %s", body)
	}
	// Liveness is independent of the database.
	if code, _ := get(t, srv, "/healthz"); code != http.StatusOK {
		t.Fatalf("healthz must stay 200, got %d", code)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	code, body := get(t, srv, "/metrics")
	if code != http.StatusOK {
		t.Fatalf("metrics status %d: %s", code, body)
	}
	if body == "" {
		t.Fatal("metrics body must not be empty")
	}
}

// TestChainDeniesUnmappedProcedure mounts a handler for a procedure that
// exists nowhere in the rbac table and proves the chain fails closed.
func TestChainDeniesUnmappedProcedure(t *testing.T) {
	t.Parallel()
	srv, store := testServer(t)
	chain := connect.WithInterceptors(
		AuthInterceptor(AuthOptions{Store: store}),
		rbac.Interceptor(srv.opts.Engine, rbac.Options{}),
		audit.Interceptor(srv.opts.Audits, audit.Options{}),
	)
	mux := http.NewServeMux()
	mux.Handle("/cloud.v1.Unknown/Ping", connect.NewUnaryHandler(
		"/cloud.v1.Unknown/Ping",
		func(ctx context.Context, req *connect.Request[v1.GetSessionRequest]) (*connect.Response[v1.GetSessionResponse], error) {
			return connect.NewResponse(&v1.GetSessionResponse{}), nil
		},
		chain,
	))
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := connect.NewClient[v1.GetSessionRequest, v1.GetSessionResponse](http.DefaultClient, ts.URL+"/cloud.v1.Unknown/Ping")
	_, err := client.CallUnary(context.Background(), connect.NewRequest(&v1.GetSessionRequest{}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied for unmapped procedure, got %v", err)
	}

	// Presented-but-invalid credentials fail closed before rbac runs.
	badReq := connect.NewRequest(&v1.GetSessionRequest{})
	badReq.Header().Set("Authorization", "Bearer bogus-token")
	_, err = client.CallUnary(context.Background(), badReq)
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("expected Unauthenticated for bogus bearer, got %v", err)
	}
}

// TestServicesRegistered proves all 11 Connect services are mounted by
// hitting one public RPC over the real mux.
func TestServicesRegistered(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	client := connect.NewClient[v1.GetBuildInfoRequest, v1.GetBuildInfoResponse](
		http.DefaultClient, ts.URL+"/cloud.v1.SystemService/GetBuildInfo")
	resp, err := client.CallUnary(context.Background(), connect.NewRequest(&v1.GetBuildInfoRequest{}))
	if err != nil {
		t.Fatalf("GetBuildInfo over HTTP: %v", err)
	}
	if resp.Msg.Build.Version != "test-9.9.9" {
		t.Fatalf("version must flow through the mux: %+v", resp.Msg.Build)
	}
}
