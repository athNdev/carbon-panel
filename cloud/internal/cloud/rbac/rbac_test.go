package rbac

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	cloudv1connect "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1/cloudv1connect"
)

func testEngine(t *testing.T, src BindingSource) *Engine {
	t.Helper()
	e, err := New(Options{Source: src})
	require.NoError(t, err)
	require.NotNil(t, e)
	return e
}

func orgPrincipal(role string) principal.Principal {
	return principal.Principal{
		Kind:   principal.KindSession,
		UserID: "user-1",
		OrgID:  "org-1",
		Role:   role,
		Email:  "op@example.com",
	}
}

func ctxWithPrincipal(role string) context.Context {
	return principal.WithPrincipal(context.Background(), orgPrincipal(role))
}

// TestEveryProcedureIsMapped fails if any descriptor procedure has no
// permission: adding an RPC without a mapping entry must break the build.
func TestEveryProcedureIsMapped(t *testing.T) {
	t.Parallel()
	procs := AllProcedures()
	require.NotEmpty(t, procs)
	seen := map[string]struct{}{}
	for _, proc := range procs {
		require.NotEmpty(t, proc)
		_, dup := seen[proc]
		require.False(t, dup, "duplicate procedure %q", proc)
		seen[proc] = struct{}{}
		perm, ok := PermissionForProcedure(proc)
		require.True(t, ok, "procedure %q has no permission mapping", proc)
		require.NotEmpty(t, perm, "procedure %q maps to empty permission", proc)
	}
	// Mapping table and enumeration must agree exactly.
	require.Len(t, procedurePermissions, len(procs),
		"mapping table drifted from AllProcedures")
}

// TestUnknownProcedureDenied covers unknown, empty and mistyped procedures at
// both the lookup and the interceptor decision layer.
func TestUnknownProcedureDenied(t *testing.T) {
	t.Parallel()
	for _, proc := range []string{"", "/cloud.v1.NodeService/Nope", "ListNodes", "/other.Service/Method"} {
		_, ok := PermissionForProcedure(proc)
		require.False(t, ok, "procedure %q must not map", proc)
	}
	e := testEngine(t, nil)
	in := Interceptor(e, Options{})
	for _, proc := range []string{"", "/cloud.v1.NodeService/Nope"} {
		_, err := in.(*failClosedInterceptor).authorize(ctxWithPrincipal(RoleAdmin), proc)
		require.Error(t, err)
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		if proc != "" {
			require.NotContains(t, err.Error(), proc, "denial must not leak the procedure")
		}
	}
}

// TestPublicAllowlistIsExhaustive requires every allowlist entry to be a real
// descriptor procedure with a mapping; nothing public may be unmapped.
func TestPublicAllowlistIsExhaustive(t *testing.T) {
	t.Parallel()
	public := PublicProcedures()
	require.NotEmpty(t, public)
	known := map[string]struct{}{}
	for _, proc := range AllProcedures() {
		known[proc] = struct{}{}
	}
	for _, proc := range public {
		_, ok := known[proc]
		require.True(t, ok, "public %q is not a descriptor procedure", proc)
		_, mapped := PermissionForProcedure(proc)
		require.True(t, mapped, "public %q has no permission mapping", proc)
		require.True(t, IsPublic(proc))
	}
	require.False(t, IsPublic(cloudv1connect.NodeServiceDeleteNodeProcedure))
}

// TestRoleMatrix asserts owner ⊇ admin ⊇ operator over the catalogue and that
// viewer (and billing) hold no mutating permission.
func TestRoleMatrix(t *testing.T) {
	t.Parallel()
	set := func(role string) map[Permission]struct{} {
		out := map[Permission]struct{}{}
		for _, p := range PermissionsForRole(role) {
			out[p] = struct{}{}
		}
		return out
	}
	owner, admin, operator := set(RoleOwner), set(RoleAdmin), set(RoleOperator)
	for p := range admin {
		_, ok := owner[p]
		require.True(t, ok, "owner must hold admin permission %q", p)
	}
	for p := range operator {
		_, ok := admin[p]
		require.True(t, ok, "admin must hold operator permission %q", p)
	}
	require.Greater(t, len(owner), len(admin), "owner must strictly exceed admin")
	for _, role := range []string{RoleViewer, RoleBilling} {
		for _, p := range PermissionsForRole(role) {
			require.False(t, IsMutating(p), "%s must not hold mutating %q", role, p)
		}
	}
	require.NotEmpty(t, PermissionsForRole(RoleViewer), "viewer must still read")
	// Boundary: unknown and oddly-cased roles hold nothing.
	require.Empty(t, PermissionsForRole("superadmin"))
	require.Empty(t, PermissionsForRole(""))
	require.Equal(t, PermissionsForRole(RoleAdmin), PermissionsForRole("  ADMIN "))

	e := testEngine(t, nil)
	ctx := context.Background()
	for _, tc := range []struct {
		role string
		perm Permission
		want bool
	}{
		{RoleOwner, PermOrgDelete, true},
		{RoleAdmin, PermOrgDelete, false},
		{RoleAdmin, PermNodesWrite, true},
		{RoleOperator, PermProvisionApply, false},
		{RoleOperator, PermProvisionWrite, true},
		{RoleViewer, PermNodesRead, true},
		{RoleViewer, PermNodesWrite, false},
		{RoleViewer, PermWorkloadsExec, false},
		{RoleBilling, PermNodesRead, false},
		{"superadmin", PermNodesRead, false},
	} {
		got, err := e.Allowed(ctx, orgPrincipal(tc.role), tc.perm)
		require.NoError(t, err)
		require.Equal(t, tc.want, got, "role %q perm %q", tc.role, tc.perm)
	}
}

// stubSource is a fake BindingSource.
type stubSource struct {
	bindings []Binding
	err      error
}

func (s stubSource) Bindings(context.Context, string) ([]Binding, error) {
	return s.bindings, s.err
}

// TestBindingScoping proves a binding on node A never authorizes node B.
func TestBindingScoping(t *testing.T) {
	t.Parallel()
	src := stubSource{bindings: []Binding{{
		SubjectType:  "user",
		SubjectID:    "user-1",
		ResourceType: "node",
		ResourceID:   "node-A",
		Permissions:  []Permission{PermNodesWrite},
	}}}
	e := testEngine(t, src)
	ctx := context.Background()
	p := orgPrincipal(RoleViewer)

	ok, err := e.AllowedForResource(ctx, p, PermNodesWrite, "node", "node-A")
	require.NoError(t, err)
	require.True(t, ok, "scoped binding must authorize its own resource")

	for _, tc := range []struct {
		name, rtype, rid string
	}{
		{"other node", "node", "node-B"},
		{"other type", "workload", "node-A"},
		{"empty type", "", "node-A"},
	} {
		ok, err := e.AllowedForResource(ctx, p, PermNodesWrite, tc.rtype, tc.rid)
		require.NoError(t, err)
		require.False(t, ok, tc.name)
	}
	// Scoped bindings never leak into the org-wide check.
	ok, err = e.Allowed(ctx, p, PermNodesWrite)
	require.NoError(t, err)
	require.False(t, ok)

	// Unscoped binding grants org-wide and for any resource.
	e2 := testEngine(t, stubSource{bindings: []Binding{{
		SubjectType: "role", SubjectID: RoleViewer,
		Permissions: []Permission{PermNodesRead},
	}}})
	ok, err = e2.Allowed(ctx, p, PermNodesRead)
	require.NoError(t, err)
	require.True(t, ok)
}

// TestEngineErrorDenies proves storage failures deny instead of opening.
func TestEngineErrorDenies(t *testing.T) {
	t.Parallel()
	e := testEngine(t, stubSource{err: errors.New("db down")})
	// Viewer lacks nodes.write by role, so the check reaches the failing
	// source and must surface the error instead of opening or masking it.
	_, err := e.Allowed(context.Background(), orgPrincipal(RoleViewer), PermNodesWrite)
	require.Error(t, err, "binding-source failure must surface an error")

	in := Interceptor(e, Options{})
	// Viewer lacks nodes.write by role, so the check reaches the failing
	// source and the interceptor must deny fail-closed.
	_, aerr := in.(*failClosedInterceptor).authorize(
		ctxWithPrincipal(RoleViewer),
		cloudv1connect.NodeServiceDeleteNodeProcedure,
	)
	require.Error(t, aerr)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(aerr))
}

// TestInterceptorHappyAndDenialPaths covers the unary decision chain:
// public bypass, role allow, role deny, missing principal, missing org.
func TestInterceptorHappyAndDenialPaths(t *testing.T) {
	t.Parallel()
	e := testEngine(t, nil)
	in := Interceptor(e, Options{}).(*failClosedInterceptor)

	// Public procedure without any principal.
	ctx, err := in.authorize(context.Background(), cloudv1connect.SystemServiceGetBuildInfoProcedure)
	require.NoError(t, err)
	info, ok := AuditInfoFrom(ctx)
	require.True(t, ok)
	require.Equal(t, PermSystemRead, info.Permission)
	require.False(t, info.Authenticated)

	// Role allow attaches audit metadata.
	ctx, err = in.authorize(ctxWithPrincipal(RoleOperator), cloudv1connect.NodeServiceListNodesProcedure)
	require.NoError(t, err)
	info, ok = AuditInfoFrom(ctx)
	require.True(t, ok)
	require.True(t, info.Authenticated)
	require.Equal(t, PermNodesRead, info.Permission)
	require.Equal(t, "org-1", info.Actor.OrgID)

	// Role deny.
	_, err = in.authorize(ctxWithPrincipal(RoleViewer), cloudv1connect.NodeServiceDeleteNodeProcedure)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	// Missing principal denies on protected procedures.
	_, err = in.authorize(context.Background(), cloudv1connect.NodeServiceListNodesProcedure)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	// Missing org denies tenant-scoped RPCs but not bootstrap ones.
	noOrg := principal.WithPrincipal(context.Background(), principal.Principal{
		Kind: principal.KindSession, UserID: "u-1", Role: RoleAdmin,
	})
	_, err = in.authorize(noOrg, cloudv1connect.NodeServiceListNodesProcedure)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	_, err = in.authorize(noOrg, cloudv1connect.OrgServiceCreateOrgProcedure)
	require.NoError(t, err, "org bootstrap must work before tenant selection")
	_, err = in.authorize(noOrg, cloudv1connect.SessionServiceListMyOrgsProcedure)
	require.NoError(t, err, "session bootstrap must work before tenant selection")

	// Node identity and system principals.
	nodeCtx := principal.WithPrincipal(context.Background(), principal.Principal{
		Kind: principal.KindNode, NodeID: "node-1",
	})
	_, err = in.authorize(nodeCtx, cloudv1connect.AgentServiceConnectProcedure)
	require.NoError(t, err)
	_, err = in.authorize(nodeCtx, cloudv1connect.NodeServiceListNodesProcedure)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	sysCtx := principal.WithPrincipal(context.Background(), principal.Principal{Kind: principal.KindSystem})
	_, err = in.authorize(sysCtx, cloudv1connect.NodeServiceDeleteNodeProcedure)
	require.NoError(t, err)

	// Nil engine denies everything protected.
	nilIn := Interceptor(nil, Options{}).(*failClosedInterceptor)
	_, err = nilIn.authorize(ctxWithPrincipal(RoleOwner), cloudv1connect.NodeServiceListNodesProcedure)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}

// TestUnaryInterceptorDeniesThroughPublicSurface exercises WrapUnary without
// hand-rolled request fakes: a client-built request carries no procedure, so
// it must deny fail-closed and never reach the handler.
func TestUnaryInterceptorDeniesThroughPublicSurface(t *testing.T) {
	t.Parallel()
	e := testEngine(t, nil)
	called := false
	next := func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		called = true
		return nil, nil
	}
	_, err := Interceptor(e, Options{}).WrapUnary(next)(
		ctxWithPrincipal(RoleOwner), connect.NewRequest(&emptypb.Empty{}),
	)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	require.False(t, called)
}

// fakeStreamConn is a minimal server-side streaming connection.
type fakeStreamConn struct {
	procedure string
}

func (f *fakeStreamConn) Spec() connect.Spec { return connect.Spec{Procedure: f.procedure} }
func (f *fakeStreamConn) Peer() connect.Peer { return connect.Peer{} }
func (f *fakeStreamConn) Receive(any) error  { return nil }
func (f *fakeStreamConn) RequestHeader() http.Header {
	return http.Header{}
}
func (f *fakeStreamConn) Send(any) error              { return nil }
func (f *fakeStreamConn) ResponseHeader() http.Header { return http.Header{} }
func (f *fakeStreamConn) ResponseTrailer() http.Header {
	return http.Header{}
}

// TestStreamingInterceptorDenies exercises the streaming handler chain:
// denials for unknown and unauthorized procedures, pass-through for public
// and authorized ones.
func TestStreamingInterceptorDenies(t *testing.T) {
	t.Parallel()
	e := testEngine(t, nil)
	in := Interceptor(e, Options{})

	denied := in.WrapStreamingHandler(func(context.Context, connect.StreamingHandlerConn) error {
		return nil
	})
	err := denied(ctxWithPrincipal(RoleViewer), &fakeStreamConn{
		procedure: cloudv1connect.WorkloadServiceStreamWorkloadLogsProcedure,
	})
	require.NoError(t, err, "viewer holds workloads.read")

	err = denied(ctxWithPrincipal(RoleViewer), &fakeStreamConn{
		procedure: cloudv1connect.ProvisionServiceStreamProvisionLogsProcedure,
	})
	require.NoError(t, err, "viewer holds provision.read")

	err = denied(context.Background(), &fakeStreamConn{procedure: "/cloud.v1.X/Nope"})
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	err = denied(ctxWithPrincipal(RoleOperator), &fakeStreamConn{
		procedure: cloudv1connect.AgentServiceConnectProcedure,
	})
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err),
		"org roles must not ride the node control stream")

	// Public streaming-adjacent procedure passes without a principal and
	// reaches the handler with audit metadata attached.
	passed := false
	allow := in.WrapStreamingHandler(func(ctx context.Context, _ connect.StreamingHandlerConn) error {
		_, ok := AuditInfoFrom(ctx)
		passed = ok
		return nil
	})
	err = allow(context.Background(), &fakeStreamConn{
		procedure: cloudv1connect.AgentServiceJoinNodeProcedure,
	})
	require.NoError(t, err)
	require.True(t, passed)
}

// TestEngineBoundaryCases covers empty permission, anonymous principal,
// unknown role and empty-permission principals.
func TestEngineBoundaryCases(t *testing.T) {
	t.Parallel()
	e := testEngine(t, nil)
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		p    principal.Principal
		perm Permission
	}{
		{"empty permission", orgPrincipal(RoleOwner), ""},
		{"unknown permission", orgPrincipal(RoleOwner), "nodes.destroy"},
		{"anonymous", principal.Principal{}, PermNodesRead},
		{"missing org", principal.Principal{Kind: principal.KindSession, UserID: "u", Role: RoleOwner}, PermNodesRead},
		{"unknown role", orgPrincipal("root"), PermNodesRead},
	} {
		ok, err := e.Allowed(ctx, tc.p, tc.perm)
		require.NoError(t, err, tc.name)
		require.False(t, ok, tc.name)
	}
	// Explicit principal.Permissions grant (API-key style) without role help.
	keyed := orgPrincipal("unknown-role")
	keyed.APIKeyID = "key-1"
	keyed.Permissions = []string{string(PermAuditRead)}
	ok, err := e.Allowed(ctx, keyed, PermAuditRead)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = e.Allowed(ctx, keyed, PermNodesWrite)
	require.NoError(t, err)
	require.False(t, ok)
}
