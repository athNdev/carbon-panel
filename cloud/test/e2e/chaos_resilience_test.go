package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/audit"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/auth"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/billing"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/httpapi"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/keys"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/node"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/nodetype"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/provision"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/rbac"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/svc"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/athNdev/carbon-panel/pkg/proto/cloud/v1/cloudv1connect"
	"github.com/stretchr/testify/require"
)

func TestChaosAndConcurrencyResilience(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store, err := db.Open(db.Options{
		Driver:       "sqlite",
		DSN:          "file:chaos_resilience?mode=memory&cache=shared&_busy_timeout=5000",
		MaxOpenConns: 1,
		AutoMigrate:  true,
	})
	require.NoError(t, err)

	require.NoError(t, store.SeedDefaults(ctx))
	catalog := nodetype.NewCatalog(store)
	require.NoError(t, catalog.EnsureSeeded(ctx))

	pepper := []byte("chaos-resilience-pepper-32bytes!")
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
		LogBufferSize:   100,
	})
	require.NoError(t, err)

	verifier := &fakeClerkVerifier{
		tokens: map[string]auth.Claims{
			"tok_chaos": {
				Subject: "user_chaos_runner",
				Email:   "chaos@carbon.dev",
			},
		},
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
		Verifier: verifier,
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
	defer srv.Close()

	orgClient := cloudv1connect.NewOrgServiceClient(srv.Client(), srv.URL)
	nodeClient := cloudv1connect.NewNodeServiceClient(srv.Client(), srv.URL)
	agentClient := cloudv1connect.NewAgentServiceClient(srv.Client(), srv.URL)

	// Create test org
	createOrgReq := connect.NewRequest(&v1.CreateOrgRequest{
		Name: "Chaos Engineering Org",
		Slug: "chaos-org",
	})
	createOrgReq.Header().Set("Authorization", "Bearer tok_chaos")
	orgResp, err := orgClient.CreateOrg(ctx, createOrgReq)
	require.NoError(t, err)
	orgID := orgResp.Msg.Org.Id

	verifier.tokens["tok_chaos"] = auth.Claims{
		Subject: "user_chaos_runner",
		Email:   "chaos@carbon.dev",
		OrgID:   orgID,
		OrgRole: "org:admin",
	}

	t.Run("Concurrent Join Token Creation", func(t *testing.T) {
		const numGoroutines = 20
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		createdSecrets := make([]string, numGoroutines)
		tokenIDs := make([]string, numGoroutines)
		errorsCh := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(idx int) {
				defer wg.Done()
				req := connect.NewRequest(&v1.CreateJoinTokenRequest{
					Name:       fmt.Sprintf("concurrent-token-%d", idx),
					NodeTypeId: "small",
				})
				req.Header().Set("Authorization", "Bearer tok_chaos")

				resp, err := nodeClient.CreateJoinToken(ctx, req)
				if err != nil {
					errorsCh <- err
					return
				}
				createdSecrets[idx] = resp.Msg.Secret
				tokenIDs[idx] = resp.Msg.Token.Id
			}(i)
		}

		wg.Wait()
		close(errorsCh)

		for err := range errorsCh {
			require.NoError(t, err)
		}

		// Ensure all created tokens are distinct
		uniqueSecrets := make(map[string]bool)
		for _, s := range createdSecrets {
			require.NotEmpty(t, s)
			require.False(t, uniqueSecrets[s], "duplicate token secret generated!")
			uniqueSecrets[s] = true
		}
	})

	t.Run("Race Condition on Single-Use Join Token Redemption", func(t *testing.T) {
		// Create single join token
		req := connect.NewRequest(&v1.CreateJoinTokenRequest{
			Name:       "single-use-race-token",
			NodeTypeId: "small",
		})
		req.Header().Set("Authorization", "Bearer tok_chaos")

		resp, err := nodeClient.CreateJoinToken(ctx, req)
		require.NoError(t, err)
		secret := resp.Msg.Secret

		// 10 concurrent bootstrap attempts using the EXACT same token secret
		const racers = 10
		var wg sync.WaitGroup
		wg.Add(racers)

		var successCount int64
		var failureCount int64

		for i := 0; i < racers; i++ {
			go func(idx int) {
				defer wg.Done()
				joinReq := connect.NewRequest(&v1.JoinNodeRequest{
					Token:        secret,
					Hostname:     fmt.Sprintf("racer-%d.internal", idx),
					AgentVersion: "0.2.0-test",
					Capacity: &v1.NodeCapacity{
						Vcpu:   4,
						RamMb:  8192,
						DiskGb: 100,
					},
				})
				_, joinErr := agentClient.JoinNode(ctx, joinReq)
				if joinErr == nil {
					atomic.AddInt64(&successCount, 1)
				} else {
					atomic.AddInt64(&failureCount, 1)
				}
			}(i)
		}

		wg.Wait()

		// In atomic token redemption, exactly ONE must succeed, and all other 9 must fail!
		require.Equal(t, int64(1), successCount, "expected exactly 1 node to successfully redeem the token")
		require.Equal(t, int64(racers-1), failureCount, "expected all other attempts to fail")
	})

	t.Run("High-Frequency Heartbeat Updates", func(t *testing.T) {
		// Register a node for heartbeat stress
		req := connect.NewRequest(&v1.CreateJoinTokenRequest{
			Name:       "heartbeat-test-token",
			NodeTypeId: "medium",
		})
		req.Header().Set("Authorization", "Bearer tok_chaos")

		tokenResp, err := nodeClient.CreateJoinToken(ctx, req)
		require.NoError(t, err)

		joinReq := connect.NewRequest(&v1.JoinNodeRequest{
			Token:        tokenResp.Msg.Secret,
			Hostname:     "hb-node.internal",
			AgentVersion: "0.2.0-test",
			Capacity: &v1.NodeCapacity{
				Vcpu:   8,
				RamMb:  16384,
				DiskGb: 200,
			},
		})
		joinResp, err := agentClient.JoinNode(ctx, joinReq)
		require.NoError(t, err)
		nodeID := joinResp.Msg.Identity.NodeId

		// Fire 30 rapid heartbeats in parallel
		const hbCount = 30
		var wg sync.WaitGroup
		wg.Add(hbCount)
		orgScopedCtx := principal.WithPrincipal(ctx, principal.Principal{
			Kind:   principal.KindSession,
			UserID: "user_chaos_runner",
			OrgID:  orgID,
			Role:   "owner",
		})

		for i := 0; i < hbCount; i++ {
			go func() {
				defer wg.Done()
				_, _ = nodesService.Heartbeat(orgScopedCtx, nodeID)
			}()
		}
		wg.Wait()

		// Verify node status remains ONLINE
		getReq := connect.NewRequest(&v1.GetNodeRequest{Id: nodeID})
		getReq.Header().Set("Authorization", "Bearer tok_chaos")
		nodeRes, err := nodeClient.GetNode(ctx, getReq)
		require.NoError(t, err)
		require.Equal(t, v1.NodeStatus_NODE_STATUS_ONLINE, nodeRes.Msg.Node.Status)
	})

	t.Run("Concurrent Quota Enforcement Gate", func(t *testing.T) {
		cat := billing.NewCatalog()
		enforcer := billing.NewQuotaEnforcer(cat)
		freePlan := cat.GetOrDefault("free") // Limit: 2 workloads

		var currentMu sync.Mutex
		current := billing.Usage{WorkloadCount: 0}

		const numAttempts = 10
		var successfulProvisions int64
		var rejectedProvisions int64

		var wg sync.WaitGroup
		wg.Add(numAttempts)

		for i := 0; i < numAttempts; i++ {
			go func() {
				defer wg.Done()
				currentMu.Lock()
				defer currentMu.Unlock()

				delta := billing.Usage{WorkloadCount: 1}
				err := enforcer.Check(freePlan, current, delta)
				if err == nil {
					current = current.Add(delta)
					atomic.AddInt64(&successfulProvisions, 1)
				} else {
					atomic.AddInt64(&rejectedProvisions, 1)
				}
			}()
		}

		wg.Wait()

		require.Equal(t, int64(2), successfulProvisions, "only 2 workloads should be permitted on free plan")
		require.Equal(t, int64(numAttempts-2), rejectedProvisions, "remaining attempts must be rejected by quota enforcer")
	})
}
