package e2e_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/audit"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/auth"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/httpapi"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/keys"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/node"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/nodetype"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/provision"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/rbac"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/svc"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/athNdev/carbon-panel/pkg/proto/cloud/v1/cloudv1connect"
	"github.com/stretchr/testify/require"
)

func TestCrossOrgIsolationRPCs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. Storage & services setup
	store, err := db.Open(db.Options{
		Driver:      "sqlite",
		DSN:         ":memory:",
		AutoMigrate: true,
	})
	require.NoError(t, err)

	require.NoError(t, store.SeedDefaults(ctx))
	catalog := nodetype.NewCatalog(store)
	require.NoError(t, catalog.EnsureSeeded(ctx))

	pepper := []byte("cross-org-isolation-test-pepper!")
	nodeDeps := node.Deps{Store: store, Pepper: pepper}
	nodesService := node.NewService(nodeDeps)
	joinTokenService := node.NewJoinTokenService(nodeDeps)
	auditsStore := audit.NewGormStore(store)
	fakeRunner := &provision.FakeRunner{}

	secProvider := fakeSecretsProvider{
		keys.ProviderHetznerToken: "fake-hetzner-token-1234567890abcdef",
	}

	provSvc, err := provision.NewService(provision.Options{
		Root:            t.TempDir(),
		TerraformPath:   "tofu",
		StateBackend:    "local",
		StateLocalDir:   t.TempDir(),
		Store:           store,
		Secrets:         secProvider,
		Runner:          fakeRunner,
		ControlPlaneURL: "http://127.0.0.1:8080",
		LogBufferSize:   50,
	})
	require.NoError(t, err)

	mockVerifier := &fakeClerkVerifier{
		tokens: map[string]auth.Claims{},
	}

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

	apiServer, err := httpapi.New(httpapi.Options{
		Store:    store,
		Services: services,
		Verifier: mockVerifier,
		Engine:   engine,
		Audits:   auditsStore,
	})
	require.NoError(t, err)

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	srv := httptest.NewUnstartedServer(apiServer.Handler())
	srv.Config.Protocols = protocols
	srv.Start()
	t.Cleanup(srv.Close)

	clientHttpClient := srv.Client()

	orgClient := cloudv1connect.NewOrgServiceClient(clientHttpClient, srv.URL)
	nodeClient := cloudv1connect.NewNodeServiceClient(clientHttpClient, srv.URL)
	agentClient := cloudv1connect.NewAgentServiceClient(clientHttpClient, srv.URL)
	provisionClient := cloudv1connect.NewProvisionServiceClient(clientHttpClient, srv.URL)
	workloadClient := cloudv1connect.NewWorkloadServiceClient(clientHttpClient, srv.URL)
	apiKeyClient := cloudv1connect.NewApiKeyServiceClient(clientHttpClient, srv.URL)
	auditClient := cloudv1connect.NewAuditServiceClient(clientHttpClient, srv.URL)

	// 2. Create Org Alpha (Attacker context)
	mockVerifier.tokens["token-user-a"] = auth.Claims{
		Subject: "user_alpha_admin",
		Email:   "alice@alpha.org",
	}
	createOrgAReq := connect.NewRequest(&v1.CreateOrgRequest{
		Name: "Org Alpha",
		Slug: "org-alpha",
	})
	createOrgAReq.Header().Set("Authorization", "Bearer token-user-a")
	orgAResp, err := orgClient.CreateOrg(ctx, createOrgAReq)
	require.NoError(t, err)
	orgAID := orgAResp.Msg.Org.Id

	// Update Alice claims to Org Alpha
	mockVerifier.tokens["token-user-a"] = auth.Claims{
		Subject: "user_alpha_admin",
		Email:   "alice@alpha.org",
		OrgID:   orgAID,
		OrgRole: "org:admin",
	}

	// 3. Create Org Beta (Target context)
	mockVerifier.tokens["token-user-b"] = auth.Claims{
		Subject: "user_beta_admin",
		Email:   "bob@beta.org",
	}
	createOrgBReq := connect.NewRequest(&v1.CreateOrgRequest{
		Name: "Org Beta",
		Slug: "org-beta",
	})
	createOrgBReq.Header().Set("Authorization", "Bearer token-user-b")
	orgBResp, err := orgClient.CreateOrg(ctx, createOrgBReq)
	require.NoError(t, err)
	orgBID := orgBResp.Msg.Org.Id

	// Update Bob claims to Org Beta
	mockVerifier.tokens["token-user-b"] = auth.Claims{
		Subject: "user_beta_admin",
		Email:   "bob@beta.org",
		OrgID:   orgBID,
		OrgRole: "org:admin",
	}

	// 4. Bob creates resources inside Org Beta
	// 4a. Join Token & Node in Beta
	tokReq := connect.NewRequest(&v1.CreateJoinTokenRequest{Name: "beta-node-token"})
	tokReq.Header().Set("Authorization", "Bearer token-user-b")
	tokResp, err := nodeClient.CreateJoinToken(ctx, tokReq)
	require.NoError(t, err)
	tokenBetaID := tokResp.Msg.Token.Id
	rawSecretBeta := tokResp.Msg.Secret

	joinReq := connect.NewRequest(&v1.JoinNodeRequest{
		Token:        rawSecretBeta,
		Hostname:     "beta-node-01.internal",
		AgentVersion: "v1.0.0",
		Capacity:     &v1.NodeCapacity{Vcpu: 4, RamMb: 8192, DiskGb: 100},
	})
	joinResp, err := agentClient.JoinNode(ctx, joinReq)
	require.NoError(t, err)
	nodeBetaID := joinResp.Msg.Identity.NodeId

	// 4b. Workload in Beta
	wlReq := connect.NewRequest(&v1.CreateWorkloadRequest{
		Name:   "beta-workload-01",
		NodeId: nodeBetaID,
		Spec:   &v1.WorkloadSpec{Loader: "vanilla", MemoryMb: 1024},
	})
	wlReq.Header().Set("Authorization", "Bearer token-user-b")
	wlResp, err := workloadClient.CreateWorkload(ctx, wlReq)
	require.NoError(t, err)
	wlBetaID := wlResp.Msg.Workload.Id

	// 4c. Provision in Beta
	provReq := connect.NewRequest(&v1.CreateProvisionRequest{
		Name:         "beta-prov-01",
		Provider:     v1.ProviderId_PROVIDER_ID_HETZNER,
		Region:       "fsn1",
		NodeTypeId:   "small",
		PlanOnly:     true,
		ProviderVars: map[string]string{"ssh_key": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFAKEPUBLICKEY"},
	})
	provReq.Header().Set("Authorization", "Bearer token-user-b")
	provResp, err := provisionClient.CreateProvision(ctx, provReq)
	require.NoError(t, err)
	provBetaID := provResp.Msg.Provision.Id

	// 4d. API Key in Beta
	keyReq := connect.NewRequest(&v1.CreateApiKeyRequest{
		Name:        "beta-ci-key",
		Permissions: []string{"nodes.list"},
	})
	keyReq.Header().Set("Authorization", "Bearer token-user-b")
	keyResp, err := apiKeyClient.CreateApiKey(ctx, keyReq)
	require.NoError(t, err)
	apiKeyBetaID := keyResp.Msg.ApiKey.Id

	// -------------------------------------------------------------
	// 5. Alice (Org Alpha) attempts cross-org access against Org Beta
	// Every single operation must be rejected with CodeNotFound or CodePermissionDenied.
	// -------------------------------------------------------------
	testCases := []struct {
		name string
		exec func() error
	}{
		{
			name: "NodeService.GetNode cross-org access",
			exec: func() error {
				req := connect.NewRequest(&v1.GetNodeRequest{Id: nodeBetaID})
				req.Header().Set("Authorization", "Bearer token-user-a")
				_, err := nodeClient.GetNode(ctx, req)
				return err
			},
		},
		{
			name: "NodeService.DrainNode cross-org mutation",
			exec: func() error {
				req := connect.NewRequest(&v1.DrainNodeRequest{Id: nodeBetaID})
				req.Header().Set("Authorization", "Bearer token-user-a")
				_, err := nodeClient.DrainNode(ctx, req)
				return err
			},
		},
		{
			name: "NodeService.ResumeNode cross-org mutation",
			exec: func() error {
				req := connect.NewRequest(&v1.ResumeNodeRequest{Id: nodeBetaID})
				req.Header().Set("Authorization", "Bearer token-user-a")
				_, err := nodeClient.ResumeNode(ctx, req)
				return err
			},
		},
		{
			name: "NodeService.DeleteNode cross-org deletion",
			exec: func() error {
				req := connect.NewRequest(&v1.DeleteNodeRequest{Id: nodeBetaID})
				req.Header().Set("Authorization", "Bearer token-user-a")
				_, err := nodeClient.DeleteNode(ctx, req)
				return err
			},
		},
		{
			name: "NodeService.RevokeJoinToken cross-org revocation",
			exec: func() error {
				req := connect.NewRequest(&v1.RevokeJoinTokenRequest{Id: tokenBetaID})
				req.Header().Set("Authorization", "Bearer token-user-a")
				_, err := nodeClient.RevokeJoinToken(ctx, req)
				return err
			},
		},
		{
			name: "WorkloadService.GetWorkload cross-org access",
			exec: func() error {
				req := connect.NewRequest(&v1.GetWorkloadRequest{Id: wlBetaID})
				req.Header().Set("Authorization", "Bearer token-user-a")
				_, err := workloadClient.GetWorkload(ctx, req)
				return err
			},
		},
		{
			name: "WorkloadService.StartWorkload cross-org start",
			exec: func() error {
				req := connect.NewRequest(&v1.StartWorkloadRequest{Id: wlBetaID})
				req.Header().Set("Authorization", "Bearer token-user-a")
				_, err := workloadClient.StartWorkload(ctx, req)
				return err
			},
		},
		{
			name: "WorkloadService.StopWorkload cross-org stop",
			exec: func() error {
				req := connect.NewRequest(&v1.StopWorkloadRequest{Id: wlBetaID})
				req.Header().Set("Authorization", "Bearer token-user-a")
				_, err := workloadClient.StopWorkload(ctx, req)
				return err
			},
		},
		{
			name: "WorkloadService.DeleteWorkload cross-org deletion",
			exec: func() error {
				req := connect.NewRequest(&v1.DeleteWorkloadRequest{Id: wlBetaID})
				req.Header().Set("Authorization", "Bearer token-user-a")
				_, err := workloadClient.DeleteWorkload(ctx, req)
				return err
			},
		},
		{
			name: "ProvisionService.GetProvision cross-org access",
			exec: func() error {
				req := connect.NewRequest(&v1.GetProvisionRequest{Id: provBetaID})
				req.Header().Set("Authorization", "Bearer token-user-a")
				_, err := provisionClient.GetProvision(ctx, req)
				return err
			},
		},
		{
			name: "ProvisionService.PlanProvision cross-org execution",
			exec: func() error {
				req := connect.NewRequest(&v1.PlanProvisionRequest{Id: provBetaID})
				req.Header().Set("Authorization", "Bearer token-user-a")
				_, err := provisionClient.PlanProvision(ctx, req)
				return err
			},
		},
		{
			name: "ProvisionService.DestroyProvision cross-org destruction",
			exec: func() error {
				req := connect.NewRequest(&v1.DestroyProvisionRequest{
					Id:        provBetaID,
					ConfirmId: provBetaID,
				})
				req.Header().Set("Authorization", "Bearer token-user-a")
				_, err := provisionClient.DestroyProvision(ctx, req)
				return err
			},
		},
		{
			name: "ApiKeyService.RevokeApiKey cross-org revocation",
			exec: func() error {
				req := connect.NewRequest(&v1.RevokeApiKeyRequest{Id: apiKeyBetaID})
				req.Header().Set("Authorization", "Bearer token-user-a")
				_, err := apiKeyClient.RevokeApiKey(ctx, req)
				return err
			},
		},
		{
			name: "AuditService.GetAuditEvent cross-org lookup",
			exec: func() error {
				req := connect.NewRequest(&v1.GetAuditEventRequest{Id: "some-arbitrary-event-id"})
				req.Header().Set("Authorization", "Bearer token-user-a")
				_, err := auditClient.GetAuditEvent(ctx, req)
				return err
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.exec()
			require.Error(t, err, "cross-org access must fail for %s", tc.name)
			code := connect.CodeOf(err)
			require.True(t,
				code == connect.CodeNotFound || code == connect.CodePermissionDenied,
				"expected CodeNotFound or CodePermissionDenied for %s, got: %v", tc.name, code,
			)
		})
	}

	// 6. Assert Org Alpha lists are clean (0 nodes, 0 workloads)
	listNodesReq := connect.NewRequest(&v1.ListNodesRequest{})
	listNodesReq.Header().Set("Authorization", "Bearer token-user-a")
	alphaNodes, err := nodeClient.ListNodes(ctx, listNodesReq)
	require.NoError(t, err)
	require.Empty(t, alphaNodes.Msg.Nodes, "Org Alpha must see 0 nodes")

	listWlReq := connect.NewRequest(&v1.ListWorkloadsRequest{})
	listWlReq.Header().Set("Authorization", "Bearer token-user-a")
	alphaWls, err := workloadClient.ListWorkloads(ctx, listWlReq)
	require.NoError(t, err)
	require.Empty(t, alphaWls.Msg.Workloads, "Org Alpha must see 0 workloads")
}
