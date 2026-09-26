package provision

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/provider"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/secrets"
)

// KnownNodeTypes mirrors the node_type validation in every
// cloud/terraform/modules/node/*/variables.tf.
var KnownNodeTypes = []string{"nano", "small", "medium", "large", "xlarge", "custom"}

// IsKnownNodeType reports whether id is a valid node type.
func IsKnownNodeType(id string) bool {
	for _, k := range KnownNodeTypes {
		if k == id {
			return true
		}
	}
	return false
}

// PlanRequest is everything needed to render one node's .tfvars plus its
// bootstrap. The node type travels as plain data: the nodetype lane is not
// available to this lane.
type PlanRequest struct {
	OrgID           string
	ProvisionID     string
	NodeID          string
	NodeName        string
	Provider        string
	Region          string
	NodeTypeID      string
	VCPU            int
	RAMMB           int
	DiskGB          int
	InstanceType    string
	SSHPublicKey    string
	ControlPlaneURL string
	JoinToken       string
	Image           string
	Tags            map[string]string
	ProviderExtra   map[string]string

	// Generic-only (cloud/terraform/modules/node/generic extras).
	Host          string
	SSHUser       string
	SSHPort       int
	SSHPrivateKey string

	// Bootstrap (cloud/terraform/modules/cloudinit).
	AgentVersion    string
	AgentInstallURL string
	AgentSHA256     string
	ExtraRuncmd     []string
}

// Validate checks provider, region, node type and credentials. Deny by
// default: any unknown or missing input is a typed error, never a partial
// render.
func (r PlanRequest) Validate(ctx context.Context, resolver *secrets.Resolver) error {
	d, ok := provider.Get(r.Provider)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownProvider, r.Provider)
	}
	if !d.HasRegion(r.Region) {
		return fmt.Errorf("%w: %q for provider %q", ErrUnknownRegion, r.Region, r.Provider)
	}
	if !IsKnownNodeType(r.NodeTypeID) {
		return fmt.Errorf("%w: %q", ErrUnknownNodeType, r.NodeTypeID)
	}
	if strings.TrimSpace(r.OrgID) == "" || strings.TrimSpace(r.NodeName) == "" {
		return fmt.Errorf("%w: org and node name are required", ErrUnknownNodeType)
	}
	var missing []string
	if resolver == nil {
		missing = append([]string(nil), d.CredentialKeys...)
	} else {
		missing = d.MissingKeys(ctx, resolver)
	}
	if len(missing) > 0 {
		return &MissingCredentialsError{Provider: r.Provider, Missing: missing}
	}
	if r.Provider == "generic" && strings.TrimSpace(r.Host) == "" {
		return fmt.Errorf("provision: generic provider requires host")
	}
	return nil
}

func hclQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`)
	return `"` + r.Replace(s) + `"`
}

func hclStringMap(m map[string]string) string {
	if len(m) == 0 {
		return "{}"
	}
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	var b strings.Builder
	b.WriteString("{\n")
	for _, k := range ks {
		fmt.Fprintf(&b, "  %s = %s\n", hclQuote(k), hclQuote(m[k]))
	}
	b.WriteString("}")
	return b.String()
}

// RenderTFVars renders terraform.tfvars for the provider's node module. The
// variable names match cloud/terraform/modules/node/*/variables.tf exactly:
// org_id, node_id, node_name, node_type, vcpu, ram_mb, disk_gb, region,
// instance_type, ssh_public_key, cloud_init, control_plane_url, join_token,
// tags, image, provider_extra (+ host/ssh_user/ssh_port/ssh_private_key for
// generic). userData is the rendered bootstrap (cloud-init or shellscript).
func RenderTFVars(req PlanRequest, userData string) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "org_id = %s\n", hclQuote(req.OrgID))
	fmt.Fprintf(&b, "node_id = %s\n", hclQuote(req.NodeID))
	fmt.Fprintf(&b, "node_name = %s\n", hclQuote(req.NodeName))
	fmt.Fprintf(&b, "node_type = %s\n", hclQuote(req.NodeTypeID))
	fmt.Fprintf(&b, "vcpu = %d\n", req.VCPU)
	fmt.Fprintf(&b, "ram_mb = %d\n", req.RAMMB)
	fmt.Fprintf(&b, "disk_gb = %d\n", req.DiskGB)
	fmt.Fprintf(&b, "region = %s\n", hclQuote(req.Region))
	fmt.Fprintf(&b, "instance_type = %s\n", hclQuote(req.InstanceType))
	fmt.Fprintf(&b, "ssh_public_key = %s\n", hclQuote(req.SSHPublicKey))
	fmt.Fprintf(&b, "cloud_init = %s\n", hclQuote(userData))
	fmt.Fprintf(&b, "control_plane_url = %s\n", hclQuote(req.ControlPlaneURL))
	fmt.Fprintf(&b, "join_token = %s\n", hclQuote(req.JoinToken))
	fmt.Fprintf(&b, "tags = %s\n", hclStringMap(req.Tags))
	fmt.Fprintf(&b, "image = %s\n", hclQuote(req.Image))
	fmt.Fprintf(&b, "provider_extra = %s\n", hclStringMap(req.ProviderExtra))
	if req.Provider == "generic" {
		fmt.Fprintf(&b, "host = %s\n", hclQuote(req.Host))
		sshUser := req.SSHUser
		if sshUser == "" {
			sshUser = "root"
		}
		fmt.Fprintf(&b, "ssh_user = %s\n", hclQuote(sshUser))
		port := req.SSHPort
		if port == 0 {
			port = 22
		}
		fmt.Fprintf(&b, "ssh_port = %d\n", port)
		fmt.Fprintf(&b, "ssh_private_key = %s\n", hclQuote(req.SSHPrivateKey))
	}
	return b.String(), nil
}

// BackendArgs returns the -backend-config flags for init. Local pins the
// state file under stateLocalDir; s3 addresses bucket/key/region (+ endpoint
// when set). Both shapes are asserted by tests.
func BackendArgs(stateBackend, stateLocalDir, workspace, s3Bucket, s3Region, s3Endpoint string) []string {
	switch stateBackend {
	case "", "local":
		dir := strings.TrimSuffix(stateLocalDir, "/")
		if dir == "" {
			dir = "."
		}
		return []string{"-backend-config=path=" + dir + "/" + workspace + ".tfstate"}
	case "s3":
		region := s3Region
		if region == "" {
			region = "auto"
		}
		args := []string{
			"-backend-config=bucket=" + s3Bucket,
			"-backend-config=key=" + workspace + "/terraform.tfstate",
			"-backend-config=region=" + region,
		}
		if s3Endpoint != "" {
			args = append(args, "-backend-config=endpoint="+s3Endpoint)
		}
		return args
	default:
		return []string{"-backend-config=path=" + workspace + ".tfstate"}
	}
}
