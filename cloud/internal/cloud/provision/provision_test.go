package provision

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/keys"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/secrets"
	"github.com/stretchr/testify/require"
)

// mapSecrets is a fake secrets.Provider backed by a map.
type mapSecrets map[string]string

func (m mapSecrets) Kind() string { return "test" }
func (m mapSecrets) Get(_ context.Context, key string) (string, error) {
	if v, ok := m[key]; ok && v != "" {
		return v, nil
	}
	return "", secrets.ErrNotFound
}
func (m mapSecrets) Health(_ context.Context) error { return nil }

func hetznerSecrets() mapSecrets {
	return mapSecrets{keys.ProviderHetznerToken: "fake-hetzner-token"}
}

func testCtx() context.Context {
	return principal.WithPrincipal(context.Background(), principal.Principal{
		Kind:   principal.KindSession,
		UserID: "user-1",
		OrgID:  "org-test",
		Role:   "owner",
	})
}

func openStore(t *testing.T) *db.Store {
	t.Helper()
	s, err := db.Open(db.Options{Driver: "sqlite", DSN: ":memory:", AutoMigrate: true})
	require.NoError(t, err)
	return s
}

func newService(t *testing.T, store *db.Store, sec secrets.Provider) (*Service, *FakeRunner) {
	t.Helper()
	fake := &FakeRunner{}
	svc, err := NewService(Options{
		Root:            t.TempDir(),
		TerraformPath:   "tofu",
		StateBackend:    "local",
		StateLocalDir:   t.TempDir(),
		ControlPlaneURL: "https://cloud.example.com",
		AgentVersion:    "v0.0-test",
		AgentInstallURL: "https://example.com/cloudnoded",
		Store:           store,
		Secrets:         sec,
		Runner:          fake,
		LogBufferSize:   50,
		PlanTimeout:     time.Minute,
		ApplyTimeout:    time.Minute,
	})
	require.NoError(t, err)
	return svc, fake
}

func hetznerReq() CreateRequest {
	return CreateRequest{
		Name: "node-1", Provider: "hetzner", Region: "fsn1", NodeTypeID: "small",
		VCPU: 2, RAMMB: 4096, DiskGB: 40, InstanceType: "cx32",
		SSHPublicKey: "ssh-ed25519 FAKE", JoinToken: "fake-join-token",
		CreatedBy: "user-1",
	}
}

func TestLifecyclePlanApplyDestroy(t *testing.T) {
	t.Parallel()
	ctx := testCtx()
	svc, fake := newService(t, openStore(t), hetznerSecrets())

	p, err := svc.CreateProvision(ctx, hetznerReq())
	require.NoError(t, err)
	require.Equal(t, StatusPending, p.Status)
	require.NoError(t, ValidateWorkspace(p.Workspace))

	p, err = svc.Plan(ctx, p.ID, hetznerReq())
	require.NoError(t, err)
	require.Equal(t, StatusPlanned, p.Status)
	require.NotEmpty(t, p.PlanHash)
	require.NotEmpty(t, p.PlanSummary)
	require.Contains(t, p.PlanDiff, "Plan:")

	names := fake.CommandNames()
	require.Contains(t, names, "init")
	require.Contains(t, names, "plan")

	p, err = svc.Apply(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, StatusApplied, p.Status)
	require.Equal(t, "203.0.113.10", p.OutputMap()["public_ip"])

	// Node row linked.
	q, err := svc.opts.Store.Org(ctx)
	require.NoError(t, err)
	var node db.Node
	require.NoError(t, q.Where("id = ?", p.NodeID).First(&node).Error)
	require.Equal(t, p.ID, node.ProvisionID)
	require.Equal(t, "managed", node.Origin)
	require.Equal(t, "203.0.113.10", node.PublicIP)

	p, err = svc.Destroy(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, StatusDestroyed, p.Status)
	require.Contains(t, fake.CommandNames(), "destroy")
}

func TestApplyWithoutPlanRejected(t *testing.T) {
	t.Parallel()
	ctx := testCtx()
	svc, _ := newService(t, openStore(t), hetznerSecrets())

	p, err := svc.CreateProvision(ctx, hetznerReq())
	require.NoError(t, err)
	_, err = svc.Apply(ctx, p.ID)
	require.ErrorIs(t, err, ErrPlanRequired)

	// Driver gate directly: empty stored hash.
	ghost := *p
	ghost.PlanHash = ""
	require.ErrorIs(t, svc.driver.Apply(ctx, &ghost, "abc"), ErrPlanRequired)
}

func TestApplyAfterPlanChangedRejected(t *testing.T) {
	t.Parallel()
	ctx := testCtx()
	svc, _ := newService(t, openStore(t), hetznerSecrets())

	p, err := svc.CreateProvision(ctx, hetznerReq())
	require.NoError(t, err)
	p, err = svc.Plan(ctx, p.ID, hetznerReq())
	require.NoError(t, err)

	// Re-plan with a different region changes the hash.
	changed := hetznerReq()
	changed.Region = "nbg1"
	p2, err := svc.Plan(ctx, p.ID, changed)
	require.NoError(t, err)
	require.NotEqual(t, p.PlanHash, p2.PlanHash)

	// Applying with the stale hash is refused before Terraform runs.
	require.ErrorIs(t, svc.driver.Apply(ctx, p2, p.PlanHash), ErrPlanChanged)

	// Unknown provider / region / node type are deny-by-default.
	bad := hetznerReq()
	bad.Provider = "nope"
	_, err = svc.CreateProvision(ctx, bad)
	require.ErrorIs(t, err, ErrUnknownProvider)
	bad = hetznerReq()
	bad.Region = "nope-1"
	_, err = svc.CreateProvision(ctx, bad)
	require.ErrorIs(t, err, ErrUnknownRegion)
	bad = hetznerReq()
	bad.NodeTypeID = "nope"
	_, err = svc.CreateProvision(ctx, bad)
	require.ErrorIs(t, err, ErrUnknownNodeType)
}

func TestMissingCredentialsDenied(t *testing.T) {
	t.Parallel()
	ctx := testCtx()
	svc, _ := newService(t, openStore(t), mapSecrets{})
	_, err := svc.CreateProvision(ctx, hetznerReq())
	require.ErrorIs(t, err, ErrMissingCredentials)
	var mce *MissingCredentialsError
	require.True(t, errors.As(err, &mce))
	require.Contains(t, mce.Missing, keys.ProviderHetznerToken)
}

func TestNoOrgRefused(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, openStore(t), hetznerSecrets())
	_, err := svc.CreateProvision(context.Background(), hetznerReq())
	require.ErrorIs(t, err, ErrNoOrg)
}

func TestWorkspaceTraversalRejected(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"", "..", "../x", "/abs", "a/b", "a\\b", ".", "UPPER", "with space", "a.b", "x..y"} {
		require.ErrorIs(t, ValidateWorkspace(bad), ErrInvalidWorkspace, bad)
		_, err := WorkspaceDir(t.TempDir(), bad)
		require.ErrorIs(t, err, ErrInvalidWorkspace, bad)
	}
	require.NoError(t, ValidateWorkspace("p1a2b3c4"))
	dir, err := WorkspaceDir(t.TempDir(), "p1a2b3c4")
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(dir, "p1a2b3c4"))
}

func TestBackendArgsLocalAndS3(t *testing.T) {
	t.Parallel()
	local := BackendArgs("local", "/data/tfstate", "pabc", "", "", "")
	require.Equal(t, []string{"-backend-config=path=/data/tfstate/pabc.tfstate"}, local)

	s3 := BackendArgs("s3", "", "pabc", "state-bucket", "", "")
	require.Equal(t, []string{
		"-backend-config=bucket=state-bucket",
		"-backend-config=key=pabc/terraform.tfstate",
		"-backend-config=region=auto",
	}, s3)

	s3full := BackendArgs("s3", "", "pabc", "b", "eu-west-1", "https://s3.example.com")
	require.Contains(t, s3full, "-backend-config=endpoint=https://s3.example.com")
	require.Contains(t, s3full, "-backend-config=region=eu-west-1")
}

var varRe = regexp.MustCompile(`variable\s+"([^"]+)"`)

// TestTFVarsCoversModuleVariables reads every node module's variables.tf and
// asserts the rendered tfvars assigns each declared variable (generic extras
// only for generic). This keeps the renderer honest against the wave-1 modules.
func TestTFVarsCoversModuleVariables(t *testing.T) {
	t.Parallel()
	modRoot := filepath.Join("..", "..", "..", "..", "cloud", "terraform", "modules", "node")
	for _, prov := range []string{"hetzner", "aws", "gcp", "digitalocean", "proxmox", "generic"} {
		raw, err := os.ReadFile(filepath.Join(modRoot, prov, "variables.tf"))
		require.NoError(t, err, prov)
		// Module file must exist and declare the shared contract.
		mainRaw, err := os.ReadFile(filepath.Join(modRoot, prov, "main.tf"))
		require.NoError(t, err, prov)
		require.NotEmpty(t, mainRaw, prov)

		vars := varRe.FindAllStringSubmatch(string(raw), -1)
		require.NotEmpty(t, vars, prov)

		req := PlanRequest{
			OrgID: "org-test", ProvisionID: "prov-1", NodeID: "node-1", NodeName: "node-1",
			Provider: prov, NodeTypeID: "small", VCPU: 2, RAMMB: 4096, DiskGB: 40,
			InstanceType: "custom", SSHPublicKey: "ssh-ed25519 FAKE",
			ControlPlaneURL: "https://cloud.example.com", JoinToken: "fake-join",
		}
		if prov == "proxmox" {
			req.Region = "onprem"
		}
		if prov == "generic" {
			req.Region = "onprem"
			req.Host = "192.0.2.10"
		}
		if prov == "aws" {
			req.Region = "us-east-1"
		}
		if prov == "gcp" {
			req.Region = "us-central1"
		}
		if prov == "digitalocean" {
			req.Region = "nyc1"
		}
		tfvars, err := RenderTFVars(req, "#cloud-config fake")
		require.NoError(t, err, prov)
		for _, m := range vars {
			name := m[1]
			require.Contains(t, tfvars, name+" = ", "provider %s missing var %s", prov, name)
		}
	}
}

func TestCloudInitBootstrap(t *testing.T) {
	t.Parallel()
	ud, ct, err := RenderUserData(BootstrapRequest{
		ControlPlaneURL: "https://cloud.example.com",
		JoinToken:       "fake-join",
		NodeName:        "node-1",
		NodeTypeID:      "small",
		AgentVersion:    "v1.2.3",
		AgentInstallURL: "https://example.com/cloudnoded",
		Provider:        "hetzner",
	})
	require.NoError(t, err)
	require.Equal(t, "text/cloud-config", ct)
	require.Contains(t, ud, "https://cloud.example.com")
	require.Contains(t, ud, "fake-join")
	require.Contains(t, ud, "v1.2.3")

	sh, ct, err := RenderUserData(BootstrapRequest{
		ControlPlaneURL: "https://cloud.example.com",
		JoinToken:       "fake-join",
		NodeName:        "node-1",
		NodeTypeID:      "small",
		AgentInstallURL: "https://example.com/cloudnoded",
		Provider:        "generic",
	})
	require.NoError(t, err)
	require.Equal(t, "text/x-shellscript", ct)
	require.True(t, strings.HasPrefix(sh, "#!/bin/sh"))

	_, _, err = RenderUserData(BootstrapRequest{ControlPlaneURL: "https://x", Format: "yaml"})
	require.Error(t, err)
	_, _, err = RenderUserData(BootstrapRequest{ControlPlaneURL: "https://x", JoinToken: "", Provider: "hetzner"})
	require.Error(t, err)
	_, _, err = RenderUserData(BootstrapRequest{JoinToken: "j", Provider: "hetzner"})
	require.Error(t, err)
}

func TestLogBufferReplayAndFanout(t *testing.T) {
	t.Parallel()
	b := NewLogBuffer(3)
	b.Append("a")
	b.Append("b")
	b.Append("c")
	b.Append("d") // evicts "a" at cap 3
	replay, ch, cancel := b.Subscribe()
	require.Equal(t, []string{"b", "c", "d"}, replay)
	b.Append("e")
	select {
	case got := <-ch:
		require.Equal(t, "e", got)
	case <-time.After(2 * time.Second):
		t.Fatal("live fan-out did not deliver")
	}
	cancel()
	_, open := <-ch
	require.False(t, open)
}

func TestStreamLogsReplayWhileRunning(t *testing.T) {
	t.Parallel()
	ctx := testCtx()
	svc, _ := newService(t, openStore(t), hetznerSecrets())
	p, err := svc.CreateProvision(ctx, hetznerReq())
	require.NoError(t, err)
	p, err = svc.Plan(ctx, p.ID, hetznerReq())
	require.NoError(t, err)
	replay, _, cancel := svc.StreamLogs(p.ID)
	defer cancel()
	require.NotEmpty(t, replay)
	joined := strings.Join(replay, "\n")
	require.Contains(t, joined, "plan")
}

func TestOutputsParseFailure(t *testing.T) {
	t.Parallel()
	ctx := testCtx()
	svc, fake := newService(t, openStore(t), hetznerSecrets())
	fake.Handle = func(_ context.Context, _ string, args []string) (string, error) {
		if len(args) > 0 && args[0] == "output" {
			return "not-json{{{", nil
		}
		return "ok", nil
	}
	p, err := svc.CreateProvision(ctx, hetznerReq())
	require.NoError(t, err)
	_, err = svc.driver.Outputs(ctx, p)
	require.Error(t, err)
}

func TestPlanHashStableAndSensitive(t *testing.T) {
	t.Parallel()
	a := PlanHash("tfvars-a", "user-a")
	require.Equal(t, a, PlanHash("tfvars-a", "user-a"))
	require.NotEqual(t, a, PlanHash("tfvars-b", "user-a"))
	require.Len(t, a, 64)
}
