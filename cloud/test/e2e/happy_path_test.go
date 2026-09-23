package e2e_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/audit"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/auth"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/httpapi"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/keys"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/node"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/nodetype"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/provision"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/rbac"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/secrets"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/svc"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/athNdev/carbon-panel/pkg/proto/cloud/v1/cloudv1connect"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type fakeClerkVerifier struct {
	tokens map[string]auth.Claims
}

func (f *fakeClerkVerifier) Verify(_ context.Context, tok string) (auth.Claims, error) {
	if c, ok := f.tokens[tok]; ok {
		return c, nil
	}
	return auth.Claims{}, errors.New("invalid token")
}

type fakeSecretsProvider map[string]string

func (m fakeSecretsProvider) Kind() string { return "test" }
func (m fakeSecretsProvider) Get(_ context.Context, key string) (string, error) {
	if v, ok := m[key]; ok && v != "" {
		return v, nil
	}
	return "", secrets.ErrNotFound
}
func (m fakeSecretsProvider) Health(_ context.Context) error { return nil }

func TestEndToEndHappyPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. Initialize DB Store
	store, err := db.Open(db.Options{
		Driver:      "sqlite",
		DSN:         ":memory:",
		AutoMigrate: true,
	})
	require.NoError(t, err)

	// Seed catalog and default permissions
	require.NoError(t, store.SeedDefaults(ctx))
	catalog := nodetype.NewCatalog(store)
	require.NoError(t, catalog.EnsureSeeded(ctx))

	pepper := []byte("test-e2e-pepper-32bytes-secret!!")
	nodeDeps := node.Deps{Store: store, Pepper: pepper}
	nodesService := node.NewService(nodeDeps)
	joinTokenService := node.NewJoinTokenService(nodeDeps)
	auditsStore := audit.NewGormStore(store)

	secProvider := fakeSecretsProvider{
		keys.ProviderHetznerToken: "fake-hetzner-token-1234567890abcdef",
	}

	fakeRunner := &provision.FakeRunner{}

	provSvc, err := provision.NewService(provision.Options{
		Root:            t.TempDir(),
		TerraformPath:   "tofu",
		StateBackend:    "local",
		StateLocalDir:   t.TempDir(),
		Store:           store,
		Secrets:         secProvider,
		Runner:          fakeRunner,
		ControlPlaneURL: "http://127.0.0.1:8080",
		LogBufferSize:   100,
	})
	require.NoError(t, err)

	// 2. Mock Clerk Verifier
	mockVerifier := &fakeClerkVerifier{
		tokens: map[string]auth.Claims{
			"valid-clerk-token": {
				Subject: "user_clerk_owner_1",
				Email:   "owner@test.org",
			},
			"unauthorized-token": {
				Subject: "user_hacker",
				Email:   "hacker@evil.org",
			},
		},
	}

	// 3. Build RBAC Engine & RPC Services bundle
	engine, err := rbac.New(rbac.Options{Source: &httpapi.BindingSource{Store: store}})
	require.NoError(t, err)

	services, err := svc.New(svc.Deps{
		Store:           store,
		Nodes:           nodesService,
		JoinTokens:      joinTokenService,
		Catalog:         catalog,
		Provision:       provSvc,
		Audits:          auditsStore,
		ControlPlaneURL: "http://127.0.0.1:8080",
	})
	require.NoError(t, err)

	// Build production HTTP server with authenticating and authorizing interceptor chain
	apiServer, err := httpapi.New(httpapi.Options{
		Store:    store,
		Services: services,
		Verifier: mockVerifier,
		Engine:   engine,
		Audits:   auditsStore,
	})
	require.NoError(t, err)

	// Run H2C Server for real network protocol test
	srv := httptest.NewServer(h2c.NewHandler(apiServer.Handler(), &http2.Server{}))
	t.Cleanup(srv.Close)

	clientHttpClient := srv.Client()

	// Connect Clients
	orgClient := cloudv1connect.NewOrgServiceClient(clientHttpClient, srv.URL)
	nodeClient := cloudv1connect.NewNodeServiceClient(clientHttpClient, srv.URL)
	agentClient := cloudv1connect.NewAgentServiceClient(clientHttpClient, srv.URL)
	provisionClient := cloudv1connect.NewProvisionServiceClient(clientHttpClient, srv.URL)
	nodeTypeClient := cloudv1connect.NewNodeTypeServiceClient(clientHttpClient, srv.URL)
	workloadClient := cloudv1connect.NewWorkloadServiceClient(clientHttpClient, srv.URL)
	auditClient := cloudv1connect.NewAuditServiceClient(clientHttpClient, srv.URL)

	// -------------------------------------------------------------
	// STEP A: Bootstrap Tenant & Verify RBAC
	// -------------------------------------------------------------
	createOrgReq := connect.NewRequest(&v1.CreateOrgRequest{
		Name: "Acme Cloud Tenant",
		Slug: "acme-cloud",
	})
	createOrgReq.Header().Set("Authorization", "Bearer valid-clerk-token")

	orgResp, err := orgClient.CreateOrg(ctx, createOrgReq)
	require.NoError(t, err)
	orgID := orgResp.Msg.Org.Id
	require.NotEmpty(t, orgID)
	require.Equal(t, "Acme Cloud Tenant", orgResp.Msg.Org.Name)

	// Update mockVerifier claims now that the tenant exists so subsequent
	// session requests carry org context
	mockVerifier.tokens["valid-clerk-token"] = auth.Claims{
		Subject: "user_clerk_owner_1",
		Email:   "owner@test.org",
		OrgID:   orgID,
		OrgRole: "org:admin",
	}

	// Verify unauthenticated / unauthorized requests fail
	badAuthReq := connect.NewRequest(&v1.ListNodesRequest{})
	badAuthReq.Header().Set("Authorization", "Bearer definitely-bad-token")
	_, err = nodeClient.ListNodes(ctx, badAuthReq)
	require.Error(t, err)
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))

	unauthReq := connect.NewRequest(&v1.ListNodesRequest{})
	unauthReq.Header().Set("Authorization", "Bearer unauthorized-token")
	_, err = nodeClient.ListNodes(ctx, unauthReq)
	require.Error(t, err)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	// -------------------------------------------------------------
	// STEP B: Join Token Generation & BYON Node Redemption
	// -------------------------------------------------------------
	issueReq := connect.NewRequest(&v1.CreateJoinTokenRequest{
		Name:       "byo-server-munich",
		NodeTypeId: "small",
	})
	issueReq.Header().Set("Authorization", "Bearer valid-clerk-token")
	tokenResp, err := nodeClient.CreateJoinToken(ctx, issueReq)
	require.NoError(t, err)
	rawJoinSecret := tokenResp.Msg.Secret
	require.True(t, strings.HasPrefix(rawJoinSecret, "ccj_"))

	// Agent redeems join token
	joinReq := connect.NewRequest(&v1.JoinNodeRequest{
		Token:        rawJoinSecret,
		Hostname:     "node-munich-01.acme.internal",
		AgentVersion: "0.2.0-test",
		Capacity: &v1.NodeCapacity{
			Vcpu:   8,
			RamMb:  16384,
			DiskGb: 200,
		},
	})
	joinResp, err := agentClient.JoinNode(ctx, joinReq)
	require.NoError(t, err)
	byoNodeID := joinResp.Msg.Identity.NodeId
	require.NotEmpty(t, byoNodeID)
	require.Equal(t, orgID, joinResp.Msg.Identity.OrgId)

	// Verify single-use token semantics
	_, err = agentClient.JoinNode(ctx, joinReq)
	require.Error(t, err, "expected single-use token to fail on reuse")

	// -------------------------------------------------------------
	// STEP C: Node Heartbeat & Status
	// -------------------------------------------------------------
	orgScopedCtx := principal.WithPrincipal(ctx, principal.Principal{
		Kind:   principal.KindSession,
		UserID: "user_clerk_owner_1",
		OrgID:  orgID,
		Role:   "owner",
	})
	_, hbErr := nodesService.Heartbeat(orgScopedCtx, byoNodeID)
	require.NoError(t, hbErr)

	// Verify node appears in tenant node list
	listNodesReq := connect.NewRequest(&v1.ListNodesRequest{})
	listNodesReq.Header().Set("Authorization", "Bearer valid-clerk-token")
	nodesList, err := nodeClient.ListNodes(ctx, listNodesReq)
	require.NoError(t, err)
	require.Len(t, nodesList.Msg.Nodes, 1)
	require.Equal(t, byoNodeID, nodesList.Msg.Nodes[0].Id)
	require.Equal(t, v1.NodeStatus_NODE_STATUS_ONLINE, nodesList.Msg.Nodes[0].Status)

	// -------------------------------------------------------------
	// STEP D: Managed Node Provisioning (Tofu / Terraform driver)
	// -------------------------------------------------------------
	createProvReq := connect.NewRequest(&v1.CreateProvisionRequest{
		Name:         "managed-hetzner-node",
		Provider:     v1.ProviderId_PROVIDER_ID_HETZNER,
		Region:       "fsn1",
		NodeTypeId:   "small",
		PlanOnly:     true,
		ProviderVars: map[string]string{"ssh_key": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFAKEPUBLICKEY"},
	})
	createProvReq.Header().Set("Authorization", "Bearer valid-clerk-token")
	provResp, err := provisionClient.CreateProvision(ctx, createProvReq)
	require.NoError(t, err)
	provID := provResp.Msg.Provision.Id
	require.NotEmpty(t, provID)

	// Run plan
	planReq := connect.NewRequest(&v1.PlanProvisionRequest{
		Id: provID,
	})
	planReq.Header().Set("Authorization", "Bearer valid-clerk-token")
	plannedResp, err := provisionClient.PlanProvision(ctx, planReq)
	require.NoError(t, err)
	require.Equal(t, v1.ProvisionStatus_PROVISION_STATUS_PLANNED, plannedResp.Msg.Provision.Status)
	require.Contains(t, plannedResp.Msg.Provision.PlanSummary, "provider=hetzner")
	require.Contains(t, plannedResp.Msg.Provision.PlanDiff, "1 to add")

	// -------------------------------------------------------------
	// STEP E: Node Type Catalog & Capacity Accounting
	// -------------------------------------------------------------
	listTypesReq := connect.NewRequest(&v1.ListNodeTypesRequest{})
	listTypesReq.Header().Set("Authorization", "Bearer valid-clerk-token")
	typesResp, err := nodeTypeClient.ListNodeTypes(ctx, listTypesReq)
	require.NoError(t, err)
	require.NotEmpty(t, typesResp.Msg.NodeTypes)

	// Verify Capacity Check against node
	capacityChecker := node.NewCapacity(nodeDeps)

	// 1. Fits within 16384 RAM
	err = capacityChecker.Check(orgScopedCtx, byoNodeID, db.NodeCapacity{VCPU: 2, RAMMB: 4096, DiskGB: 50})
	require.NoError(t, err)

	// 2. Exceeds capacity -> CapacityExceeded
	err = capacityChecker.Check(orgScopedCtx, byoNodeID, db.NodeCapacity{VCPU: 16, RAMMB: 32768, DiskGB: 500})
	require.ErrorIs(t, err, node.ErrCapacityExceeded)

	// -------------------------------------------------------------
	// STEP F: Workload Schedule & Lifecycle
	// -------------------------------------------------------------
	createWlReq := connect.NewRequest(&v1.CreateWorkloadRequest{
		Name:   "survival-minecraft-01",
		NodeId: byoNodeID,
		Spec: &v1.WorkloadSpec{
			Loader:           "paper",
			MinecraftVersion: "1.21",
			MemoryMb:         4096,
		},
	})
	createWlReq.Header().Set("Authorization", "Bearer valid-clerk-token")
	wlResp, err := workloadClient.CreateWorkload(ctx, createWlReq)
	require.NoError(t, err)
	wlID := wlResp.Msg.Workload.Id
	require.NotEmpty(t, wlID)
	require.Equal(t, v1.WorkloadStatus_WORKLOAD_STATUS_PENDING, wlResp.Msg.Workload.Status)

	// Start workload
	startWlReq := connect.NewRequest(&v1.StartWorkloadRequest{Id: wlID})
	startWlReq.Header().Set("Authorization", "Bearer valid-clerk-token")
	_, err = workloadClient.StartWorkload(ctx, startWlReq)
	require.NoError(t, err)

	// Check events
	eventsReq := connect.NewRequest(&v1.ListWorkloadEventsRequest{Id: wlID})
	eventsReq.Header().Set("Authorization", "Bearer valid-clerk-token")
	eventsResp, err := workloadClient.ListWorkloadEvents(ctx, eventsReq)
	require.NoError(t, err)
	require.NotEmpty(t, eventsResp.Msg.Events)

	// -------------------------------------------------------------
	// STEP G: Audit Log Records Every State Change
	// -------------------------------------------------------------
	auditReq := connect.NewRequest(&v1.ListAuditEventsRequest{})
	auditReq.Header().Set("Authorization", "Bearer valid-clerk-token")
	auditList, err := auditClient.ListAuditEvents(ctx, auditReq)
	require.NoError(t, err)
	require.NotEmpty(t, auditList.Msg.Events)

	actionsRecorded := map[string]bool{}
	for _, evt := range auditList.Msg.Events {
		require.Equal(t, orgID, evt.OrgId)
		require.Equal(t, "user_clerk_owner_1", evt.ActorUserId)
		actionsRecorded[evt.Action] = true
	}

	// Verify mutating actions were captured
	require.True(t, actionsRecorded["nodes.create_join_token"], "expected nodes.create_join_token in audit log")
	require.True(t, actionsRecorded["provisions.create"], "expected provisions.create in audit log")
	require.True(t, actionsRecorded["workloads.create"], "expected workloads.create in audit log")
	require.True(t, actionsRecorded["workloads.start"], "expected workloads.start in audit log")
}
