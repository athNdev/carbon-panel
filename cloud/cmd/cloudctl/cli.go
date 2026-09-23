// Package main implements cloudctl, the Carbon Cloud command-line interface.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/athNdev/carbon-panel/pkg/proto/cloud/v1/cloudv1connect"
)

// Config represents persistent CLI settings.
type Config struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
	OrgID    string `json:"org_id,omitempty"`
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "carbon-cloud", "config.json")
}

func loadConfig(path string) (*Config, error) {
	if path == "" {
		path = defaultConfigPath()
	}
	cfg := &Config{
		Endpoint: "http://127.0.0.1:8080",
	}
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "http://127.0.0.1:8080"
	}
	return cfg, nil
}

func saveConfig(path string, cfg *Config) error {
	if path == "" {
		path = defaultConfigPath()
	}
	if path == "" {
		return errors.New("cannot determine home directory")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// Client wraps Connect RPC clients for cloudctl.
type Client struct {
	HTTPClient  *http.Client
	Endpoint    string
	BearerToken string
	OrgID       string

	System    cloudv1connect.SystemServiceClient
	Session   cloudv1connect.SessionServiceClient
	Node      cloudv1connect.NodeServiceClient
	Workload  cloudv1connect.WorkloadServiceClient
	Provision cloudv1connect.ProvisionServiceClient
	NodeType  cloudv1connect.NodeTypeServiceClient
	APIKey    cloudv1connect.ApiKeyServiceClient
	Audit     cloudv1connect.AuditServiceClient
	Org       cloudv1connect.OrgServiceClient
}

type authInterceptor struct {
	token string
	orgID string
}

func (a *authInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if a.token != "" {
			req.Header().Set("Authorization", "Bearer "+a.token)
		}
		if a.orgID != "" {
			req.Header().Set("X-Carbon-Org-Id", a.orgID)
		}
		return next(ctx, req)
	}
}

func (a *authInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		if a.token != "" {
			conn.RequestHeader().Set("Authorization", "Bearer "+a.token)
		}
		if a.orgID != "" {
			conn.RequestHeader().Set("X-Carbon-Org-Id", a.orgID)
		}
		return conn
	}
}

func (a *authInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

// NewClient initializes a client with endpoint and auth headers.
func NewClient(endpoint, token, orgID string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	endpoint = strings.TrimRight(endpoint, "/")
	interceptor := &authInterceptor{token: token, orgID: orgID}
	opts := connect.WithInterceptors(interceptor)

	return &Client{
		HTTPClient:  httpClient,
		Endpoint:    endpoint,
		BearerToken: token,
		OrgID:       orgID,

		System:    cloudv1connect.NewSystemServiceClient(httpClient, endpoint, opts),
		Session:   cloudv1connect.NewSessionServiceClient(httpClient, endpoint, opts),
		Node:      cloudv1connect.NewNodeServiceClient(httpClient, endpoint, opts),
		Workload:  cloudv1connect.NewWorkloadServiceClient(httpClient, endpoint, opts),
		Provision: cloudv1connect.NewProvisionServiceClient(httpClient, endpoint, opts),
		NodeType:  cloudv1connect.NewNodeTypeServiceClient(httpClient, endpoint, opts),
		APIKey:    cloudv1connect.NewApiKeyServiceClient(httpClient, endpoint, opts),
		Audit:     cloudv1connect.NewAuditServiceClient(httpClient, endpoint, opts),
		Org:       cloudv1connect.NewOrgServiceClient(httpClient, endpoint, opts),
	}
}

// CLI holds the execution environment for command parsing.
type CLI struct {
	Stdout     io.Writer
	Stderr     io.Writer
	ConfigPath string
	Client     *Client
	JSONOutput bool
}

// Run executes the command line arguments.
func (c *CLI) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return c.printHelp()
	}

	cfg, err := loadConfig(c.ConfigPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	var (
		endpointFlag string
		tokenFlag    string
		apiKeyFlag   string
		orgFlag      string
		jsonFlag     bool
	)

	// Global flags can appear before or after subcommand. We'll parse top-level args.
	var cleanArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--json" || arg == "-j":
			jsonFlag = true
		case strings.HasPrefix(arg, "--endpoint="):
			endpointFlag = strings.TrimPrefix(arg, "--endpoint=")
		case arg == "--endpoint" && i+1 < len(args):
			endpointFlag = args[i+1]
			i++
		case strings.HasPrefix(arg, "--token="):
			tokenFlag = strings.TrimPrefix(arg, "--token=")
		case arg == "--token" && i+1 < len(args):
			tokenFlag = args[i+1]
			i++
		case strings.HasPrefix(arg, "--api-key="):
			apiKeyFlag = strings.TrimPrefix(arg, "--api-key=")
		case arg == "--api-key" && i+1 < len(args):
			apiKeyFlag = args[i+1]
			i++
		case strings.HasPrefix(arg, "--org="):
			orgFlag = strings.TrimPrefix(arg, "--org=")
		case arg == "--org" && i+1 < len(args):
			orgFlag = args[i+1]
			i++
		default:
			cleanArgs = append(cleanArgs, arg)
		}
	}

	c.JSONOutput = jsonFlag

	endpoint := cfg.Endpoint
	if env := os.Getenv("CARBON_CLOUD_ENDPOINT"); env != "" {
		endpoint = env
	}
	if endpointFlag != "" {
		endpoint = endpointFlag
	}

	token := cfg.Token
	if cfg.APIKey != "" {
		token = cfg.APIKey
	}
	if env := os.Getenv("CARBON_CLOUD_TOKEN"); env != "" {
		token = env
	}
	if env := os.Getenv("CARBON_CLOUD_API_KEY"); env != "" {
		token = env
	}
	if tokenFlag != "" {
		token = tokenFlag
	}
	if apiKeyFlag != "" {
		token = apiKeyFlag
	}

	orgID := cfg.OrgID
	if env := os.Getenv("CARBON_CLOUD_ORG_ID"); env != "" {
		orgID = env
	}
	if orgFlag != "" {
		orgID = orgFlag
	}

	if c.Client == nil {
		c.Client = NewClient(endpoint, token, orgID, nil)
	}

	if len(cleanArgs) == 0 {
		return c.printHelp()
	}

	cmd := cleanArgs[0]
	subArgs := cleanArgs[1:]

	switch cmd {
	case "help", "-h", "--help":
		return c.printHelp()
	case "status", "info":
		return c.runStatus(ctx)
	case "whoami":
		return c.runWhoami(ctx)
	case "config":
		return c.runConfig(cfg, subArgs)
	case "nodes":
		return c.runNodes(ctx, subArgs)
	case "tokens":
		return c.runTokens(ctx, subArgs)
	case "nodetypes":
		return c.runNodeTypes(ctx, subArgs)
	case "provisions":
		return c.runProvisions(ctx, subArgs)
	case "workloads":
		return c.runWorkloads(ctx, subArgs)
	case "apikeys":
		return c.runAPIKeys(ctx, subArgs)
	case "audit":
		return c.runAudit(ctx, subArgs)
	case "orgs":
		return c.runOrgs(ctx, subArgs)
	default:
		return fmt.Errorf("unknown command: %s (run 'cloudctl help' for usage)", cmd)
	}
}

func (c *CLI) printHelp() error {
	help := `cloudctl - Carbon Cloud management CLI

Usage:
  cloudctl [flags] <command> [subcommand] [arguments...]

Global Flags:
  --endpoint <url>   Carbon Cloud control plane URL (default: http://127.0.0.1:8080)
  --token <token>    Session token for authentication
  --api-key <key>    Org API key for authentication (starts with cc_)
  --org <org_id>     Target Organization ID
  --json, -j         Format output as JSON

Commands:
  status, info       Show control plane build info and system capabilities
  whoami             Show current authenticated principal and organization
  config             View or update local CLI configuration (view, set)
  nodes              Manage nodes (list, get, drain, resume, delete)
  tokens             Manage node join tokens (create, list, revoke)
  nodetypes          List node types catalog
  provisions         Manage cloud provisions (list, get, plan, create, destroy, logs)
  workloads          Manage workloads (list, get, start, stop, delete)
  apikeys            Manage organization API keys (list, create, revoke)
  audit              View audit trail (list, get)
  orgs               Manage organizations (list, get)
`
	_, err := fmt.Fprint(c.Stdout, help)
	return err
}

func (c *CLI) printOutput(v any, formatFunc func(io.Writer) error) error {
	if c.JSONOutput {
		enc := json.NewEncoder(c.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	return formatFunc(c.Stdout)
}

func (c *CLI) runStatus(ctx context.Context) error {
	build, err := c.Client.System.GetBuildInfo(ctx, connect.NewRequest(&v1.GetBuildInfoRequest{}))
	if err != nil {
		return fmt.Errorf("get build info: %w", err)
	}
	caps, err := c.Client.System.GetCapabilities(ctx, connect.NewRequest(&v1.GetCapabilitiesRequest{}))
	if err != nil {
		return fmt.Errorf("get capabilities: %w", err)
	}

	return c.printOutput(map[string]any{
		"build":        build.Msg,
		"capabilities": caps.Msg,
	}, func(w io.Writer) error {
		_, _ = fmt.Fprintf(w, "Carbon Cloud Control Plane Status\n")
		if b := build.Msg.Build; b != nil {
			fmt.Fprintf(w, "  Version:    %s\n", b.Version)
			fmt.Fprintf(w, "  Commit:     %s\n", b.Commit)
			fmt.Fprintf(w, "  Build Time: %s\n", b.BuildTime)
			fmt.Fprintf(w, "  Go Version: %s\n", b.GoVersion)
			fmt.Fprintf(w, "  DB Driver:  %s\n", b.DatabaseDriver)
		}
		fmt.Fprintf(w, "Capabilities (%d):\n", len(caps.Msg.Capabilities))
		for _, cap := range caps.Msg.Capabilities {
			status := "enabled"
			if !cap.Enabled {
				status = "disabled"
				if len(cap.MissingKeys) > 0 {
					status = fmt.Sprintf("disabled (missing keys: %s)", strings.Join(cap.MissingKeys, ", "))
				}
			}
			fmt.Fprintf(w, "  - %s: %s\n", cap.Id, status)
		}
		return nil
	})
}

func (c *CLI) runWhoami(ctx context.Context) error {
	resp, err := c.Client.Session.ListMyOrgs(ctx, connect.NewRequest(&v1.ListMyOrgsRequest{}))
	if err != nil {
		return fmt.Errorf("whoami failed (ensure --token or --api-key is valid): %w", err)
	}
	return c.printOutput(resp.Msg, func(w io.Writer) error {
		fmt.Fprintf(w, "Authenticated organizations (%d):\n", len(resp.Msg.Orgs))
		for _, org := range resp.Msg.Orgs {
			active := ""
			if c.Client.OrgID == org.Id {
				active = " [active]"
			}
			fmt.Fprintf(w, "  - ID: %s | Name: %s | Slug: %s%s\n", org.Id, org.Name, org.Slug, active)
		}
		return nil
	})
}

func (c *CLI) runConfig(cfg *Config, args []string) error {
	if len(args) == 0 || args[0] == "view" {
		return c.printOutput(cfg, func(w io.Writer) error {
			fmt.Fprintf(w, "Current CLI Configuration:\n")
			fmt.Fprintf(w, "  Endpoint: %s\n", cfg.Endpoint)
			fmt.Fprintf(w, "  Org ID:   %s\n", cfg.OrgID)
			fmt.Fprintf(w, "  API Key:  %s\n", maskKey(cfg.APIKey))
			fmt.Fprintf(w, "  Token:    %s\n", maskKey(cfg.Token))
			return nil
		})
	}

	if args[0] == "set" && len(args) >= 3 {
		key := args[1]
		val := args[2]
		switch key {
		case "endpoint":
			cfg.Endpoint = val
		case "api_key", "apikey":
			cfg.APIKey = val
		case "token":
			cfg.Token = val
		case "org_id", "org":
			cfg.OrgID = val
		default:
			return fmt.Errorf("unknown config property: %s", key)
		}
		if err := saveConfig(c.ConfigPath, cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Fprintf(c.Stdout, "Config updated: %s = %s\n", key, val)
		return nil
	}

	return errors.New("usage: cloudctl config [view | set <endpoint|api_key|token|org> <value>]")
}

func (c *CLI) runNodes(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "list" {
		resp, err := c.Client.Node.ListNodes(ctx, connect.NewRequest(&v1.ListNodesRequest{}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Nodes, func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(tw, "ID\tNAME\tSTATUS\tORIGIN\tADDRESS\tALLOCATIONS")
			for _, n := range resp.Msg.Nodes {
				alloc := ""
				if n.Capacity != nil && n.Allocation != nil {
					alloc = fmt.Sprintf("%d/%d MB RAM, %d/%d mCPU",
						n.Allocation.RamMb, n.Capacity.RamMb,
						n.Allocation.CpuMillicores, int64(n.Capacity.Vcpu)*1000)
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
					n.Id, n.Name, n.Status, n.Origin, n.PublicIp, alloc)
			}
			return tw.Flush()
		})
	}

	switch args[0] {
	case "get":
		if len(args) < 2 {
			return errors.New("usage: cloudctl nodes get <id>")
		}
		resp, err := c.Client.Node.GetNode(ctx, connect.NewRequest(&v1.GetNodeRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Node, func(w io.Writer) error {
			n := resp.Msg.Node
			fmt.Fprintf(w, "Node: %s (%s)\n", n.Name, n.Id)
			fmt.Fprintf(w, "  Status:    %s\n", n.Status)
			fmt.Fprintf(w, "  Origin:    %s\n", n.Origin)
			fmt.Fprintf(w, "  Public IP: %s\n", n.PublicIp)
			return nil
		})
	case "drain":
		if len(args) < 2 {
			return errors.New("usage: cloudctl nodes drain <id>")
		}
		resp, err := c.Client.Node.DrainNode(ctx, connect.NewRequest(&v1.DrainNodeRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		fmt.Fprintf(c.Stdout, "Node %s is now draining (status: %s)\n", resp.Msg.Node.Id, resp.Msg.Node.Status)
		return nil
	case "resume":
		if len(args) < 2 {
			return errors.New("usage: cloudctl nodes resume <id>")
		}
		resp, err := c.Client.Node.ResumeNode(ctx, connect.NewRequest(&v1.ResumeNodeRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		fmt.Fprintf(c.Stdout, "Node %s resumed (status: %s)\n", resp.Msg.Node.Id, resp.Msg.Node.Status)
		return nil
	case "delete":
		if len(args) < 2 {
			return errors.New("usage: cloudctl nodes delete <id>")
		}
		_, err := c.Client.Node.DeleteNode(ctx, connect.NewRequest(&v1.DeleteNodeRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		fmt.Fprintf(c.Stdout, "Node %s deleted\n", args[1])
		return nil
	default:
		return fmt.Errorf("unknown nodes command: %s", args[0])
	}
}

func (c *CLI) runTokens(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "list" {
		resp, err := c.Client.Node.ListJoinTokens(ctx, connect.NewRequest(&v1.ListJoinTokensRequest{}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Tokens, func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(tw, "ID\tNAME\tORIGIN\tSTATUS\tEXPIRES")
			for _, t := range resp.Msg.Tokens {
				status := "active"
				if t.RevokedAt != nil {
					status = "revoked"
				} else if t.UsedAt != nil {
					status = "used"
				}
				expires := ""
				if t.ExpiresAt != nil {
					expires = t.ExpiresAt.AsTime().Format(time.RFC3339)
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", t.Id, t.Name, t.Origin, status, expires)
			}
			return tw.Flush()
		})
	}

	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("tokens create", flag.ContinueOnError)
		name := fs.String("name", "byon-node", "Human-readable label")
		nodeType := fs.String("type", "small", "Node type ID")
		ttl := fs.Duration("ttl", 24*time.Hour, "Token time-to-live")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}

		resp, err := c.Client.Node.CreateJoinToken(ctx, connect.NewRequest(&v1.CreateJoinTokenRequest{
			Name:       *name,
			NodeTypeId: *nodeType,
			TtlSeconds: int64(ttl.Seconds()),
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg, func(w io.Writer) error {
			fmt.Fprintf(w, "Join Token Created:\n")
			fmt.Fprintf(w, "  Token ID: %s\n", resp.Msg.Token.Id)
			fmt.Fprintf(w, "  Secret:   %s\n", resp.Msg.Secret)
			fmt.Fprintf(w, "\nBootstrap command:\n")
			fmt.Fprintf(w, "  curl -fsSL %s/bootstrap | sudo bash -s -- --token %s\n",
				c.Client.Endpoint, resp.Msg.Secret)
			return nil
		})
	case "revoke":
		if len(args) < 2 {
			return errors.New("usage: cloudctl tokens revoke <id>")
		}
		_, err := c.Client.Node.RevokeJoinToken(ctx, connect.NewRequest(&v1.RevokeJoinTokenRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		fmt.Fprintf(c.Stdout, "Join token %s revoked\n", args[1])
		return nil
	default:
		return fmt.Errorf("unknown tokens command: %s", args[0])
	}
}

func (c *CLI) runNodeTypes(ctx context.Context, args []string) error {
	resp, err := c.Client.NodeType.ListNodeTypes(ctx, connect.NewRequest(&v1.ListNodeTypesRequest{}))
	if err != nil {
		return err
	}
	return c.printOutput(resp.Msg.NodeTypes, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
		_, _ = fmt.Fprintln(tw, "ID\tNAME\tvCPU\tRAM\tDISK\tPRICE/MO\tENABLED")
		for _, nt := range resp.Msg.NodeTypes {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%d MB\t%d GB\t$%.2f\t%t\n",
				nt.Id, nt.Name, nt.Vcpu, nt.RamMb, nt.DiskGb, nt.MonthlyPriceUsd, nt.Enabled)
		}
		return tw.Flush()
	})
}

func parseProvider(s string) v1.ProviderId {
	switch strings.ToLower(s) {
	case "generic":
		return v1.ProviderId_PROVIDER_ID_GENERIC
	case "hetzner":
		return v1.ProviderId_PROVIDER_ID_HETZNER
	case "aws":
		return v1.ProviderId_PROVIDER_ID_AWS
	case "gcp":
		return v1.ProviderId_PROVIDER_ID_GCP
	case "digitalocean", "do":
		return v1.ProviderId_PROVIDER_ID_DIGITALOCEAN
	case "proxmox":
		return v1.ProviderId_PROVIDER_ID_PROXMOX
	default:
		return v1.ProviderId_PROVIDER_ID_UNSPECIFIED
	}
}

func (c *CLI) runProvisions(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "list" {
		resp, err := c.Client.Provision.ListProvisions(ctx, connect.NewRequest(&v1.ListProvisionsRequest{}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Provisions, func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(tw, "ID\tPROVIDER\tREGION\tSTATUS\tNODE ID\tERROR")
			for _, p := range resp.Msg.Provisions {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
					p.Id, p.Provider, p.Region, p.Status, p.NodeId, p.Error)
			}
			return tw.Flush()
		})
	}

	switch args[0] {
	case "get":
		if len(args) < 2 {
			return errors.New("usage: cloudctl provisions get <id>")
		}
		resp, err := c.Client.Provision.GetProvision(ctx, connect.NewRequest(&v1.GetProvisionRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Provision, func(w io.Writer) error {
			p := resp.Msg.Provision
			fmt.Fprintf(w, "Provision: %s\n", p.Id)
			fmt.Fprintf(w, "  Provider: %s (region: %s)\n", p.Provider, p.Region)
			fmt.Fprintf(w, "  Status:   %s\n", p.Status)
			if p.NodeId != "" {
				fmt.Fprintf(w, "  Node ID:  %s\n", p.NodeId)
			}
			if p.Error != "" {
				fmt.Fprintf(w, "  Error:    %s\n", p.Error)
			}
			return nil
		})
	case "plan":
		if len(args) < 2 {
			return errors.New("usage: cloudctl provisions plan <provision_id>")
		}
		resp, err := c.Client.Provision.PlanProvision(ctx, connect.NewRequest(&v1.PlanProvisionRequest{
			Id: args[1],
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg, func(w io.Writer) error {
			p := resp.Msg.Provision
			fmt.Fprintf(w, "Provision Planned: %s\n", p.Id)
			fmt.Fprintf(w, "  Summary: %s\n", p.PlanSummary)
			fmt.Fprintf(w, "\nTerraform Plan Diff:\n%s\n", p.PlanDiff)
			return nil
		})
	case "create":
		fs := flag.NewFlagSet("provisions create", flag.ContinueOnError)
		name := fs.String("name", "managed-node", "Node display name")
		providerStr := fs.String("provider", "hetzner", "Cloud provider")
		region := fs.String("region", "fsn1", "Provider region")
		nodeType := fs.String("type", "small", "Node type ID")
		planOnly := fs.Bool("plan-only", false, "Stop after plan")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		resp, err := c.Client.Provision.CreateProvision(ctx, connect.NewRequest(&v1.CreateProvisionRequest{
			Name:       *name,
			Provider:   parseProvider(*providerStr),
			Region:     *region,
			NodeTypeId: *nodeType,
			PlanOnly:   *planOnly,
		}))
		if err != nil {
			return err
		}
		fmt.Fprintf(c.Stdout, "Provision started: ID %s (status: %s)\n", resp.Msg.Provision.Id, resp.Msg.Provision.Status)
		return nil
	case "destroy":
		if len(args) < 2 {
			return errors.New("usage: cloudctl provisions destroy <id>")
		}
		id := args[1]
		resp, err := c.Client.Provision.DestroyProvision(ctx, connect.NewRequest(&v1.DestroyProvisionRequest{
			Id:        id,
			ConfirmId: id,
		}))
		if err != nil {
			return err
		}
		fmt.Fprintf(c.Stdout, "Destroying provision: %s (status: %s)\n", resp.Msg.Provision.Id, resp.Msg.Provision.Status)
		return nil
	case "logs":
		if len(args) < 2 {
			return errors.New("usage: cloudctl provisions logs <id>")
		}
		stream, err := c.Client.Provision.StreamProvisionLogs(ctx, connect.NewRequest(&v1.StreamProvisionLogsRequest{
			Id: args[1],
		}))
		if err != nil {
			return err
		}
		for stream.Receive() {
			msg := stream.Msg()
			fmt.Fprintf(c.Stdout, "[%d] %s\n", msg.Sequence, msg.Line)
		}
		return stream.Err()
	default:
		return fmt.Errorf("unknown provisions command: %s", args[0])
	}
}

func (c *CLI) runWorkloads(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "list" {
		resp, err := c.Client.Workload.ListWorkloads(ctx, connect.NewRequest(&v1.ListWorkloadsRequest{}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Workloads, func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(tw, "ID\tNAME\tSTATUS\tNODE ID\tVERSION\tALLOCATIONS")
			for _, wl := range resp.Msg.Workloads {
				alloc := ""
				version := ""
				if req := wl.Spec; req != nil {
					alloc = fmt.Sprintf("%d MB, %d mCPU", req.MemoryMb, req.CpuMillicores)
					version = req.MinecraftVersion
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
					wl.Id, wl.Name, wl.Status, wl.NodeId, version, alloc)
			}
			return tw.Flush()
		})
	}

	switch args[0] {
	case "get":
		if len(args) < 2 {
			return errors.New("usage: cloudctl workloads get <id>")
		}
		resp, err := c.Client.Workload.GetWorkload(ctx, connect.NewRequest(&v1.GetWorkloadRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Workload, func(w io.Writer) error {
			wl := resp.Msg.Workload
			fmt.Fprintf(w, "Workload: %s (%s)\n", wl.Name, wl.Id)
			fmt.Fprintf(w, "  Status:  %s\n", wl.Status)
			fmt.Fprintf(w, "  Node ID: %s\n", wl.NodeId)
			if wl.Spec != nil {
				fmt.Fprintf(w, "  Version: %s (%s)\n", wl.Spec.MinecraftVersion, wl.Spec.Loader)
			}
			return nil
		})
	case "start":
		if len(args) < 2 {
			return errors.New("usage: cloudctl workloads start <id>")
		}
		resp, err := c.Client.Workload.StartWorkload(ctx, connect.NewRequest(&v1.StartWorkloadRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		fmt.Fprintf(c.Stdout, "Workload %s started (status: %s)\n", resp.Msg.Workload.Id, resp.Msg.Workload.Status)
		return nil
	case "stop":
		if len(args) < 2 {
			return errors.New("usage: cloudctl workloads stop <id>")
		}
		resp, err := c.Client.Workload.StopWorkload(ctx, connect.NewRequest(&v1.StopWorkloadRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		fmt.Fprintf(c.Stdout, "Workload %s stopped (status: %s)\n", resp.Msg.Workload.Id, resp.Msg.Workload.Status)
		return nil
	case "delete":
		if len(args) < 2 {
			return errors.New("usage: cloudctl workloads delete <id>")
		}
		_, err := c.Client.Workload.DeleteWorkload(ctx, connect.NewRequest(&v1.DeleteWorkloadRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		fmt.Fprintf(c.Stdout, "Workload %s deleted\n", args[1])
		return nil
	default:
		return fmt.Errorf("unknown workloads command: %s", args[0])
	}
}

func (c *CLI) runAPIKeys(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "list" {
		resp, err := c.Client.APIKey.ListApiKeys(ctx, connect.NewRequest(&v1.ListApiKeysRequest{}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.ApiKeys, func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
			fmt.Fprintln(tw, "ID\tNAME\tPREFIX\tPERMISSIONS\tSTATUS\tCREATED")
			for _, k := range resp.Msg.ApiKeys {
				status := "active"
				if k.RevokedAt != nil {
					status = "revoked"
				}
				created := ""
				if k.CreatedAt != nil {
					created = k.CreatedAt.AsTime().Format(time.RFC3339)
				}
				perms := strings.Join(k.Permissions, ",")
				if perms == "" {
					perms = "*"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
					k.Id, k.Name, k.Prefix, perms, status, created)
			}
			return tw.Flush()
		})
	}

	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("apikeys create", flag.ContinueOnError)
		name := fs.String("name", "ci-deployer", "Key name")
		perms := fs.String("permissions", "nodes.read,nodes.create", "Comma-separated permissions")
		ttlSec := fs.Int64("expires-in", 0, "Expiry in seconds (0 = never)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		var permList []string
		if *perms != "" {
			for _, p := range strings.Split(*perms, ",") {
				if trimmed := strings.TrimSpace(p); trimmed != "" {
					permList = append(permList, trimmed)
				}
			}
		}
		resp, err := c.Client.APIKey.CreateApiKey(ctx, connect.NewRequest(&v1.CreateApiKeyRequest{
			Name:             *name,
			Permissions:      permList,
			ExpiresInSeconds: *ttlSec,
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg, func(w io.Writer) error {
			fmt.Fprintf(w, "API Key Created:\n")
			fmt.Fprintf(w, "  ID:     %s\n", resp.Msg.ApiKey.Id)
			fmt.Fprintf(w, "  Name:   %s\n", resp.Msg.ApiKey.Name)
			fmt.Fprintf(w, "  Secret: %s\n", resp.Msg.Secret)
			fmt.Fprintf(w, "\nImportant: Save this secret key now. It will not be shown again.\n")
			return nil
		})
	case "revoke":
		if len(args) < 2 {
			return errors.New("usage: cloudctl apikeys revoke <id>")
		}
		_, err := c.Client.APIKey.RevokeApiKey(ctx, connect.NewRequest(&v1.RevokeApiKeyRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		fmt.Fprintf(c.Stdout, "API key %s revoked\n", args[1])
		return nil
	default:
		return fmt.Errorf("unknown apikeys command: %s", args[0])
	}
}

func (c *CLI) runAudit(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "list" {
		fs := flag.NewFlagSet("audit list", flag.ContinueOnError)
		action := fs.String("action", "", "Filter by action prefix")
		resType := fs.String("type", "", "Filter by resource type")
		if err := fs.Parse(args[min(len(args), 1):]); err != nil {
			return err
		}

		resp, err := c.Client.Audit.ListAuditEvents(ctx, connect.NewRequest(&v1.ListAuditEventsRequest{
			ActionPrefix: *action,
			ResourceType: *resType,
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Events, func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
			fmt.Fprintln(tw, "ID\tTIMESTAMP\tACTION\tACTOR\tSTATUS\tRESOURCE")
			for _, e := range resp.Msg.Events {
				ts := ""
				if e.CreatedAt != nil {
					ts = e.CreatedAt.AsTime().Format(time.RFC3339)
				}
				actor := e.ActorUserId
				if actor == "" {
					actor = e.ActorApiKeyId
				}
				if actor == "" {
					actor = "system"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
					e.Id, ts, e.Action, actor, e.Result, e.ResourceType+":"+e.ResourceId)
			}
			return tw.Flush()
		})
	}

	if args[0] == "get" {
		if len(args) < 2 {
			return errors.New("usage: cloudctl audit get <id>")
		}
		resp, err := c.Client.Audit.GetAuditEvent(ctx, connect.NewRequest(&v1.GetAuditEventRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Event, func(w io.Writer) error {
			e := resp.Msg.Event
			fmt.Fprintf(w, "Audit Event: %s\n", e.Id)
			fmt.Fprintf(w, "  Action:    %s\n", e.Action)
			fmt.Fprintf(w, "  Actor:     %s\n", e.ActorUserId)
			fmt.Fprintf(w, "  Result:    %s\n", e.Result)
			fmt.Fprintf(w, "  Resource:  %s (%s)\n", e.ResourceId, e.ResourceType)
			if e.DetailJson != "" {
				fmt.Fprintf(w, "  Detail:    %s\n", e.DetailJson)
			}
			return nil
		})
	}

	return fmt.Errorf("unknown audit command: %s", args[0])
}

func (c *CLI) runOrgs(ctx context.Context, args []string) error {
	resp, err := c.Client.Session.ListMyOrgs(ctx, connect.NewRequest(&v1.ListMyOrgsRequest{}))
	if err != nil {
		return err
	}
	return c.printOutput(resp.Msg.Orgs, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tSLUG\tPLAN")
		for _, o := range resp.Msg.Orgs {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
				o.Id, o.Name, o.Slug, o.Plan)
		}
		return tw.Flush()
	})
}

func maskKey(k string) string {
	if len(k) <= 8 {
		return "***"
	}
	return k[:4] + "..." + k[len(k)-4:]
}
