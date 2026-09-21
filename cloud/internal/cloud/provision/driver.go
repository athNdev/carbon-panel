package provision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
)

// Driver runs the Terraform lifecycle inside one provision workspace.
type Driver interface {
	Init(ctx context.Context, p *db.Provision) error
	Plan(ctx context.Context, p *db.Provision, req PlanRequest) (summary, diff, hash string, err error)
	Apply(ctx context.Context, p *db.Provision, planHash string) error
	Destroy(ctx context.Context, p *db.Provision) error
	Outputs(ctx context.Context, p *db.Provision) (map[string]string, error)
}

// DriverOptions configures TerraformDriver. It is the driver subset of
// Options: NewService builds it from config.Provisioner plus overrides.
type DriverOptions struct {
	Root            string // == config.Provisioner.WorkDir
	TerraformPath   string // == config.Provisioner.TerraformPath
	StateBackend    string // local|s3
	StateLocalDir   string
	StateS3Bucket   string
	StateS3Region   string
	StateS3Endpoint string
	Runner          Runner
	Logs            *LogRegistry
}

// TerraformDriver is the os/exec-backed Driver. All terraform output is tee'd
// into the provision's log buffer when Logs is set.
type TerraformDriver struct {
	opts DriverOptions
}

// NewDriver returns a Driver over opts. Runner must be set (ExecRunner in
// production, FakeRunner in tests).
func NewDriver(opts DriverOptions) (*TerraformDriver, error) {
	if strings.TrimSpace(opts.Root) == "" {
		return nil, fmt.Errorf("provision: driver root is required")
	}
	if opts.Runner == nil {
		return nil, fmt.Errorf("provision: runner is required")
	}
	return &TerraformDriver{opts: opts}, nil
}

func (d *TerraformDriver) dirFor(p *db.Provision) (string, error) {
	return WorkspaceDir(d.opts.Root, p.Workspace)
}

func (d *TerraformDriver) log(p *db.Provision, line string) {
	if d.opts.Logs != nil {
		d.opts.Logs.Append(p.ID, line)
	}
}

func (d *TerraformDriver) run(ctx context.Context, p *db.Provision, args ...string) (string, error) {
	dir, err := d.dirFor(p)
	if err != nil {
		return "", err
	}
	d.log(p, "$ tofu "+strings.Join(args, " "))
	out, err := d.opts.Runner.Run(ctx, dir, args...)
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			d.log(p, line)
		}
	}
	return out, err
}

// writeWorkspace materializes backend.tf, main.tf (module call into the
// provider's node module) and terraform.tfvars. The module source path is
// relative so workspaces stay relocatable within a checkout.
func (d *TerraformDriver) writeWorkspace(p *db.Provision, req PlanRequest, userData, tfvars string) error {
	dir, err := d.dirFor(p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	backend := "local"
	if d.opts.StateBackend == "s3" {
		backend = "s3"
	}
	backendTF := "terraform {\n  backend " + hclQuote(backend) + " {}\n}\n"
	mainTF := "module \"node\" {\n" +
		"  source = " + hclQuote("../../../../terraform/modules/node/"+req.Provider) + "\n" +
		"}\n"
	for _, f := range []struct{ name, body string }{
		{"backend.tf", backendTF},
		{"main.tf", mainTF},
		{"terraform.tfvars", tfvars},
		{"bootstrap.txt", userData},
	} {
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.body), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// Init runs terraform init with the configured state backend config.
func (d *TerraformDriver) Init(ctx context.Context, p *db.Provision) error {
	args := append([]string{"init", "-input=false"},
		BackendArgs(d.opts.StateBackend, d.opts.StateLocalDir, p.Workspace,
			d.opts.StateS3Bucket, d.opts.StateS3Region, d.opts.StateS3Endpoint)...)
	_, err := d.run(ctx, p, args...)
	return err
}

// PlanHash is sha256(tfvars + "\x00" + userData).
func PlanHash(tfvars, userData string) string {
	sum := sha256.Sum256([]byte(tfvars + "\x00" + userData))
	return hex.EncodeToString(sum[:])
}

// Plan renders the bootstrap + tfvars, writes the workspace, inits and plans.
// It returns a short summary, a diffable plan excerpt and the plan hash the
// caller must present back to Apply.
func (d *TerraformDriver) Plan(ctx context.Context, p *db.Provision, req PlanRequest) (string, string, string, error) {
	userData, _, err := RenderUserData(BootstrapRequest{
		ControlPlaneURL: req.ControlPlaneURL,
		JoinToken:       req.JoinToken,
		NodeName:        req.NodeName,
		NodeTypeID:      req.NodeTypeID,
		AgentVersion:    req.AgentVersion,
		AgentInstallURL: req.AgentInstallURL,
		AgentSHA256:     req.AgentSHA256,
		ExtraRuncmd:     req.ExtraRuncmd,
		Provider:        req.Provider,
	})
	if err != nil {
		return "", "", "", err
	}
	tfvars, err := RenderTFVars(req, userData)
	if err != nil {
		return "", "", "", err
	}
	if err := d.writeWorkspace(p, req, userData, tfvars); err != nil {
		return "", "", "", err
	}
	if err := d.Init(ctx, p); err != nil {
		return "", "", "", err
	}
	out, err := d.run(ctx, p, "plan", "-input=false", "-detailed-exitcode",
		"-var-file=terraform.tfvars", "-out=tfplan")
	if err != nil {
		// -detailed-exitcode returns exit 2 when a diff exists; the fake
		// runner returns nil. Treat non-empty output as the plan either way.
		if strings.TrimSpace(out) == "" {
			return "", "", "", err
		}
	}
	hash := PlanHash(tfvars, userData)
	summary := "provider=" + req.Provider + " region=" + req.Region + " type=" + req.NodeTypeID
	diff := "--- plan " + hash[:12] + "\n" + strings.TrimSpace(out) + "\n"
	d.log(p, "plan hash "+hash)
	return summary, diff, hash, nil
}

// checkApplyGate enforces: no plan -> ErrPlanRequired; hash drift ->
// ErrPlanChanged. Called before any terraform invocation so a gated apply
// never partially mutates infrastructure.
func checkApplyGate(p *db.Provision, planHash string) error {
	if strings.TrimSpace(p.PlanHash) == "" || strings.TrimSpace(planHash) == "" {
		return ErrPlanRequired
	}
	if p.PlanHash != planHash {
		return fmt.Errorf("%w: stored %q != requested %q", ErrPlanChanged, p.PlanHash, planHash)
	}
	return nil
}

// Apply gates on the plan hash, then runs terraform apply of the planned file.
func (d *TerraformDriver) Apply(ctx context.Context, p *db.Provision, planHash string) error {
	if err := checkApplyGate(p, planHash); err != nil {
		return err
	}
	_, err := d.run(ctx, p, "apply", "-input=false", "-auto-approve", "tfplan")
	return err
}

// Destroy runs terraform destroy. It is not plan-gated: destroying a failed
// or half-planned workspace must always be possible.
func (d *TerraformDriver) Destroy(ctx context.Context, p *db.Provision) error {
	_, err := d.run(ctx, p, "destroy", "-input=false", "-auto-approve", "-var-file=terraform.tfvars")
	return err
}

// Outputs parses `terraform output -json` into a string map. Values may be
// strings or {"value":...} objects; both flatten. This matches the six
// outputs every node module declares: node_id, public_ip, private_ip,
// hostname, instance_id, provider.
func (d *TerraformDriver) Outputs(ctx context.Context, p *db.Provision) (map[string]string, error) {
	out, err := d.run(ctx, p, "output", "-json")
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return map[string]string{}, nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return nil, fmt.Errorf("provision: parse outputs: %w", err)
	}
	flat := make(map[string]string, len(raw))
	for k, v := range raw {
		switch t := v.(type) {
		case map[string]any:
			if val, ok := t["value"]; ok {
				flat[k] = fmt.Sprintf("%v", val)
				continue
			}
			flat[k] = fmt.Sprintf("%v", v)
		default:
			flat[k] = fmt.Sprintf("%v", v)
		}
	}
	return flat, nil
}
