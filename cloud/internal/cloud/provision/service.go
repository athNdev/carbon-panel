package provision

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/config"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/secrets"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Provision statuses persisted on db.Provision.Status.
const (
	StatusPending    = "pending"
	StatusPlanned    = "planned"
	StatusApplying   = "applying"
	StatusApplied    = "applied"
	StatusDestroying = "destroying"
	StatusDestroyed  = "destroyed"
	StatusFailed     = "failed"
)

// Options configures Service. Root/StateBackend/TerraformPath default from
// config.Provisioner; Secrets gates provider availability; Store persists
// db.Provision rows; Runner executes Terraform (ExecRunner in production,
// FakeRunner in tests).
type Options struct {
	Root            string
	TerraformPath   string
	StateBackend    string
	StateLocalDir   string
	StateS3Bucket   string
	StateS3Region   string
	StateS3Endpoint string
	ControlPlaneURL string
	AgentVersion    string
	AgentInstallURL string
	AgentSHA256     string
	Store           *db.Store
	Secrets         secrets.Provider
	Runner          Runner
	LogBufferSize   int
	PlanTimeout     time.Duration
	ApplyTimeout    time.Duration
}

// OptionsFromConfig builds Options from config.Provisioner. The secrets
// provider, store, runner and control-plane URL are caller-supplied: config
// carries no credentials.
func OptionsFromConfig(cfg config.Provisioner, store *db.Store, sec secrets.Provider, runner Runner) Options {
	return Options{
		Root:          cfg.WorkDir,
		TerraformPath: cfg.TerraformPath,
		StateBackend:  cfg.StateBackend,
		StateLocalDir: cfg.StateLocalDir,
		Store:         store,
		Secrets:       sec,
		Runner:        runner,
		PlanTimeout:   cfg.PlanTimeout,
		ApplyTimeout:  cfg.ApplyTimeout,
	}
}

// Service orchestrates CreateProvision -> plan -> apply -> node row ->
// outputs over db.Provision rows.
type Service struct {
	opts     Options
	resolver *secrets.Resolver
	driver   *TerraformDriver
	logs     *LogRegistry
}

// NewService validates Options and returns a Service.
func NewService(opts Options) (*Service, error) {
	if opts.Store == nil {
		return nil, fmt.Errorf("provision: store is required")
	}
	if opts.Runner == nil {
		return nil, fmt.Errorf("provision: runner is required")
	}
	if strings.TrimSpace(opts.Root) == "" {
		return nil, fmt.Errorf("provision: root work dir is required")
	}
	switch opts.StateBackend {
	case "", "local", "s3":
	default:
		return nil, fmt.Errorf("provision: state backend must be one of local|s3, got %q", opts.StateBackend)
	}
	binary := opts.TerraformPath
	if binary == "" {
		binary = "tofu"
	}
	logs := NewLogRegistry(opts.LogBufferSize)
	driver, err := NewDriver(DriverOptions{
		Root:            opts.Root,
		TerraformPath:   binary,
		StateBackend:    opts.StateBackend,
		StateLocalDir:   opts.StateLocalDir,
		StateS3Bucket:   opts.StateS3Bucket,
		StateS3Region:   opts.StateS3Region,
		StateS3Endpoint: opts.StateS3Endpoint,
		Runner:          opts.Runner,
		Logs:            logs,
	})
	if err != nil {
		return nil, err
	}
	var resolver *secrets.Resolver
	if opts.Secrets != nil {
		resolver = secrets.NewResolver(opts.Secrets)
	}
	opts.TerraformPath = binary
	return &Service{opts: opts, resolver: resolver, driver: driver, logs: logs}, nil
}

// Driver exposes the underlying workspace driver (for wave-3 escape hatches).
func (s *Service) Driver() Driver { return s.driver }

// CreateRequest creates one provision. NodeType travels as plain data
// (VCPU/RAMMB/DiskGB + NodeTypeID); the nodetype lane is not a dependency.
type CreateRequest struct {
	Name            string
	Provider        string
	Region          string
	NodeTypeID      string
	VCPU            int
	RAMMB           int
	DiskGB          int
	InstanceType    string
	SSHPublicKey    string
	Image           string
	Tags            map[string]string
	ProviderExtra   map[string]string
	Host            string
	SSHUser         string
	SSHPort         int
	NodeName        string
	JoinToken       string
	CreatedBy       string
	ExtraRuncmd     []string
	AgentInstallURL string
	AgentSHA256     string
	AgentVersion    string
}

func (s *Service) orgDB(ctx context.Context) (*gorm.DB, error) {
	q, err := s.opts.Store.Org(ctx)
	if err != nil {
		return nil, ErrNoOrg
	}
	return q, nil
}

func (s *Service) planRequestFor(orgID, provisionID, nodeID string, c CreateRequest) PlanRequest {
	agentURL := c.AgentInstallURL
	if agentURL == "" {
		agentURL = s.opts.AgentInstallURL
	}
	agentVer := c.AgentVersion
	if agentVer == "" {
		agentVer = s.opts.AgentVersion
	}
	agentSum := c.AgentSHA256
	if agentSum == "" {
		agentSum = s.opts.AgentSHA256
	}
	nodeName := c.NodeName
	if nodeName == "" {
		nodeName = c.Name
	}
	return PlanRequest{
		OrgID: orgID, ProvisionID: provisionID, NodeID: nodeID, NodeName: nodeName,
		Provider: c.Provider, Region: c.Region, NodeTypeID: c.NodeTypeID,
		VCPU: c.VCPU, RAMMB: c.RAMMB, DiskGB: c.DiskGB,
		InstanceType: c.InstanceType, SSHPublicKey: c.SSHPublicKey,
		ControlPlaneURL: s.opts.ControlPlaneURL, JoinToken: c.JoinToken,
		Image: c.Image, Tags: c.Tags, ProviderExtra: c.ProviderExtra,
		Host: c.Host, SSHUser: c.SSHUser, SSHPort: c.SSHPort,
		AgentVersion: agentVer, AgentInstallURL: agentURL, AgentSHA256: agentSum,
		ExtraRuncmd: c.ExtraRuncmd,
	}
}

func newWorkspace() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("p%x", b)
}

// CreateProvision validates (deny by default) and persists a pending row.
func (s *Service) CreateProvision(ctx context.Context, c CreateRequest) (*db.Provision, error) {
	q, err := s.orgDB(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(c.Name) == "" {
		return nil, fmt.Errorf("provision: name is required")
	}
	pre := s.planRequestFor("pre", "pre", "pre", c)
	if err := pre.Validate(ctx, s.resolver); err != nil {
		return nil, err
	}
	orgID := principal.OrgID(ctx)
	if orgID == "" {
		return nil, ErrNoOrg
	}
	p := &db.Provision{
		TenantBase: db.TenantBase{ID: uuid.NewString(), OrgID: orgID},
		Name:       c.Name,
		Provider:   c.Provider,
		Region:     c.Region,
		NodeTypeID: c.NodeTypeID,
		Status:     StatusPending,
		Workspace:  newWorkspace(),
		CreatedBy:  c.CreatedBy,
	}
	if err := ValidateWorkspace(p.Workspace); err != nil {
		return nil, err
	}
	if err := q.Create(p).Error; err != nil {
		return nil, err
	}
	return p, nil
}

// Get returns one provision in the caller's org.
func (s *Service) Get(ctx context.Context, id string) (*db.Provision, error) {
	return s.load(ctx, id)
}

// load fetches one row. Every caller must fetch a FRESH org handle for each
// subsequent statement: reusing a GORM handle across First+Save accumulates
// clauses and breaks (ambiguous org_id on SQLite).
func (s *Service) load(ctx context.Context, id string) (*db.Provision, error) {
	q, err := s.orgDB(ctx)
	if err != nil {
		return nil, err
	}
	var p db.Provision
	if err := q.Where("id = ?", id).First(&p).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

func (s *Service) save(ctx context.Context, p *db.Provision) error {
	q, err := s.orgDB(ctx)
	if err != nil {
		return err
	}
	return q.Save(p).Error
}

func (s *Service) fail(ctx context.Context, p *db.Provision, msg string) {
	p.Status = StatusFailed
	p.Error = msg
	_ = s.save(ctx, p)
}

// Plan runs Init+Plan for a pending/failed/planned row and stores the plan.
func (s *Service) Plan(ctx context.Context, id string, c CreateRequest) (*db.Provision, error) {
	p, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	req := s.planRequestFor(p.OrgID, p.ID, p.NodeID, c)
	// Provider/region/type may be corrected at plan time; keep the row's
	// values when the request leaves them blank.
	if req.Provider == "" {
		req.Provider = p.Provider
	}
	if req.Region == "" {
		req.Region = p.Region
	}
	if req.NodeTypeID == "" {
		req.NodeTypeID = p.NodeTypeID
	}
	if err := req.Validate(ctx, s.resolver); err != nil {
		s.fail(ctx, p, err.Error())
		return nil, err
	}
	if tctx, cancel := s.planCtx(ctx); cancel != nil {
		defer cancel()
		ctx = tctx
	}
	if p.NodeID == "" {
		p.NodeID = "node-" + strings.ReplaceAll(p.ID[:8], "-", "") + "-" + shortID()
	}
	summary, diff, hash, perr := s.driver.Plan(ctx, p, req)
	if perr != nil {
		s.fail(ctx, p, perr.Error())
		return nil, perr
	}
	p.Provider = req.Provider
	p.Region = req.Region
	p.NodeTypeID = req.NodeTypeID
	p.PlanSummary = summary
	p.PlanDiff = diff
	p.PlanHash = hash
	p.Status = StatusPlanned
	p.Error = ""
	if err := s.save(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Apply gates on the stored plan hash, applies, records outputs and links a
// db.Node row. Never partially applies: the gate runs before Terraform.
func (s *Service) Apply(ctx context.Context, id string) (*db.Provision, error) {
	p, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := checkApplyGate(p, p.PlanHash); err != nil {
		return nil, err
	}
	p.Status = StatusApplying
	p.Error = ""
	if err := s.save(ctx, p); err != nil {
		return nil, err
	}
	if tctx, cancel := s.applyCtx(ctx); cancel != nil {
		defer cancel()
		ctx = tctx
	}
	if err := s.driver.Apply(ctx, p, p.PlanHash); err != nil {
		s.fail(ctx, p, err.Error())
		return nil, err
	}
	outputs, err := s.driver.Outputs(ctx, p)
	if err != nil {
		s.fail(ctx, p, err.Error())
		return nil, err
	}
	if err := s.linkNode(ctx, p, outputs); err != nil {
		s.fail(ctx, p, err.Error())
		return nil, err
	}
	p.SetOutputs(outputs)
	p.Status = StatusApplied
	p.Error = ""
	if err := s.save(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Destroy tears down and marks the row destroyed. Not plan-gated.
func (s *Service) Destroy(ctx context.Context, id string) (*db.Provision, error) {
	p, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	p.Status = StatusDestroying
	if err := s.save(ctx, p); err != nil {
		return nil, err
	}
	if derr := s.driver.Destroy(ctx, p); derr != nil {
		s.fail(ctx, p, derr.Error())
		return nil, derr
	}
	p.Status = StatusDestroyed
	p.Error = ""
	if err := s.save(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Outputs refreshes stored outputs from Terraform.
func (s *Service) Outputs(ctx context.Context, id string) (map[string]string, error) {
	p, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	out, err := s.driver.Outputs(ctx, p)
	if err != nil {
		return nil, err
	}
	p.SetOutputs(out)
	if err := s.save(ctx, p); err != nil {
		return nil, err
	}
	return out, nil
}

// StreamLogs replays retained lines and follows live output for id.
func (s *Service) StreamLogs(id string) (replay []string, ch <-chan string, cancel func()) {
	return s.logs.For(id).Subscribe()
}

func (s *Service) linkNode(ctx context.Context, p *db.Provision, outputs map[string]string) error {
	q, err := s.orgDB(ctx)
	if err != nil {
		return err
	}
	var node db.Node
	err = q.Where("id = ?", p.NodeID).First(&node).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	if err == gorm.ErrRecordNotFound {
		node = db.Node{
			TenantBase:  db.TenantBase{ID: p.NodeID, OrgID: p.OrgID},
			Name:        p.Name,
			Origin:      "managed",
			Provider:    p.Provider,
			NodeTypeID:  p.NodeTypeID,
			Region:      p.Region,
			Status:      "active",
			ProvisionID: p.ID,
		}
	} else {
		node.Provider = p.Provider
		node.Region = p.Region
		node.NodeTypeID = p.NodeTypeID
		node.Status = "active"
		node.ProvisionID = p.ID
	}
	if v, ok := outputs["public_ip"]; ok {
		node.PublicIP = v
	}
	if v, ok := outputs["private_ip"]; ok {
		node.PrivateIP = v
	}
	if v, ok := outputs["hostname"]; ok {
		node.Hostname = v
	}
	if err == gorm.ErrRecordNotFound {
		qc, cerr := s.orgDB(ctx)
		if cerr != nil {
			return cerr
		}
		return qc.Create(&node).Error
	}
	qs, serr := s.orgDB(ctx)
	if serr != nil {
		return serr
	}
	return qs.Save(&node).Error
}

func (s *Service) planCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if s.opts.PlanTimeout > 0 {
		return context.WithTimeout(ctx, s.opts.PlanTimeout)
	}
	return ctx, nil
}

func (s *Service) applyCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if s.opts.ApplyTimeout > 0 {
		return context.WithTimeout(ctx, s.opts.ApplyTimeout)
	}
	return ctx, nil
}

func shortID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b)
}
