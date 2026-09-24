package svc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/audit"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/billing"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/node"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/nodetype"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/notify"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
)

const testOrg = "org-test-1"

// testBundleCustom opens an in-memory store and builds services with custom deps.
func testBundleCustom(t *testing.T, mut func(deps *Deps)) (*Services, *db.Store) {
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
	deps := Deps{
		Store:      store,
		Nodes:      node.NewService(ndeps),
		JoinTokens: node.NewJoinTokenService(ndeps),
		Catalog:    catalog,
		Audits:     audit.NewGormStore(store),
	}
	if mut != nil {
		mut(&deps)
	}
	svcs, err := New(deps)
	if err != nil {
		t.Fatalf("new services: %v", err)
	}
	return svcs, store
}

// testBundle opens an in-memory store and builds every service. No Docker,
// no Postgres, no network.
func testBundle(t *testing.T) *Services {
	svcs, _ := testBundleCustom(t, nil)
	return svcs
}

func sysCtx() context.Context {
	return principal.WithPrincipal(context.Background(), principal.Principal{Kind: principal.KindSystem})
}

func orgCtx() context.Context {
	return principal.WithPrincipal(context.Background(), principal.Principal{
		Kind: principal.KindAPIKey, UserID: "user-1", OrgID: testOrg, Role: "admin",
	})
}

func TestSystemBuildInfo(t *testing.T) {
	t.Parallel()
	svcs, _ := testBundleCustom(t, func(d *Deps) {
		d.Version = "test-1.2.3"
		d.Commit = "abc123"
	})
	resp, err := svcs.System.GetBuildInfo(sysCtx(), connect.NewRequest(&v1.GetBuildInfoRequest{}))
	if err != nil {
		t.Fatalf("GetBuildInfo: %v", err)
	}
	if resp.Msg.Build.Version != "test-1.2.3" || resp.Msg.Build.Commit != "abc123" {
		t.Fatalf("unexpected build info: %+v", resp.Msg.Build)
	}
	if resp.Msg.Build.GoVersion == "" {
		t.Fatal("go version must be set")
	}
}

func TestSystemCapabilities(t *testing.T) {
	t.Parallel()
	svcs := testBundle(t)
	resp, err := svcs.System.GetCapabilities(sysCtx(), connect.NewRequest(&v1.GetCapabilitiesRequest{}))
	if err != nil {
		t.Fatalf("GetCapabilities: %v", err)
	}
	if len(resp.Msg.Capabilities) == 0 {
		t.Fatal("expected at least one capability")
	}
	for _, c := range resp.Msg.Capabilities {
		if c.Id == "" {
			t.Fatal("capability id must be set")
		}
	}
}

func TestOrgLifecycle(t *testing.T) {
	t.Parallel()
	svcs := testBundle(t)
	ctx := principal.WithPrincipal(context.Background(), principal.Principal{
		Kind: principal.KindSession, UserID: "user-creator", Email: "creator@example.com",
	})
	created, err := svcs.Org.CreateOrg(ctx, connect.NewRequest(&v1.CreateOrgRequest{Name: "Acme Inc"}))
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	if created.Msg.Org.Slug != "acme-inc" {
		t.Fatalf("slug derived incorrectly: %q", created.Msg.Org.Slug)
	}
	orgID := created.Msg.Org.Id

	// Empty name is rejected.
	if _, err := svcs.Org.CreateOrg(ctx, connect.NewRequest(&v1.CreateOrgRequest{})); err == nil {
		t.Fatal("expected error for empty org name")
	}

	mctx := principal.WithPrincipal(context.Background(), principal.Principal{
		Kind: principal.KindSession, UserID: "user-creator", OrgID: orgID, Role: "owner",
	})
	got, err := svcs.Org.GetOrg(mctx, connect.NewRequest(&v1.GetOrgRequest{}))
	if err != nil {
		t.Fatalf("GetOrg: %v", err)
	}
	if got.Msg.Org.Id != orgID {
		t.Fatalf("wrong org: %q", got.Msg.Org.Id)
	}

	members, err := svcs.Org.ListMembers(mctx, connect.NewRequest(&v1.ListMembersRequest{}))
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members.Msg.Members) != 1 || members.Msg.Members[0].Role != v1.Role_ROLE_OWNER {
		t.Fatalf("creator must be sole owner member: %+v", members.Msg.Members)
	}

	// Wrong confirmation refuses deletion.
	if _, err := svcs.Org.DeleteOrg(mctx, connect.NewRequest(&v1.DeleteOrgRequest{ConfirmOrgId: "nope"})); err == nil {
		t.Fatal("expected confirm mismatch error")
	}
	if _, err := svcs.Org.DeleteOrg(mctx, connect.NewRequest(&v1.DeleteOrgRequest{ConfirmOrgId: orgID})); err != nil {
		t.Fatalf("DeleteOrg: %v", err)
	}
	if _, err := svcs.Org.GetOrg(mctx, connect.NewRequest(&v1.GetOrgRequest{})); err == nil {
		t.Fatal("expected not-found after delete")
	}
}

func TestAPIKeySecretOnce(t *testing.T) {
	t.Parallel()
	svcs := testBundle(t)
	ctx := orgCtx()

	created, err := svcs.APIKey.CreateApiKey(ctx, connect.NewRequest(&v1.CreateApiKeyRequest{Name: "ci"}))
	if err != nil {
		t.Fatalf("CreateApiKey: %v", err)
	}
	if created.Msg.Secret == "" || !strings.HasPrefix(created.Msg.Secret, "cc_") {
		t.Fatalf("secret must be returned once: %q", created.Msg.Secret)
	}
	secret := created.Msg.Secret

	listed, err := svcs.APIKey.ListApiKeys(ctx, connect.NewRequest(&v1.ListApiKeysRequest{}))
	if err != nil {
		t.Fatalf("ListApiKeys: %v", err)
	}
	if len(listed.Msg.ApiKeys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(listed.Msg.ApiKeys))
	}
	if listed.Msg.ApiKeys[0].Prefix == "" || listed.Msg.ApiKeys[0].Prefix == secret {
		t.Fatal("list must expose the prefix, never the secret")
	}

	// Empty name is rejected.
	if _, err := svcs.APIKey.CreateApiKey(ctx, connect.NewRequest(&v1.CreateApiKeyRequest{})); err == nil {
		t.Fatal("expected error for empty key name")
	}

	rotated, err := svcs.APIKey.RotateApiKey(ctx, connect.NewRequest(&v1.RotateApiKeyRequest{Id: created.Msg.ApiKey.Id}))
	if err != nil {
		t.Fatalf("RotateApiKey: %v", err)
	}
	if rotated.Msg.Secret == "" || rotated.Msg.Secret == secret {
		t.Fatal("rotation must mint a fresh secret")
	}

	if _, err := svcs.APIKey.RevokeApiKey(ctx, connect.NewRequest(&v1.RevokeApiKeyRequest{Id: rotated.Msg.ApiKey.Id})); err != nil {
		t.Fatalf("RevokeApiKey: %v", err)
	}
}

func TestNodeJoinAndRegistry(t *testing.T) {
	t.Parallel()
	svcs := testBundle(t)
	ctx := orgCtx()

	issued, err := svcs.Node.CreateJoinToken(ctx, connect.NewRequest(&v1.CreateJoinTokenRequest{Name: "byo-1", TtlSeconds: 3600}))
	if err != nil {
		t.Fatalf("CreateJoinToken: %v", err)
	}
	if issued.Msg.Secret == "" || issued.Msg.JoinCommand == "" {
		t.Fatal("join token must carry a one-time secret and command")
	}
	if strings.Contains(issued.Msg.JoinCommand, issued.Msg.Secret) == false {
		t.Fatal("join command must embed the secret")
	}

	// Tokens list exposes metadata, never the secret.
	tokens, err := svcs.Node.ListJoinTokens(ctx, connect.NewRequest(&v1.ListJoinTokensRequest{}))
	if err != nil {
		t.Fatalf("ListJoinTokens: %v", err)
	}
	if len(tokens.Msg.Tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens.Msg.Tokens))
	}

	joined, err := svcs.Agent.JoinNode(context.Background(), connect.NewRequest(&v1.JoinNodeRequest{
		Token: issued.Msg.Secret, Hostname: "byo-host-1", AgentVersion: "0.0-test",
		Capacity: &v1.NodeCapacity{Vcpu: 4, RamMb: 8192, DiskGb: 100},
	}))
	if err != nil {
		t.Fatalf("JoinNode: %v", err)
	}
	if joined.Msg.Identity.NodeId == "" || joined.Msg.Identity.OrgId != testOrg {
		t.Fatalf("bad identity: %+v", joined.Msg.Identity)
	}

	// Single-use: redeeming again fails.
	if _, err := svcs.Agent.JoinNode(context.Background(), connect.NewRequest(&v1.JoinNodeRequest{Token: issued.Msg.Secret})); err == nil {
		t.Fatal("expected single-use token to be consumed")
	}

	nodes, err := svcs.Node.ListNodes(ctx, connect.NewRequest(&v1.ListNodesRequest{}))
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes.Msg.Nodes) != 1 || nodes.Msg.Nodes[0].Name != "byo-host-1" {
		t.Fatalf("expected the joined node: %+v", nodes.Msg.Nodes)
	}
	if nodes.Msg.Nodes[0].Capacity.Vcpu != 4 {
		t.Fatalf("capacity must round-trip: %+v", nodes.Msg.Nodes[0].Capacity)
	}

	nodeID := nodes.Msg.Nodes[0].Id
	drained, err := svcs.Node.DrainNode(ctx, connect.NewRequest(&v1.DrainNodeRequest{Id: nodeID}))
	if err != nil {
		t.Fatalf("DrainNode: %v", err)
	}
	if drained.Msg.Node.Status != v1.NodeStatus_NODE_STATUS_DRAINING {
		t.Fatalf("expected draining, got %v", drained.Msg.Node.Status)
	}
	if _, err := svcs.Node.ResumeNode(ctx, connect.NewRequest(&v1.ResumeNodeRequest{Id: nodeID})); err != nil {
		t.Fatalf("ResumeNode: %v", err)
	}
	if _, err := svcs.Node.DeleteNode(ctx, connect.NewRequest(&v1.DeleteNodeRequest{Id: nodeID})); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}
	if _, err := svcs.Node.GetNode(ctx, connect.NewRequest(&v1.GetNodeRequest{Id: nodeID})); err == nil {
		t.Fatal("expected not-found after delete")
	}
}

func TestWorkloadIntents(t *testing.T) {
	t.Parallel()
	svcs := testBundle(t)
	ctx := orgCtx()

	created, err := svcs.Workload.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
		Name: "survival", NodeId: "node-1",
		Spec: &v1.WorkloadSpec{Loader: "paper", MinecraftVersion: "1.21", MemoryMb: 2048},
	}))
	if err != nil {
		t.Fatalf("CreateWorkload: %v", err)
	}
	if created.Msg.Workload.Status != v1.WorkloadStatus_WORKLOAD_STATUS_PENDING {
		t.Fatalf("new workloads are pending: %v", created.Msg.Workload.Status)
	}
	wid := created.Msg.Workload.Id

	started, err := svcs.Workload.StartWorkload(ctx, connect.NewRequest(&v1.StartWorkloadRequest{Id: wid}))
	if err != nil {
		t.Fatalf("StartWorkload: %v", err)
	}
	if started.Msg.Workload.Status != v1.WorkloadStatus_WORKLOAD_STATUS_PENDING {
		t.Fatalf("start records intent (starting maps to pending): %v", started.Msg.Workload.Status)
	}

	events, err := svcs.Workload.ListWorkloadEvents(ctx, connect.NewRequest(&v1.ListWorkloadEventsRequest{Id: wid}))
	if err != nil {
		t.Fatalf("ListWorkloadEvents: %v", err)
	}
	if len(events.Msg.Events) < 2 {
		t.Fatalf("expected created+start events, got %d", len(events.Msg.Events))
	}

	// Live execution belongs to the node agent.
	if _, err := svcs.Workload.SendWorkloadCommand(ctx, connect.NewRequest(&v1.SendWorkloadCommandRequest{Id: wid, Command: "list"})); connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Fatalf("expected Unimplemented for console commands, got %v", err)
	}

	if _, err := svcs.Workload.DeleteWorkload(ctx, connect.NewRequest(&v1.DeleteWorkloadRequest{Id: wid})); err != nil {
		t.Fatalf("DeleteWorkload: %v", err)
	}
}

func TestRoleCatalogueAndBindings(t *testing.T) {
	t.Parallel()
	svcs := testBundle(t)
	ctx := orgCtx()

	roles, err := svcs.Role.ListRoles(ctx, connect.NewRequest(&v1.ListRolesRequest{}))
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles.Msg.Roles) == 0 {
		t.Fatal("role catalogue must not be empty")
	}

	perms, err := svcs.Role.ListPermissions(ctx, connect.NewRequest(&v1.ListPermissionsRequest{}))
	if err != nil {
		t.Fatalf("ListPermissions: %v", err)
	}
	if len(perms.Msg.Permissions) == 0 {
		t.Fatal("permission catalogue must not be empty")
	}

	// Unknown permissions are rejected.
	if _, err := svcs.Role.CreateRoleBinding(ctx, connect.NewRequest(&v1.CreateRoleBindingRequest{
		SubjectType: "user", SubjectId: "u-1", Permissions: []string{"nope.unknown"},
	})); err == nil {
		t.Fatal("expected error for unknown permission")
	}

	one := perms.Msg.Permissions[0].Id
	bound, err := svcs.Role.CreateRoleBinding(ctx, connect.NewRequest(&v1.CreateRoleBindingRequest{
		SubjectType: "user", SubjectId: "u-1", ResourceType: "node", ResourceId: "n-1",
		Permissions: []string{one},
	}))
	if err != nil {
		t.Fatalf("CreateRoleBinding: %v", err)
	}
	listed, err := svcs.Role.ListRoleBindings(ctx, connect.NewRequest(&v1.ListRoleBindingsRequest{SubjectId: "u-1"}))
	if err != nil {
		t.Fatalf("ListRoleBindings: %v", err)
	}
	if len(listed.Msg.Bindings) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(listed.Msg.Bindings))
	}
	if _, err := svcs.Role.DeleteRoleBinding(ctx, connect.NewRequest(&v1.DeleteRoleBindingRequest{Id: bound.Msg.Binding.Id})); err != nil {
		t.Fatalf("DeleteRoleBinding: %v", err)
	}
}

func TestNodeTypeCatalog(t *testing.T) {
	t.Parallel()
	svcs := testBundle(t)
	resp, err := svcs.NodeType.ListNodeTypes(sysCtx(), connect.NewRequest(&v1.ListNodeTypesRequest{}))
	if err != nil {
		t.Fatalf("ListNodeTypes: %v", err)
	}
	if len(resp.Msg.NodeTypes) == 0 {
		t.Fatal("seeded catalog must not be empty")
	}
	got, err := svcs.NodeType.GetNodeType(sysCtx(), connect.NewRequest(&v1.GetNodeTypeRequest{Id: resp.Msg.NodeTypes[0].Id}))
	if err != nil {
		t.Fatalf("GetNodeType: %v", err)
	}
	if got.Msg.NodeType.Id != resp.Msg.NodeTypes[0].Id {
		t.Fatal("catalog get mismatch")
	}
	if _, err := svcs.NodeType.GetNodeType(sysCtx(), connect.NewRequest(&v1.GetNodeTypeRequest{Id: "nope"})); err == nil {
		t.Fatal("expected not-found for unknown node type")
	}
	provs, err := svcs.NodeType.ListProviders(sysCtx(), connect.NewRequest(&v1.ListProvidersRequest{}))
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if len(provs.Msg.Providers) == 0 {
		t.Fatal("provider list must not be empty")
	}
}

func TestProvisionWithoutProvisioner(t *testing.T) {
	t.Parallel()
	svcs := testBundle(t)
	ctx := orgCtx()

	// Nil provisioner degrades to Unavailable, never a panic.
	if _, err := svcs.Provision.CreateProvision(ctx, connect.NewRequest(&v1.CreateProvisionRequest{
		Name: "web-1", Provider: v1.ProviderId_PROVIDER_ID_HETZNER, Region: "fsn1", NodeTypeId: "small",
	})); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("expected Unavailable without provisioner, got %v", err)
	}

	listed, err := svcs.Provision.ListProvisions(ctx, connect.NewRequest(&v1.ListProvisionsRequest{}))
	if err != nil {
		t.Fatalf("ListProvisions: %v", err)
	}
	if len(listed.Msg.Provisions) != 0 {
		t.Fatalf("expected empty provision list, got %d", len(listed.Msg.Provisions))
	}
}

func TestSessionAndAuditReads(t *testing.T) {
	t.Parallel()
	svcs := testBundle(t)
	ctx := orgCtx()

	sess, err := svcs.Session.GetSession(ctx, connect.NewRequest(&v1.GetSessionRequest{}))
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Msg.Principal.UserId != "user-1" || !sess.Msg.ViaApiKey {
		t.Fatalf("bad session: %+v", sess.Msg)
	}

	perms, err := svcs.Session.ListMyPermissions(ctx, connect.NewRequest(&v1.ListMyPermissionsRequest{}))
	if err != nil {
		t.Fatalf("ListMyPermissions: %v", err)
	}
	if len(perms.Msg.Permissions) == 0 {
		t.Fatal("admin must hold permissions")
	}

	trail, err := svcs.Audit.ListAuditEvents(ctx, connect.NewRequest(&v1.ListAuditEventsRequest{}))
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(trail.Msg.Events) != 0 {
		t.Fatalf("fresh org must have an empty trail, got %d", len(trail.Msg.Events))
	}
	if _, err := svcs.Audit.ListAuditEvents(ctx, connect.NewRequest(&v1.ListAuditEventsRequest{ActionPrefix: "nodes.*"})); err == nil {
		t.Fatal("expected wildcard prefix rejection")
	}
}

func TestWorkloadQuotaEnforcement(t *testing.T) {
	t.Parallel()
	enforcer := billing.NewQuotaEnforcer(billing.NewCatalog())
	svcs, store := testBundleCustom(t, func(d *Deps) {
		d.Billing = enforcer
	})
	ctx := orgCtx()

	// Seed testOrg in db with plan "free" (MaxWorkloads: 2)
	err := store.Unscoped().Create(&db.Org{
		ID:   testOrg,
		Name: "Test Org",
		Slug: "test-org",
		Plan: "free",
	}).Error
	if err != nil {
		t.Fatalf("create test org: %v", err)
	}

	// 1st workload -> success
	_, err = svcs.Workload.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
		NodeId: "node-1",
		Name:   "mc-1",
		Spec:   &v1.WorkloadSpec{MemoryMb: 1024},
	}))
	if err != nil {
		t.Fatalf("create workload 1: %v", err)
	}

	// 2nd workload -> success
	_, err = svcs.Workload.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
		NodeId: "node-1",
		Name:   "mc-2",
		Spec:   &v1.WorkloadSpec{MemoryMb: 1024},
	}))
	if err != nil {
		t.Fatalf("create workload 2: %v", err)
	}

	// 3rd workload -> exceeds free plan (max 2), must return CodeResourceExhausted
	_, err = svcs.Workload.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
		NodeId: "node-1",
		Name:   "mc-3",
		Spec:   &v1.WorkloadSpec{MemoryMb: 1024},
	}))
	if err == nil {
		t.Fatal("expected error on 3rd workload for free plan, got nil")
	}
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("expected CodeResourceExhausted, got %v", err)
	}

	// Upgrade org to "pro" (max 15 workloads)
	if err := store.Unscoped().Model(&db.Org{}).Where("id = ?", testOrg).Update("plan", "pro").Error; err != nil {
		t.Fatalf("upgrade org: %v", err)
	}

	// 3rd workload retry -> success
	_, err = svcs.Workload.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
		NodeId: "node-1",
		Name:   "mc-3",
		Spec:   &v1.WorkloadSpec{MemoryMb: 1024},
	}))
	if err != nil {
		t.Fatalf("expected success after upgrade, got: %v", err)
	}
}

func TestNodeQuotaEnforcement(t *testing.T) {
	t.Parallel()
	enforcer := billing.NewQuotaEnforcer(billing.NewCatalog())
	svcs, store := testBundleCustom(t, func(d *Deps) {
		d.Billing = enforcer
	})
	ctx := orgCtx()

	// Seed testOrg in db with plan "free" (MaxNodes: 1)
	err := store.Unscoped().Create(&db.Org{
		ID:   testOrg,
		Name: "Test Org Free Nodes",
		Slug: "test-org-free-nodes",
		Plan: "free",
	}).Error
	if err != nil {
		t.Fatalf("create test org: %v", err)
	}

	// Issue join token
	tokenResp, err := svcs.Node.CreateJoinToken(ctx, connect.NewRequest(&v1.CreateJoinTokenRequest{
		Name:       "token-1",
		TtlSeconds: 3600,
	}))
	if err != nil {
		t.Fatalf("issue join token: %v", err)
	}

	// First node join -> success
	joinResp, err := svcs.Agent.JoinNode(context.Background(), connect.NewRequest(&v1.JoinNodeRequest{
		Token: tokenResp.Msg.Secret,
		Capacity: &v1.NodeCapacity{
			Vcpu:   2,
			RamMb:  4096,
			DiskGb: 50,
		},
		Hostname:     "node-box-1",
		AgentVersion: "v1.0.0",
	}))
	if err != nil {
		t.Fatalf("node 1 join: %v", err)
	}
	if joinResp.Msg.Identity.NodeId == "" {
		t.Fatal("expected joined node id")
	}

	// Issue another token
	tokenResp2, err := svcs.Node.CreateJoinToken(ctx, connect.NewRequest(&v1.CreateJoinTokenRequest{
		Name:       "token-2",
		TtlSeconds: 3600,
	}))
	if err != nil {
		t.Fatalf("issue join token 2: %v", err)
	}

	// Second node join -> exceeds free plan (max 1 node)
	_, err = svcs.Agent.JoinNode(context.Background(), connect.NewRequest(&v1.JoinNodeRequest{
		Token: tokenResp2.Msg.Secret,
		Capacity: &v1.NodeCapacity{
			Vcpu:   2,
			RamMb:  4096,
			DiskGb: 50,
		},
		Hostname:     "node-box-2",
		AgentVersion: "v1.0.0",
	}))
	if err == nil {
		t.Fatal("expected error on 2nd node join, got nil")
	}
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("expected CodeResourceExhausted, got %v", err)
	}
}

func TestWebhookDispatchLifecycle(t *testing.T) {
	t.Parallel()

	var receivedEvents []string
	var mu sync.Mutex
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p notify.WebhookPayload
		_ = json.Unmarshal(body, &p)
		mu.Lock()
		receivedEvents = append(receivedEvents, p.EventType)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	dispatcher := notify.NewDispatcher(ts.Client())
	dispatcher.Register(&notify.WebhookSubscription{
		ID:        "sub-1",
		OrgID:     testOrg,
		TargetURL: ts.URL,
		Secret:    "secret-123",
		Events:    []string{"*"},
		Enabled:   true,
	})

	svcs, store := testBundleCustom(t, func(d *Deps) {
		d.Notifier = dispatcher
	})
	ctx := orgCtx()

	// Seed testOrg in db
	_ = store.Unscoped().Create(&db.Org{
		ID:   testOrg,
		Name: "Webhook Org",
		Slug: "webhook-org",
		Plan: "team",
	}).Error

	// Create Workload -> should dispatch workload.created
	createResp, err := svcs.Workload.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
		NodeId: "node-wh-1",
		Name:   "mc-wh",
	}))
	if err != nil {
		t.Fatalf("create workload: %v", err)
	}
	wID := createResp.Msg.Workload.Id

	// Start Workload -> should dispatch workload.started
	_, err = svcs.Workload.StartWorkload(ctx, connect.NewRequest(&v1.StartWorkloadRequest{Id: wID}))
	if err != nil {
		t.Fatalf("start workload: %v", err)
	}

	// Stop Workload -> should dispatch workload.stopped
	_, err = svcs.Workload.StopWorkload(ctx, connect.NewRequest(&v1.StopWorkloadRequest{Id: wID}))
	if err != nil {
		t.Fatalf("stop workload: %v", err)
	}

	// Delete Workload -> should dispatch workload.deleted
	_, err = svcs.Workload.DeleteWorkload(ctx, connect.NewRequest(&v1.DeleteWorkloadRequest{Id: wID}))
	if err != nil {
		t.Fatalf("delete workload: %v", err)
	}

	// Invite member -> should dispatch org.invite.created
	_, err = svcs.Org.InviteMember(ctx, connect.NewRequest(&v1.InviteMemberRequest{
		Email: "newguy@example.com",
		Role:  v1.Role_ROLE_VIEWER,
	}))
	if err != nil {
		t.Fatalf("invite member: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	expected := []string{
		"workload.created",
		"workload.started",
		"workload.stopped",
		"workload.deleted",
		"org.invite.created",
	}

	for _, exp := range expected {
		found := false
		for _, rec := range receivedEvents {
			if rec == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected dispatched event %q, received: %v", exp, receivedEvents)
		}
	}
}

func TestNodeReconnectionSelfHealing(t *testing.T) {
	t.Parallel()

	var receivedEvents []string
	var mu sync.Mutex
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p notify.WebhookPayload
		_ = json.Unmarshal(body, &p)
		mu.Lock()
		receivedEvents = append(receivedEvents, p.EventType)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	dispatcher := notify.NewDispatcher(ts.Client())
	dispatcher.Register(&notify.WebhookSubscription{
		ID:        "sub-healing",
		OrgID:     testOrg,
		TargetURL: ts.URL,
		Secret:    "secret-123",
		Events:    []string{"*"},
		Enabled:   true,
	})

	svcs, store := testBundleCustom(t, func(d *Deps) {
		d.Notifier = dispatcher
	})
	ctx := orgCtx()

	// 1. Seed org
	_ = store.Unscoped().Create(&db.Org{
		ID:   testOrg,
		Name: "Healing Org",
		Slug: "healing-org",
	}).Error

	// 2. Register node
	tokenResp, err := svcs.Node.CreateJoinToken(ctx, connect.NewRequest(&v1.CreateJoinTokenRequest{
		Name:       "node-heal-tok",
		TtlSeconds: 3600,
	}))
	if err != nil {
		t.Fatalf("issue join token: %v", err)
	}
	joinResp, err := svcs.Agent.JoinNode(context.Background(), connect.NewRequest(&v1.JoinNodeRequest{
		Token:        tokenResp.Msg.Secret,
		Hostname:     "box-heal-1",
		AgentVersion: "v1.0.0",
	}))
	if err != nil {
		t.Fatalf("join node: %v", err)
	}
	nodeID := joinResp.Msg.Identity.NodeId

	// 3. Create a workload on that node
	createWl, err := svcs.Workload.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
		NodeId: nodeID,
		Name:   "mc-heal",
	}))
	if err != nil {
		t.Fatalf("create workload: %v", err)
	}
	wlID := createWl.Msg.Workload.Id

	// 4. Simulate node going offline & workload degrading
	err = store.Unscoped().Model(&db.Node{}).Where("id = ?", nodeID).Update("status", "offline").Error
	if err != nil {
		t.Fatalf("update node offline: %v", err)
	}
	err = store.Unscoped().Model(&db.Workload{}).Where("id = ?", wlID).Update("status", "degraded").Error
	if err != nil {
		t.Fatalf("update workload degraded: %v", err)
	}

	// Verify node reads back as offline and workload in DB is degraded
	nodeGet, err := svcs.Node.GetNode(ctx, connect.NewRequest(&v1.GetNodeRequest{Id: nodeID}))
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if nodeGet.Msg.Node.Status != v1.NodeStatus_NODE_STATUS_OFFLINE {
		t.Fatalf("expected node offline, got %v", nodeGet.Msg.Node.Status)
	}
	var wlPre db.Workload
	if err := store.Unscoped().Where("id = ?", wlID).First(&wlPre).Error; err != nil {
		t.Fatalf("find pre-healing workload: %v", err)
	}
	if wlPre.Status != "degraded" {
		t.Fatalf("expected workload degraded, got %s", wlPre.Status)
	}

	// 5. Node reconnects and streams heartbeat
	nodeCtx := principal.WithPrincipal(context.Background(), principal.Principal{
		Kind:  principal.KindNode,
		OrgID: testOrg,
	})
	svcs.Agent.onHeartbeat(nodeCtx, &v1.AgentHeartbeat{
		NodeId: nodeID,
	})

	// 6. Verify self-healing:
	// - Node is back online
	nodeAfter, err := svcs.Node.GetNode(ctx, connect.NewRequest(&v1.GetNodeRequest{Id: nodeID}))
	if err != nil {
		t.Fatalf("get node after heartbeat: %v", err)
	}
	if nodeAfter.Msg.Node.Status != v1.NodeStatus_NODE_STATUS_ONLINE {
		t.Fatalf("expected node online after recovery, got %v", nodeAfter.Msg.Node.Status)
	}

	// - Workload is restored to running
	wlAfter, err := svcs.Workload.GetWorkload(ctx, connect.NewRequest(&v1.GetWorkloadRequest{Id: wlID}))
	if err != nil {
		t.Fatalf("get workload after recovery: %v", err)
	}
	if wlAfter.Msg.Workload.Status != v1.WorkloadStatus_WORKLOAD_STATUS_RUNNING {
		t.Fatalf("expected workload restored to running, got %v", wlAfter.Msg.Workload.Status)
	}

	// - WorkloadEvent has recovery entry
	var evts []db.WorkloadEvent
	err = store.Unscoped().Where("workload_id = ? AND kind = ?", wlID, "recovery").Find(&evts).Error
	if err != nil || len(evts) == 0 {
		t.Fatalf("expected recovery workload event, got err=%v count=%d", err, len(evts))
	}

	// - Webhooks dispatched: node.online and workload.restored
	mu.Lock()
	defer mu.Unlock()
	for _, expectedEvt := range []string{"node.online", "workload.restored"} {
		found := false
		for _, rec := range receivedEvents {
			if rec == expectedEvt {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected webhook event %q, received: %v", expectedEvt, receivedEvents)
		}
	}
}
