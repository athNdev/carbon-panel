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
	File      cloudv1connect.FileServiceClient
	Blueprint cloudv1connect.BlueprintServiceClient
	Addon     cloudv1connect.AddonServiceClient
	Schedule  cloudv1connect.ScheduleServiceClient
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
		httpClient = &http.Client{Timeout: 120 * time.Second}
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
		File:      cloudv1connect.NewFileServiceClient(httpClient, endpoint, opts),
		Blueprint: cloudv1connect.NewBlueprintServiceClient(httpClient, endpoint, opts),
		Addon:     cloudv1connect.NewAddonServiceClient(httpClient, endpoint, opts),
		Schedule:  cloudv1connect.NewScheduleServiceClient(httpClient, endpoint, opts),
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
	case "files":
		return c.runFiles(ctx, subArgs)
	case "blueprints":
		return c.runBlueprints(ctx, subArgs)
	case "addons":
		return c.runAddons(ctx, subArgs)
	case "schedules":
		return c.runSchedules(ctx, subArgs)
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
  provisions         Manage cloud provisions (list, get, plan, apply, create, destroy, logs)
  workloads          Manage workloads (list, get, create, start, stop, restart, delete, logs, exec, events, config, backup)
  apikeys            Manage organization API keys (list, create, revoke)
  audit              View audit trail (list, get)
  orgs               Manage organizations (list, get)
  files              Manage workload files (list, cat, put, rm, mkdir, stat)
  blueprints         Manage server blueprints & presets (list, get, create, delete)
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
			_, _ = fmt.Fprintf(w, "  Version:    %s\n", b.Version)
			_, _ = fmt.Fprintf(w, "  Commit:     %s\n", b.Commit)
			_, _ = fmt.Fprintf(w, "  Build Time: %s\n", b.BuildTime)
			_, _ = fmt.Fprintf(w, "  Go Version: %s\n", b.GoVersion)
			_, _ = fmt.Fprintf(w, "  DB Driver:  %s\n", b.DatabaseDriver)
		}
		_, _ = fmt.Fprintf(w, "Capabilities (%d):\n", len(caps.Msg.Capabilities))
		for _, cap := range caps.Msg.Capabilities {
			status := "enabled"
			if !cap.Enabled {
				status = "disabled"
				if len(cap.MissingKeys) > 0 {
					status = fmt.Sprintf("disabled (missing keys: %s)", strings.Join(cap.MissingKeys, ", "))
				}
			}
			_, _ = fmt.Fprintf(w, "  - %s: %s\n", cap.Id, status)
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
		_, _ = fmt.Fprintf(w, "Authenticated organizations (%d):\n", len(resp.Msg.Orgs))
		for _, org := range resp.Msg.Orgs {
			active := ""
			if c.Client.OrgID == org.Id {
				active = " [active]"
			}
			_, _ = fmt.Fprintf(w, "  - ID: %s | Name: %s | Slug: %s%s\n", org.Id, org.Name, org.Slug, active)
		}
		return nil
	})
}

func (c *CLI) runConfig(cfg *Config, args []string) error {
	if len(args) == 0 || args[0] == "view" {
		return c.printOutput(cfg, func(w io.Writer) error {
			_, _ = fmt.Fprintf(w, "Current CLI Configuration:\n")
			_, _ = fmt.Fprintf(w, "  Endpoint: %s\n", cfg.Endpoint)
			_, _ = fmt.Fprintf(w, "  Org ID:   %s\n", cfg.OrgID)
			_, _ = fmt.Fprintf(w, "  API Key:  %s\n", maskKey(cfg.APIKey))
			_, _ = fmt.Fprintf(w, "  Token:    %s\n", maskKey(cfg.Token))
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
		_, _ = fmt.Fprintf(c.Stdout, "Config updated: %s = %s\n", key, val)
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
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
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
			_, _ = fmt.Fprintf(w, "Node: %s (%s)\n", n.Name, n.Id)
			_, _ = fmt.Fprintf(w, "  Status:    %s\n", n.Status)
			_, _ = fmt.Fprintf(w, "  Origin:    %s\n", n.Origin)
			_, _ = fmt.Fprintf(w, "  Public IP: %s\n", n.PublicIp)
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
		_, _ = fmt.Fprintf(c.Stdout, "Node %s is now draining (status: %s)\n", resp.Msg.Node.Id, resp.Msg.Node.Status)
		return nil
	case "resume":
		if len(args) < 2 {
			return errors.New("usage: cloudctl nodes resume <id>")
		}
		resp, err := c.Client.Node.ResumeNode(ctx, connect.NewRequest(&v1.ResumeNodeRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(c.Stdout, "Node %s resumed (status: %s)\n", resp.Msg.Node.Id, resp.Msg.Node.Status)
		return nil
	case "delete":
		if len(args) < 2 {
			return errors.New("usage: cloudctl nodes delete <id>")
		}
		_, err := c.Client.Node.DeleteNode(ctx, connect.NewRequest(&v1.DeleteNodeRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(c.Stdout, "Node %s deleted\n", args[1])
		return nil
	case "ports":
		if len(args) < 2 {
			return errors.New("usage: cloudctl nodes ports <node-id>")
		}
		resp, err := c.Client.Workload.ListNodePorts(ctx, connect.NewRequest(&v1.ListNodePortsRequest{NodeId: args[1]}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg, func(w io.Writer) error {
			msg := resp.Msg
			_, _ = fmt.Fprintf(w, "Port Allocations for Node %s:\n", msg.NodeId)
			_, _ = fmt.Fprintf(w, "  Assignable Pool: %d - %d\n", msg.PortRangeMin, msg.PortRangeMax)
			_, _ = fmt.Fprintf(w, "  Available Ports: %d\n\n", msg.AvailablePorts)
			if len(msg.Allocations) == 0 {
				_, _ = fmt.Fprintln(w, "No active port allocations.")
				return nil
			}
			tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(tw, "PORT\tWORKLOAD ID\tWORKLOAD NAME\tSTATUS\tHOSTNAME")
			for _, al := range msg.Allocations {
				_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", al.Port, al.WorkloadId, al.WorkloadName, al.Status, al.Hostname)
			}
			return tw.Flush()
		})
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
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", t.Id, t.Name, t.Origin, status, expires)
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
			_, _ = fmt.Fprintf(w, "Join Token Created:\n")
			_, _ = fmt.Fprintf(w, "  Token ID: %s\n", resp.Msg.Token.Id)
			_, _ = fmt.Fprintf(w, "  Secret:   %s\n", resp.Msg.Secret)
			_, _ = fmt.Fprintf(w, "\nBootstrap command:\n")
			_, _ = fmt.Fprintf(w, "  curl -fsSL %s/bootstrap | sudo bash -s -- --token %s\n",
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
		_, _ = fmt.Fprintf(c.Stdout, "Join token %s revoked\n", args[1])
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
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%d MB\t%d GB\t$%.2f\t%t\n",
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
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
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
			_, _ = fmt.Fprintf(w, "Provision: %s\n", p.Id)
			_, _ = fmt.Fprintf(w, "  Provider: %s (region: %s)\n", p.Provider, p.Region)
			_, _ = fmt.Fprintf(w, "  Status:   %s\n", p.Status)
			if p.NodeId != "" {
				_, _ = fmt.Fprintf(w, "  Node ID:  %s\n", p.NodeId)
			}
			if p.Error != "" {
				_, _ = fmt.Fprintf(w, "  Error:    %s\n", p.Error)
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
			_, _ = fmt.Fprintf(w, "Provision Planned: %s\n", p.Id)
			_, _ = fmt.Fprintf(w, "  Summary: %s\n", p.PlanSummary)
			_, _ = fmt.Fprintf(w, "\nTerraform Plan Diff:\n%s\n", p.PlanDiff)
			return nil
		})
	case "apply":
		if len(args) < 2 {
			return errors.New("usage: cloudctl provisions apply <id> [--plan-hash <hash>]")
		}
		id := args[1]
		fs := flag.NewFlagSet("provisions apply", flag.ContinueOnError)
		planHash := fs.String("plan-hash", "", "Confirm plan hash")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		resp, err := c.Client.Provision.ApplyProvision(ctx, connect.NewRequest(&v1.ApplyProvisionRequest{
			Id:              id,
			ConfirmPlanHash: *planHash,
		}))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(c.Stdout, "Provision applied: ID %s (status: %s)\n", resp.Msg.Provision.Id, resp.Msg.Provision.Status)
		return nil
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
		_, _ = fmt.Fprintf(c.Stdout, "Provision started: ID %s (status: %s)\n", resp.Msg.Provision.Id, resp.Msg.Provision.Status)
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
		_, _ = fmt.Fprintf(c.Stdout, "Destroying provision: %s (status: %s)\n", resp.Msg.Provision.Id, resp.Msg.Provision.Status)
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
			_, _ = fmt.Fprintf(c.Stdout, "[%d] %s\n", msg.Sequence, msg.Line)
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
			_, _ = fmt.Fprintln(tw, "ID\tNAME\tSTATUS\tPORT\tNODE ID\tVERSION\tALLOCATIONS")
			for _, wl := range resp.Msg.Workloads {
				alloc := ""
				version := ""
				if req := wl.Spec; req != nil {
					alloc = fmt.Sprintf("%d MB, %d mCPU", req.MemoryMb, req.CpuMillicores)
					version = req.MinecraftVersion
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
					wl.Id, wl.Name, wl.Status, wl.HostPort, wl.NodeId, version, alloc)
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
			_, _ = fmt.Fprintf(w, "Workload: %s (%s)\n", wl.Name, wl.Id)
			_, _ = fmt.Fprintf(w, "  Status:    %s\n", wl.Status)
			_, _ = fmt.Fprintf(w, "  Node ID:   %s\n", wl.NodeId)
			_, _ = fmt.Fprintf(w, "  Host Port: %d\n", wl.HostPort)
			if wl.Hostname != "" {
				_, _ = fmt.Fprintf(w, "  Hostname:  %s\n", wl.Hostname)
			}
			if wl.Spec != nil {
				_, _ = fmt.Fprintf(w, "  Version: %s (%s)\n", wl.Spec.MinecraftVersion, wl.Spec.Loader)
			}
			return nil
		})
	case "addons":
		return c.runWorkloadAddons(ctx, args[1:])
	case "schedules":
		return c.runSchedules(ctx, args[1:])
	case "create":
		fs := flag.NewFlagSet("workloads create", flag.ContinueOnError)
		name := fs.String("name", "", "Workload display name (required)")
		nodeID := fs.String("node", "", "Node ID to pin workload to (optional)")
		version := fs.String("version", "", "Minecraft version (defaults to blueprint/1.21.4)")
		loader := fs.String("loader", "", "Loader type (defaults to blueprint/paper)")
		blueprint := fs.String("blueprint", "", "Server preset/blueprint to provision from (e.g. paper, vanilla, fabric)")
		mem := fs.Int64("memory", 0, "Memory in MB (0 to inherit blueprint default)")
		cpu := fs.Int64("cpu", 0, "CPU millicores (0 to inherit blueprint default)")
		port := fs.Int("port", 0, "Host port to allocate (optional, 0 for dynamic)")
		hostname := fs.String("hostname", "", "Public hostname for workload (optional)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" {
			return errors.New("flag -name is required")
		}
		defaultLoader := *loader
		defaultVersion := *version
		if *blueprint == "" && defaultLoader == "" {
			defaultLoader = "paper"
		}
		if *blueprint == "" && defaultVersion == "" {
			defaultVersion = "1.21.4"
		}
		defaultMem := *mem
		if *blueprint == "" && defaultMem <= 0 {
			defaultMem = 2048
		}
		defaultCpu := *cpu
		if *blueprint == "" && defaultCpu <= 0 {
			defaultCpu = 1000
		}

		resp, err := c.Client.Workload.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
			Name:   *name,
			NodeId: *nodeID,
			Spec: &v1.WorkloadSpec{
				BlueprintId:      *blueprint,
				MinecraftVersion: defaultVersion,
				Loader:           defaultLoader,
				MemoryMb:         defaultMem,
				CpuMillicores:    defaultCpu,
				HostPort:         int32(*port),
				Hostname:         *hostname,
			},
		}))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(c.Stdout, "Workload created: ID %s (port: %d, status: %s)\n", resp.Msg.Workload.Id, resp.Msg.Workload.HostPort, resp.Msg.Workload.Status)
		return nil
	case "start":
		if len(args) < 2 {
			return errors.New("usage: cloudctl workloads start <id>")
		}
		resp, err := c.Client.Workload.StartWorkload(ctx, connect.NewRequest(&v1.StartWorkloadRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(c.Stdout, "Workload %s started (status: %s)\n", resp.Msg.Workload.Id, resp.Msg.Workload.Status)
		return nil
	case "stop":
		if len(args) < 2 {
			return errors.New("usage: cloudctl workloads stop <id>")
		}
		resp, err := c.Client.Workload.StopWorkload(ctx, connect.NewRequest(&v1.StopWorkloadRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(c.Stdout, "Workload %s stopped (status: %s)\n", resp.Msg.Workload.Id, resp.Msg.Workload.Status)
		return nil
	case "restart":
		if len(args) < 2 {
			return errors.New("usage: cloudctl workloads restart <id>")
		}
		resp, err := c.Client.Workload.RestartWorkload(ctx, connect.NewRequest(&v1.RestartWorkloadRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(c.Stdout, "Workload %s restarted (status: %s)\n", resp.Msg.Workload.Id, resp.Msg.Workload.Status)
		return nil
	case "delete":
		if len(args) < 2 {
			return errors.New("usage: cloudctl workloads delete <id>")
		}
		_, err := c.Client.Workload.DeleteWorkload(ctx, connect.NewRequest(&v1.DeleteWorkloadRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(c.Stdout, "Workload %s deleted\n", args[1])
		return nil
	case "logs":
		if len(args) < 2 {
			return errors.New("usage: cloudctl workloads logs <id> [--tail <lines>]")
		}
		id := args[1]
		fs := flag.NewFlagSet("workloads logs", flag.ContinueOnError)
		tail := fs.Int("tail", 100, "Number of trailing lines to view")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		stream, err := c.Client.Workload.StreamWorkloadLogs(ctx, connect.NewRequest(&v1.StreamWorkloadLogsRequest{
			Id:        id,
			TailLines: int32(*tail),
		}))
		if err != nil {
			return err
		}
		for stream.Receive() {
			msg := stream.Msg()
			if msg.Stderr {
				_, _ = fmt.Fprintf(c.Stderr, "[stderr] %s\n", msg.Line)
			} else {
				_, _ = fmt.Fprintln(c.Stdout, msg.Line)
			}
		}
		return stream.Err()
	case "exec":
		if len(args) < 3 {
			return errors.New("usage: cloudctl workloads exec <id> <command...>")
		}
		id := args[1]
		cmd := strings.Join(args[2:], " ")
		resp, err := c.Client.Workload.SendWorkloadCommand(ctx, connect.NewRequest(&v1.SendWorkloadCommandRequest{
			Id:      id,
			Command: cmd,
		}))
		if err != nil {
			return err
		}
		if resp.Msg.Output != "" {
			_, _ = fmt.Fprintln(c.Stdout, resp.Msg.Output)
		}
		return nil
	case "events":
		if len(args) < 2 {
			return errors.New("usage: cloudctl workloads events <id>")
		}
		resp, err := c.Client.Workload.ListWorkloadEvents(ctx, connect.NewRequest(&v1.ListWorkloadEventsRequest{
			Id: args[1],
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Events, func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(tw, "ID\tKIND\tMESSAGE\tCREATED_AT")
			for _, ev := range resp.Msg.Events {
				created := ""
				if ev.CreatedAt != nil {
					created = ev.CreatedAt.AsTime().Format(time.RFC3339)
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", ev.Id, ev.Kind, ev.Message, created)
			}
			return tw.Flush()
		})
	case "config":
		if len(args) < 2 {
			return errors.New("usage: cloudctl workloads config <get|set> <id> [args...]")
		}
		switch args[1] {
		case "get":
			if len(args) < 3 {
				return errors.New("usage: cloudctl workloads config get <id> [--file <path>]")
			}
			id := args[2]
			fs := flag.NewFlagSet("workloads config get", flag.ContinueOnError)
			file := fs.String("file", "server.properties", "Config file to read")
			if err := fs.Parse(args[3:]); err != nil {
				return err
			}
			resp, err := c.Client.Workload.GetWorkloadConfig(ctx, connect.NewRequest(&v1.GetWorkloadConfigRequest{
				Id:   id,
				File: *file,
			}))
			if err != nil {
				return err
			}
			return c.printOutput(resp.Msg, func(w io.Writer) error {
				tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
				_, _ = fmt.Fprintln(tw, "PROPERTY\tVALUE")
				for k, v := range resp.Msg.Properties {
					_, _ = fmt.Fprintf(tw, "%s\t%s\n", k, v)
				}
				return tw.Flush()
			})
		case "set":
			if len(args) < 4 {
				return errors.New("usage: cloudctl workloads config set <id> key=val [key=val...] [--file <path>] [--reload]")
			}
			id := args[2]
			var propPairs []string
			var flagArgs []string
			for _, a := range args[3:] {
				if strings.HasPrefix(a, "-") {
					flagArgs = append(flagArgs, a)
				} else {
					propPairs = append(propPairs, a)
				}
			}
			fs := flag.NewFlagSet("workloads config set", flag.ContinueOnError)
			file := fs.String("file", "server.properties", "Config file to mutate")
			reload := fs.Bool("reload", false, "Trigger reload command if workload is running")
			if err := fs.Parse(flagArgs); err != nil {
				return err
			}
			props := make(map[string]string)
			for _, pair := range propPairs {
				idx := strings.Index(pair, "=")
				if idx <= 0 {
					return fmt.Errorf("invalid property format %q, expected key=value", pair)
				}
				props[pair[:idx]] = pair[idx+1:]
			}
			resp, err := c.Client.Workload.UpdateWorkloadConfig(ctx, connect.NewRequest(&v1.UpdateWorkloadConfigRequest{
				Id:              id,
				File:            *file,
				Properties:      props,
				RestartOrReload: *reload,
			}))
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(c.Stdout, "Config updated (%s). %d properties active.\n", resp.Msg.ActionTaken, len(resp.Msg.Properties))
			return nil
		default:
			return fmt.Errorf("unknown config sub-command: %s", args[1])
		}
	case "backup":
		if len(args) < 2 {
			return errors.New("usage: cloudctl workloads backup <create|list|restore|delete|lock> <workload-id> [args...]")
		}
		switch args[1] {
		case "create":
			if len(args) < 3 {
				return errors.New("usage: cloudctl workloads backup create <workload-id> [--name <name>]")
			}
			id := args[2]
			fs := flag.NewFlagSet("workloads backup create", flag.ContinueOnError)
			name := fs.String("name", "", "Human-readable label for backup")
			if err := fs.Parse(args[3:]); err != nil {
				return err
			}
			resp, err := c.Client.Workload.CreateWorkloadBackup(ctx, connect.NewRequest(&v1.CreateWorkloadBackupRequest{
				Id:   id,
				Name: *name,
			}))
			if err != nil {
				return err
			}
			b := resp.Msg.Backup
			_, _ = fmt.Fprintf(c.Stdout, "Created backup: %s (%s, %d bytes)\n", b.Name, b.Id, b.SizeBytes)
			return nil
		case "list":
			if len(args) < 3 {
				return errors.New("usage: cloudctl workloads backup list <workload-id>")
			}
			id := args[2]
			resp, err := c.Client.Workload.ListWorkloadBackups(ctx, connect.NewRequest(&v1.ListWorkloadBackupsRequest{
				Id: id,
			}))
			if err != nil {
				return err
			}
			return c.printOutput(resp.Msg, func(w io.Writer) error {
				tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
				_, _ = fmt.Fprintln(tw, "BACKUP ID\tNAME\tSIZE\tLOCKED\tSTATUS\tCREATED")
				for _, b := range resp.Msg.Backups {
					created := ""
					if b.CreatedAt != nil {
						created = b.CreatedAt.AsTime().Format(time.RFC3339)
					}
					_, _ = fmt.Fprintf(tw, "%s\t%s\t%d bytes\t%t\t%s\t%s\n", b.Id, b.Name, b.SizeBytes, b.Locked, b.Status, created)
				}
				return tw.Flush()
			})
		case "restore":
			if len(args) < 4 {
				return errors.New("usage: cloudctl workloads backup restore <workload-id> <backup-id>")
			}
			id := args[2]
			backupID := args[3]
			resp, err := c.Client.Workload.RestoreWorkloadBackup(ctx, connect.NewRequest(&v1.RestoreWorkloadBackupRequest{
				Id:       id,
				BackupId: backupID,
			}))
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(c.Stdout, "%s\n", resp.Msg.Message)
			return nil
		case "delete":
			if len(args) < 4 {
				return errors.New("usage: cloudctl workloads backup delete <workload-id> <backup-id>")
			}
			id := args[2]
			backupID := args[3]
			_, err := c.Client.Workload.DeleteWorkloadBackup(ctx, connect.NewRequest(&v1.DeleteWorkloadBackupRequest{
				Id:       id,
				BackupId: backupID,
			}))
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(c.Stdout, "Deleted backup %s\n", backupID)
			return nil
		case "lock":
			if len(args) < 4 {
				return errors.New("usage: cloudctl workloads backup lock <workload-id> <backup-id> [--locked=true|false]")
			}
			id := args[2]
			backupID := args[3]
			fs := flag.NewFlagSet("workloads backup lock", flag.ContinueOnError)
			locked := fs.Bool("locked", true, "Whether to lock or unlock the backup")
			if err := fs.Parse(args[4:]); err != nil {
				return err
			}
			resp, err := c.Client.Workload.SetWorkloadBackupLocked(ctx, connect.NewRequest(&v1.SetWorkloadBackupLockedRequest{
				Id:       id,
				BackupId: backupID,
				Locked:   *locked,
			}))
			if err != nil {
				return err
			}
			lockWord := "unlocked"
			if resp.Msg.Backup.Locked {
				lockWord = "locked"
			}
			_, _ = fmt.Fprintf(c.Stdout, "Backup %s is now %s\n", backupID, lockWord)
			return nil
		default:
			return fmt.Errorf("unknown backup sub-command: %s", args[1])
		}
	case "networking", "net":
		if len(args) < 2 {
			return errors.New("usage: cloudctl workloads networking <id>")
		}
		resp, err := c.Client.Workload.GetWorkloadNetworking(ctx, connect.NewRequest(&v1.GetWorkloadNetworkingRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg, func(w io.Writer) error {
			net := resp.Msg
			_, _ = fmt.Fprintf(w, "Networking for Workload: %s\n", net.WorkloadId)
			_, _ = fmt.Fprintf(w, "  Node ID:         %s\n", net.NodeId)
			_, _ = fmt.Fprintf(w, "  Node Address:    %s\n", net.NodeAddress)
			_, _ = fmt.Fprintf(w, "  Host Port:       %d\n", net.HostPort)
			_, _ = fmt.Fprintf(w, "  Container Port:  %d\n", net.ContainerPort)
			if net.Hostname != "" {
				_, _ = fmt.Fprintf(w, "  Hostname:        %s\n", net.Hostname)
			}
			_, _ = fmt.Fprintf(w, "  Direct Connect:  %s\n", net.PrimaryAddress)
			if net.SrvRecord != "" {
				_, _ = fmt.Fprintf(w, "  DNS SRV Record:  %s\n", net.SrvRecord)
			}
			if net.PortConflict {
				_, _ = fmt.Fprintf(w, "  WARNING: Port collision detected on this node!\n")
			}
			return nil
		})
	case "metrics", "top":
		if len(args) < 2 {
			return errors.New("usage: cloudctl workloads metrics <workload-id>")
		}
		resp, err := c.Client.Workload.GetWorkloadMetrics(ctx, connect.NewRequest(&v1.GetWorkloadMetricsRequest{Id: args[1]}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Metrics, func(w io.Writer) error {
			m := resp.Msg.Metrics
			memPct := 0.0
			if m.MemoryLimitMb > 0 {
				memPct = (m.MemoryUsedMb / m.MemoryLimitMb) * 100.0
			}
			updated := "n/a"
			if m.UpdatedAt != nil {
				updated = m.UpdatedAt.AsTime().Format(time.RFC3339)
			}
			_, _ = fmt.Fprintf(w, "Workload Metrics: %s\n", m.WorkloadId)
			_, _ = fmt.Fprintf(w, "  CPU Usage:       %.2f%%\n", m.CpuPercent)
			_, _ = fmt.Fprintf(w, "  Memory Usage:    %.1f MB / %.1f MB (%.1f%%)\n", m.MemoryUsedMb, m.MemoryLimitMb, memPct)
			_, _ = fmt.Fprintf(w, "  Disk Usage:      %.2f MB (%d bytes)\n", float64(m.DiskUsedBytes)/(1024*1024), m.DiskUsedBytes)
			_, _ = fmt.Fprintf(w, "  Network I/O:     RX: %.2f MB | TX: %.2f MB\n", float64(m.NetworkRxBytes)/(1024*1024), float64(m.NetworkTxBytes)/(1024*1024))
			_, _ = fmt.Fprintf(w, "  Players:         %d / %d\n", m.PlayersOnline, m.MaxPlayers)
			if len(m.PlayerSample) > 0 {
				_, _ = fmt.Fprintf(w, "  Online Players:  %s\n", strings.Join(m.PlayerSample, ", "))
			}
			_, _ = fmt.Fprintf(w, "  TPS:             %.2f\n", m.Tps)
			_, _ = fmt.Fprintf(w, "  Updated At:      %s\n", updated)
			return nil
		})
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
			_, _ = fmt.Fprintln(tw, "ID\tNAME\tPREFIX\tPERMISSIONS\tSTATUS\tCREATED")
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
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
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
			_, _ = fmt.Fprintf(w, "API Key Created:\n")
			_, _ = fmt.Fprintf(w, "  ID:     %s\n", resp.Msg.ApiKey.Id)
			_, _ = fmt.Fprintf(w, "  Name:   %s\n", resp.Msg.ApiKey.Name)
			_, _ = fmt.Fprintf(w, "  Secret: %s\n", resp.Msg.Secret)
			_, _ = fmt.Fprintf(w, "\nImportant: Save this secret key now. It will not be shown again.\n")
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
		_, _ = fmt.Fprintf(c.Stdout, "API key %s revoked\n", args[1])
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
			_, _ = fmt.Fprintln(tw, "ID\tTIMESTAMP\tACTION\tACTOR\tSTATUS\tRESOURCE")
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
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
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
			_, _ = fmt.Fprintf(w, "Audit Event: %s\n", e.Id)
			_, _ = fmt.Fprintf(w, "  Action:    %s\n", e.Action)
			_, _ = fmt.Fprintf(w, "  Actor:     %s\n", e.ActorUserId)
			_, _ = fmt.Fprintf(w, "  Result:    %s\n", e.Result)
			_, _ = fmt.Fprintf(w, "  Resource:  %s (%s)\n", e.ResourceId, e.ResourceType)
			if e.DetailJson != "" {
				_, _ = fmt.Fprintf(w, "  Detail:    %s\n", e.DetailJson)
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
		_, _ = fmt.Fprintln(tw, "ID\tNAME\tSLUG\tPLAN")
		for _, o := range resp.Msg.Orgs {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
				o.Id, o.Name, o.Slug, o.Plan)
		}
		return tw.Flush()
	})
}


func (c *CLI) runFiles(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: cloudctl files <list|cat|put|rm|mkdir|stat> <workload-id> [args...]")
	}

	switch args[0] {
	case "list", "ls":
		if len(args) < 2 {
			return errors.New("usage: cloudctl files list <workload-id> [path]")
		}
		workloadID := args[1]
		path := ""
		if len(args) > 2 {
			path = args[2]
		}
		resp, err := c.Client.File.ListFiles(ctx, connect.NewRequest(&v1.ListFilesRequest{
			WorkloadId: workloadID,
			Path:       path,
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Files, func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(tw, "NAME\tTYPE\tSIZE\tMODE\tMODIFIED")
			for _, f := range resp.Msg.Files {
				fileType := "FILE"
				if f.IsDir {
					fileType = "DIR"
				}
				modTime := time.Unix(f.ModifiedAtUnix, 0).Format(time.RFC3339)
				modeStr := fmt.Sprintf("%#o", f.Mode)
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%d B\t%s\t%s\n", f.Name, fileType, f.Size, modeStr, modTime)
			}
			return tw.Flush()
		})

	case "stat":
		if len(args) < 3 {
			return errors.New("usage: cloudctl files stat <workload-id> <path>")
		}
		resp, err := c.Client.File.StatFile(ctx, connect.NewRequest(&v1.StatFileRequest{
			WorkloadId: args[1],
			Path:       args[2],
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Info, func(w io.Writer) error {
			f := resp.Msg.Info
			if f == nil {
				_, _ = fmt.Fprintln(w, "not found")
				return nil
			}
			fileType := "FILE"
			if f.IsDir {
				fileType = "DIR"
			}
			modTime := time.Unix(f.ModifiedAtUnix, 0).Format(time.RFC3339)
			_, _ = fmt.Fprintf(w, "Name:     %s\n", f.Name)
			_, _ = fmt.Fprintf(w, "Path:     %s\n", f.Path)
			_, _ = fmt.Fprintf(w, "Type:     %s\n", fileType)
			_, _ = fmt.Fprintf(w, "Size:     %d bytes\n", f.Size)
			_, _ = fmt.Fprintf(w, "Mode:     %#o\n", f.Mode)
			_, _ = fmt.Fprintf(w, "Modified: %s\n", modTime)
			return nil
		})

	case "cat":
		if len(args) < 3 {
			return errors.New("usage: cloudctl files cat <workload-id> <path>")
		}
		stream, err := c.Client.File.ReadFile(ctx, connect.NewRequest(&v1.ReadFileRequest{
			WorkloadId: args[1],
			Path:       args[2],
		}))
		if err != nil {
			return err
		}
		for stream.Receive() {
			chunk := stream.Msg().Chunk
			if len(chunk) > 0 {
				if _, err := c.Stdout.Write(chunk); err != nil {
					return err
				}
			}
		}
		return stream.Err()

	case "put":
		if len(args) < 4 {
			return errors.New("usage: cloudctl files put <workload-id> <local-file> <remote-path>")
		}
		workloadID := args[1]
		localPath := args[2]
		remotePath := args[3]

		f, err := os.Open(localPath)
		if err != nil {
			return fmt.Errorf("open local file: %w", err)
		}
		defer f.Close()

		st, err := f.Stat()
		if err != nil {
			return fmt.Errorf("stat local file: %w", err)
		}
		totalSize := st.Size()
		mode := uint32(st.Mode().Perm())

		stream := c.Client.File.WriteFile(ctx)
		buf := make([]byte, 64*1024)
		var readBytes int64

		if totalSize == 0 {
			if err := stream.Send(&v1.WriteFileRequest{
				WorkloadId: workloadID,
				Path:       remotePath,
				Chunk:      []byte{},
				IsLast:     true,
				Mode:       mode,
			}); err != nil {
				return fmt.Errorf("send empty file chunk: %w", err)
			}
		} else {
			for {
				n, rErr := f.Read(buf)
				if n > 0 {
					readBytes += int64(n)
					isLast := (rErr == io.EOF) || (readBytes >= totalSize)
					chunkCopy := make([]byte, n)
					copy(chunkCopy, buf[:n])
					if err := stream.Send(&v1.WriteFileRequest{
						WorkloadId: workloadID,
						Path:       remotePath,
						Chunk:      chunkCopy,
						IsLast:     isLast,
						Mode:       mode,
					}); err != nil {
						return fmt.Errorf("send file chunk: %w", err)
					}
				}
				if rErr != nil {
					if errors.Is(rErr, io.EOF) {
						break
					}
					return fmt.Errorf("read local file: %w", rErr)
				}
			}
		}

		resp, err := stream.CloseAndReceive()
		if err != nil {
			return fmt.Errorf("write file failed: %w", err)
		}
		_, _ = fmt.Fprintf(c.Stdout, "Uploaded %d bytes to %s\n", resp.Msg.BytesWritten, resp.Msg.Path)
		return nil

	case "rename", "mv":
		if len(args) < 4 {
			return errors.New("usage: cloudctl files rename <workload-id> <old-path> <new-path>")
		}
		workloadID := args[1]
		oldPath := args[2]
		newPath := args[3]
		_, err := c.Client.File.RenameFile(ctx, connect.NewRequest(&v1.RenameFileRequest{
			WorkloadId: workloadID,
			OldPath:    oldPath,
			NewPath:    newPath,
		}))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(c.Stdout, "Renamed %s -> %s on workload %s\n", oldPath, newPath, workloadID)
		return nil

	case "rm":
		fs := flag.NewFlagSet("files rm", flag.ContinueOnError)
		recursive := fs.Bool("r", false, "Remove recursively")
		recursiveLong := fs.Bool("recursive", false, "Remove recursively")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		parsedArgs := fs.Args()
		if len(parsedArgs) < 2 {
			return errors.New("usage: cloudctl files rm [-r] <workload-id> <path>")
		}
		workloadID := parsedArgs[0]
		path := parsedArgs[1]
		rec := *recursive || *recursiveLong

		_, err := c.Client.File.DeleteFile(ctx, connect.NewRequest(&v1.DeleteFileRequest{
			WorkloadId: workloadID,
			Path:       path,
			Recursive:  rec,
		}))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(c.Stdout, "Deleted %s on workload %s\n", path, workloadID)
		return nil

	case "mkdir":
		if len(args) < 3 {
			return errors.New("usage: cloudctl files mkdir <workload-id> <path>")
		}
		workloadID := args[1]
		path := args[2]

		_, err := c.Client.File.CreateDirectory(ctx, connect.NewRequest(&v1.CreateDirectoryRequest{
			WorkloadId: workloadID,
			Path:       path,
		}))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(c.Stdout, "Created directory %s on workload %s\n", path, workloadID)
		return nil

	default:
		return fmt.Errorf("unknown files command: %s (usage: list, cat, put, rm, mkdir, stat)", args[0])
	}
}

func maskKey(k string) string {
	if len(k) <= 8 {
		return "***"
	}
	return k[:4] + "..." + k[len(k)-4:]
}

func (c *CLI) runBlueprints(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: cloudctl blueprints <list|get|create|delete>")
	}

	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("blueprints list", flag.ContinueOnError)
		loader := fs.String("loader", "", "Filter by server loader (e.g. paper, fabric, vanilla)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		resp, err := c.Client.Blueprint.ListBlueprints(ctx, connect.NewRequest(&v1.ListBlueprintsRequest{
			Loader: *loader,
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Blueprints, func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "ID\tNAME\tLOADER\tVERSION\tMEMORY\tCPU\tBUILTIN")
			for _, bp := range resp.Msg.Blueprints {
				bStr := "no"
				if bp.Builtin {
					bStr = "yes"
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%dM\t%dm\t%s\n",
					bp.Id, bp.Name, bp.Loader, bp.MinecraftVersion, bp.DefaultMemoryMb, bp.DefaultCpuMillicores, bStr)
			}
			return tw.Flush()
		})

	case "get":
		if len(args) < 2 {
			return errors.New("usage: cloudctl blueprints get <id>")
		}
		id := args[1]
		resp, err := c.Client.Blueprint.GetBlueprint(ctx, connect.NewRequest(&v1.GetBlueprintRequest{Id: id}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Blueprint, func(w io.Writer) error {
			bp := resp.Msg.Blueprint
			_, _ = fmt.Fprintf(w, "Blueprint: %s\n", bp.Id)
			_, _ = fmt.Fprintf(w, "  Name:            %s\n", bp.Name)
			_, _ = fmt.Fprintf(w, "  Description:     %s\n", bp.Description)
			_, _ = fmt.Fprintf(w, "  Loader:          %s\n", bp.Loader)
			_, _ = fmt.Fprintf(w, "  MC Version:      %s\n", bp.MinecraftVersion)
			_, _ = fmt.Fprintf(w, "  Docker Image:    %s\n", bp.DockerImage)
			_, _ = fmt.Fprintf(w, "  Default Memory:  %d MB\n", bp.DefaultMemoryMb)
			_, _ = fmt.Fprintf(w, "  Default CPU:     %d millicores\n", bp.DefaultCpuMillicores)
			bStr := "no"
			if bp.Builtin {
				bStr = "yes"
			}
			_, _ = fmt.Fprintf(w, "  Builtin:         %s\n", bStr)
			if bp.OrgId != "" {
				_, _ = fmt.Fprintf(w, "  Org ID:          %s\n", bp.OrgId)
			}
			if len(bp.DefaultEnv) > 0 {
				_, _ = fmt.Fprintf(w, "  Default Env:\n")
				for k, v := range bp.DefaultEnv {
					_, _ = fmt.Fprintf(w, "    %s: %s\n", k, v)
				}
			}
			if len(bp.DefaultJvmFlags) > 0 {
				_, _ = fmt.Fprintf(w, "  Default JVM Flags: %s\n", strings.Join(bp.DefaultJvmFlags, " "))
			}
			return nil
		})

	case "create":
		fs := flag.NewFlagSet("blueprints create", flag.ContinueOnError)
		name := fs.String("name", "", "Blueprint display name (required)")
		desc := fs.String("desc", "", "Description (optional)")
		loader := fs.String("loader", "paper", "Server loader (e.g. paper, fabric, forge)")
		version := fs.String("version", "1.21.4", "Target Minecraft version")
		image := fs.String("image", "itzg/minecraft-server:latest", "Docker container image")
		mem := fs.Int64("memory", 4096, "Default memory in MB")
		cpu := fs.Int64("cpu", 2000, "Default CPU in millicores")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" {
			return errors.New("flag -name is required")
		}
		resp, err := c.Client.Blueprint.CreateBlueprint(ctx, connect.NewRequest(&v1.CreateBlueprintRequest{
			Name:                 *name,
			Description:          *desc,
			Loader:               *loader,
			MinecraftVersion:     *version,
			DockerImage:          *image,
			DefaultMemoryMb:      *mem,
			DefaultCpuMillicores: *cpu,
		}))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(c.Stdout, "Blueprint created: ID %s (%s, loader: %s)\n", resp.Msg.Blueprint.Id, resp.Msg.Blueprint.Name, resp.Msg.Blueprint.Loader)
		return nil

	case "delete":
		if len(args) < 2 {
			return errors.New("usage: cloudctl blueprints delete <id>")
		}
		id := args[1]
		_, err := c.Client.Blueprint.DeleteBlueprint(ctx, connect.NewRequest(&v1.DeleteBlueprintRequest{Id: id}))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(c.Stdout, "Blueprint %s deleted\n", id)
		return nil

	default:		return fmt.Errorf("unknown blueprints command: %s (usage: list, get, create, delete)", args[0])
	}
}

func (c *CLI) runAddons(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: cloudctl addons <search|info|list|install|enable|disable|uninstall>")
	}
	switch args[0] {
	case "search":
		if len(args) < 2 {
			return errors.New("usage: cloudctl addons search <query> [-type plugin|mod] [-loader <loader>] [-version <mc-version>] [-limit <n>]")
		}
		query := args[1]
		fs := flag.NewFlagSet("addons search", flag.ContinueOnError)
		aTypeStr := fs.String("type", "", "Addon type: plugin or mod")
		loader := fs.String("loader", "", "Mod loader: paper, fabric, forge, velocity...")
		mcVer := fs.String("version", "", "Minecraft version: e.g. 1.21.4")
		limit := fs.Int("limit", 10, "Max search results")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		aType := v1.AddonType_ADDON_TYPE_UNSPECIFIED
		if strings.EqualFold(*aTypeStr, "plugin") {
			aType = v1.AddonType_ADDON_TYPE_PLUGIN
		} else if strings.EqualFold(*aTypeStr, "mod") {
			aType = v1.AddonType_ADDON_TYPE_MOD
		}
		resp, err := c.Client.Addon.SearchAddons(ctx, connect.NewRequest(&v1.SearchAddonsRequest{
			Query:       query,
			AddonType:   aType,
			Loader:      *loader,
			GameVersion: *mcVer,
			Limit:       int32(*limit),
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Hits, func(w io.Writer) error {
			if len(resp.Msg.Hits) == 0 {
				_, _ = fmt.Fprintln(w, "No addons found matching query.")
				return nil
			}
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "ID/SLUG	TITLE	TYPE	DOWNLOADS	LATEST	AUTHOR	DESCRIPTION")
			for _, hit := range resp.Msg.Hits {
				typeStr := "plugin"
				if hit.AddonType == v1.AddonType_ADDON_TYPE_MOD {
					typeStr = "mod"
				}
				desc := hit.Description
				if len(desc) > 50 {
					desc = desc[:47] + "..."
				}
				_, _ = fmt.Fprintf(tw, "%s	%s	%s	%d	%s	%s	%s\n",
					hit.Slug, hit.Title, typeStr, hit.Downloads, hit.LatestVersion, hit.Author, desc)
			}
			return tw.Flush()
		})

	case "info":
		if len(args) < 2 {
			return errors.New("usage: cloudctl addons info <project-id-or-slug> [-loader <loader>] [-version <mc-version>]")
		}
		projectID := args[1]
		fs := flag.NewFlagSet("addons info", flag.ContinueOnError)
		loader := fs.String("loader", "", "Mod loader filter")
		mcVer := fs.String("version", "", "Minecraft version filter")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		resp, err := c.Client.Addon.GetAddonDetails(ctx, connect.NewRequest(&v1.GetAddonDetailsRequest{
			ProjectIdOrSlug: projectID,
			Loader:          *loader,
			GameVersion:     *mcVer,
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Details, func(w io.Writer) error {
			d := resp.Msg.Details
			typeStr := "plugin"
			if d.AddonType == v1.AddonType_ADDON_TYPE_MOD {
				typeStr = "mod"
			}
			_, _ = fmt.Fprintf(w, "Addon: %s (%s)\n", d.Title, d.ProjectId)
			_, _ = fmt.Fprintf(w, "  Type:        %s\n", typeStr)
			_, _ = fmt.Fprintf(w, "  Slug:        %s\n", d.Slug)
			_, _ = fmt.Fprintf(w, "  Downloads:   %d\n", d.Downloads)
			_, _ = fmt.Fprintf(w, "  Description: %s\n", d.Description)
			if len(d.Categories) > 0 {
				_, _ = fmt.Fprintf(w, "  Categories:  %s\n", strings.Join(d.Categories, ", "))
			}
			if len(d.Loaders) > 0 {
				_, _ = fmt.Fprintf(w, "  Loaders:     %s\n", strings.Join(d.Loaders, ", "))
			}
			if len(resp.Msg.Versions) > 0 {
				_, _ = fmt.Fprintf(w, "\nCompatible Versions (%d):\n", len(resp.Msg.Versions))
				for i, v := range resp.Msg.Versions {
					if i >= 5 {
						_, _ = fmt.Fprintf(w, "  ... and %d more versions\n", len(resp.Msg.Versions)-5)
						break
					}
					var files []string
					for _, f := range v.Files {
						files = append(files, fmt.Sprintf("%s (%.1f KB)", f.Filename, float64(f.Size)/1024.0))
					}
					_, _ = fmt.Fprintf(w, "  - [%s] %s (%s) -> %s\n", v.Id, v.VersionNumber, v.Name, strings.Join(files, ", "))
				}
			}
			return nil
		})

	case "list":
		return c.runWorkloadAddons(ctx, append([]string{"list"}, args[1:]...))
	case "install":
		return c.runWorkloadAddons(ctx, append([]string{"install"}, args[1:]...))
	case "enable":
		return c.runWorkloadAddons(ctx, append([]string{"enable"}, args[1:]...))
	case "disable":
		return c.runWorkloadAddons(ctx, append([]string{"disable"}, args[1:]...))
	case "uninstall", "delete", "rm":
		return c.runWorkloadAddons(ctx, append([]string{"uninstall"}, args[1:]...))

	default:		return fmt.Errorf("unknown addons command: %s", args[0])
	}
}

func (c *CLI) runWorkloadAddons(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: cloudctl workloads addons <list|install|enable|disable|uninstall>")
	}
	switch args[0] {
	case "list":
		if len(args) < 2 {
			return errors.New("usage: cloudctl workloads addons list <workload-id> [--type plugin|mod]")
		}
		workloadID := args[1]
		fs := flag.NewFlagSet("workloads addons list", flag.ContinueOnError)
		aTypeStr := fs.String("type", "", "Filter addon type: plugin or mod")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		aType := v1.AddonType_ADDON_TYPE_UNSPECIFIED
		if strings.EqualFold(*aTypeStr, "plugin") {
			aType = v1.AddonType_ADDON_TYPE_PLUGIN
		} else if strings.EqualFold(*aTypeStr, "mod") {
			aType = v1.AddonType_ADDON_TYPE_MOD
		}
		resp, err := c.Client.Addon.ListWorkloadAddons(ctx, connect.NewRequest(&v1.ListWorkloadAddonsRequest{
			WorkloadId: workloadID,
			AddonType:  aType,
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Addons, func(w io.Writer) error {
			if len(resp.Msg.Addons) == 0 {
				_, _ = fmt.Fprintln(w, "No addons installed.")
				return nil
			}
			tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(tw, "FILENAME	NAME	TYPE	ENABLED	SIZE	MODIFIED")
			for _, a := range resp.Msg.Addons {
				typeStr := "plugin"
				if a.AddonType == v1.AddonType_ADDON_TYPE_MOD {
					typeStr = "mod"
				}
				enStr := "yes"
				if !a.Enabled {
					enStr = "no (disabled)"
				}
				modTime := time.Unix(a.ModifiedAtUnix, 0).Format(time.RFC3339)
				sizeKB := float64(a.SizeBytes) / 1024.0
				_, _ = fmt.Fprintf(tw, "%s	%s	%s	%s	%.1f KB	%s\n", a.Filename, a.Name, typeStr, enStr, sizeKB, modTime)
			}
			return tw.Flush()
		})

	case "install":
		if len(args) < 2 {
			return errors.New("usage: cloudctl workloads addons install <workload-id> [-modrinth <project-id-or-slug>] [-version <version-id>] [-url <url>] [-type plugin|mod] [-name <filename>]")
		}
		workloadID := args[1]
		fs := flag.NewFlagSet("workloads addons install", flag.ContinueOnError)
		modrinthID := fs.String("modrinth", "", "Modrinth project ID or slug")
		versionID := fs.String("version", "", "Modrinth version ID (optional)")
		downloadURL := fs.String("url", "", "Direct download URL (optional)")
		aTypeStr := fs.String("type", "", "Addon type: plugin or mod (optional)")
		filename := fs.String("name", "", "Custom filename (optional)")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		aType := v1.AddonType_ADDON_TYPE_UNSPECIFIED
		if strings.EqualFold(*aTypeStr, "plugin") {
			aType = v1.AddonType_ADDON_TYPE_PLUGIN
		} else if strings.EqualFold(*aTypeStr, "mod") {
			aType = v1.AddonType_ADDON_TYPE_MOD
		}
		resp, err := c.Client.Addon.InstallAddon(ctx, connect.NewRequest(&v1.InstallAddonRequest{
			WorkloadId:        workloadID,
			ModrinthProjectId: *modrinthID,
			ModrinthVersionId: *versionID,
			DownloadUrl:       *downloadURL,
			AddonType:         aType,
			Filename:          *filename,
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Addon, func(w io.Writer) error {
			a := resp.Msg.Addon
			typeStr := "plugin"
			if a.AddonType == v1.AddonType_ADDON_TYPE_MOD {
				typeStr = "mod"
			}
			_, _ = fmt.Fprintf(w, "Successfully installed %s: %s (%.1f KB)\n", typeStr, a.Filename, float64(a.SizeBytes)/1024.0)
			return nil
		})

	case "enable":
		if len(args) < 3 {
			return errors.New("usage: cloudctl workloads addons enable <workload-id> <filename> [--type plugin|mod]")
		}
		workloadID := args[1]
		filename := args[2]
		fs := flag.NewFlagSet("workloads addons enable", flag.ContinueOnError)
		aTypeStr := fs.String("type", "", "Addon type: plugin or mod")
		if err := fs.Parse(args[3:]); err != nil {
			return err
		}
		aType := v1.AddonType_ADDON_TYPE_UNSPECIFIED
		if strings.EqualFold(*aTypeStr, "plugin") {
			aType = v1.AddonType_ADDON_TYPE_PLUGIN
		} else if strings.EqualFold(*aTypeStr, "mod") {
			aType = v1.AddonType_ADDON_TYPE_MOD
		}
		resp, err := c.Client.Addon.ToggleAddon(ctx, connect.NewRequest(&v1.ToggleAddonRequest{
			WorkloadId: workloadID,
			Filename:   filename,
			AddonType:  aType,
			Enable:     true,
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Addon, func(w io.Writer) error {
			_, _ = fmt.Fprintf(w, "Enabled addon: %s\n", resp.Msg.Addon.Filename)
			return nil
		})

	case "disable":
		if len(args) < 3 {
			return errors.New("usage: cloudctl workloads addons disable <workload-id> <filename> [--type plugin|mod]")
		}
		workloadID := args[1]
		filename := args[2]
		fs := flag.NewFlagSet("workloads addons disable", flag.ContinueOnError)
		aTypeStr := fs.String("type", "", "Addon type: plugin or mod")
		if err := fs.Parse(args[3:]); err != nil {
			return err
		}
		aType := v1.AddonType_ADDON_TYPE_UNSPECIFIED
		if strings.EqualFold(*aTypeStr, "plugin") {
			aType = v1.AddonType_ADDON_TYPE_PLUGIN
		} else if strings.EqualFold(*aTypeStr, "mod") {
			aType = v1.AddonType_ADDON_TYPE_MOD
		}
		resp, err := c.Client.Addon.ToggleAddon(ctx, connect.NewRequest(&v1.ToggleAddonRequest{
			WorkloadId: workloadID,
			Filename:   filename,
			AddonType:  aType,
			Enable:     false,
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Addon, func(w io.Writer) error {
			_, _ = fmt.Fprintf(w, "Disabled addon: %s\n", resp.Msg.Addon.Filename)
			return nil
		})

	case "uninstall", "delete", "rm":
		if len(args) < 3 {
			return errors.New("usage: cloudctl workloads addons uninstall <workload-id> <filename> [--type plugin|mod]")
		}
		workloadID := args[1]
		filename := args[2]
		fs := flag.NewFlagSet("workloads addons uninstall", flag.ContinueOnError)
		aTypeStr := fs.String("type", "", "Addon type: plugin or mod")
		if err := fs.Parse(args[3:]); err != nil {
			return err
		}
		aType := v1.AddonType_ADDON_TYPE_UNSPECIFIED
		if strings.EqualFold(*aTypeStr, "plugin") {
			aType = v1.AddonType_ADDON_TYPE_PLUGIN
		} else if strings.EqualFold(*aTypeStr, "mod") {
			aType = v1.AddonType_ADDON_TYPE_MOD
		}
		_, err := c.Client.Addon.UninstallAddon(ctx, connect.NewRequest(&v1.UninstallAddonRequest{
			WorkloadId: workloadID,
			Filename:   filename,
			AddonType:  aType,
		}))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(c.Stdout, "Uninstalled addon: %s\n", filename)
		return nil

	default:
		return fmt.Errorf("unknown workloads addons command: %s", args[0])
	}
}

func parseScheduleAction(s string) v1.ScheduleActionType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "command", "cmd":
		return v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_COMMAND
	case "restart":
		return v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_RESTART
	case "start":
		return v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_START
	case "stop":
		return v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_STOP
	case "backup":
		return v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_BACKUP
	default:
		return v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_UNSPECIFIED
	}
}

func scheduleActionString(a v1.ScheduleActionType) string {
	switch a {
	case v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_COMMAND:
		return "command"
	case v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_RESTART:
		return "restart"
	case v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_START:
		return "start"
	case v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_STOP:
		return "stop"
	case v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_BACKUP:
		return "backup"
	default:
		return "unknown"
	}
}

func (c *CLI) runSchedules(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: cloudctl schedules <list|get|create|update|delete|run|executions>")
	}

	switch args[0] {
	case "list":
		if len(args) < 2 {
			return errors.New("usage: cloudctl schedules list <workload-id>")
		}
		workloadID := args[1]
		resp, err := c.Client.Schedule.ListSchedules(ctx, connect.NewRequest(&v1.ListSchedulesRequest{
			WorkloadId: workloadID,
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Schedules, func(w io.Writer) error {
			if len(resp.Msg.Schedules) == 0 {
				_, _ = fmt.Fprintln(w, "No schedules found.")
				return nil
			}
			tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(tw, "ID	NAME	CRON	ACTION	ENABLED	NEXT RUN	LAST RUN")
			for _, s := range resp.Msg.Schedules {
				enStr := "yes"
				if !s.Enabled {
					enStr = "no"
				}
				nextStr := "-"
				if s.NextRunAtUnix > 0 {
					nextStr = time.Unix(s.NextRunAtUnix, 0).Format(time.RFC3339)
				}
				lastStr := "-"
				if s.LastRunAtUnix > 0 {
					lastStr = time.Unix(s.LastRunAtUnix, 0).Format(time.RFC3339)
				}
				_, _ = fmt.Fprintf(tw, "%s	%s	%s	%s	%s	%s	%s\n",
					s.Id, s.Name, s.CronExpression, scheduleActionString(s.ActionType), enStr, nextStr, lastStr)
			}
			return tw.Flush()
		})

	case "get":
		if len(args) < 2 {
			return errors.New("usage: cloudctl schedules get <schedule-id>")
		}
		schedID := args[1]
		resp, err := c.Client.Schedule.GetSchedule(ctx, connect.NewRequest(&v1.GetScheduleRequest{
			ScheduleId: schedID,
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Schedule, func(w io.Writer) error {
			s := resp.Msg.Schedule
			_, _ = fmt.Fprintf(w, "Schedule:    %s\n", s.Id)
			_, _ = fmt.Fprintf(w, "Name:        %s\n", s.Name)
			_, _ = fmt.Fprintf(w, "Workload:    %s\n", s.WorkloadId)
			_, _ = fmt.Fprintf(w, "Cron:        %s\n", s.CronExpression)
			_, _ = fmt.Fprintf(w, "Action:      %s\n", scheduleActionString(s.ActionType))
			if s.Payload != "" {
				_, _ = fmt.Fprintf(w, "Payload:     %s\n", s.Payload)
			}
			_, _ = fmt.Fprintf(w, "Enabled:     %v\n", s.Enabled)
			if s.NextRunAtUnix > 0 {
				_, _ = fmt.Fprintf(w, "Next Run:    %s\n", time.Unix(s.NextRunAtUnix, 0).Format(time.RFC3339))
			}
			if s.LastRunAtUnix > 0 {
				_, _ = fmt.Fprintf(w, "Last Run:    %s\n", time.Unix(s.LastRunAtUnix, 0).Format(time.RFC3339))
			}
			return nil
		})

	case "create":
		fs := flag.NewFlagSet("schedules create", flag.ContinueOnError)
		workloadID := fs.String("workload", "", "Workload ID (required)")
		name := fs.String("name", "", "Schedule name (required)")
		cronExpr := fs.String("cron", "", "Cron expression (e.g. '0 4 * * *' or '*/10 * * * *') (required)")
		actionStr := fs.String("action", "", "Action type: command, restart, start, stop, backup (required)")
		payload := fs.String("payload", "", "Payload / command string (optional)")
		enabled := fs.Bool("enabled", true, "Whether schedule is initially enabled")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *workloadID == "" || *name == "" || *cronExpr == "" || *actionStr == "" {
			return errors.New("flags -workload, -name, -cron, and -action are required")
		}
		act := parseScheduleAction(*actionStr)
		if act == v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_UNSPECIFIED {
			return fmt.Errorf("invalid action type %q (must be command, restart, start, stop, backup)", *actionStr)
		}
		resp, err := c.Client.Schedule.CreateSchedule(ctx, connect.NewRequest(&v1.CreateScheduleRequest{
			WorkloadId:     *workloadID,
			Name:           *name,
			CronExpression: *cronExpr,
			ActionType:     act,
			Payload:        *payload,
			Enabled:        *enabled,
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Schedule, func(w io.Writer) error {
			_, _ = fmt.Fprintf(w, "Created schedule %s (%s, next: %s)\n",
				resp.Msg.Schedule.Id, resp.Msg.Schedule.Name,
				time.Unix(resp.Msg.Schedule.NextRunAtUnix, 0).Format(time.RFC3339))
			return nil
		})

	case "update":
		if len(args) < 2 {
			return errors.New("usage: cloudctl schedules update <schedule-id> [-name <name>] [-cron <expr>] [-action <type>] [-payload <payload>] [-enabled=true|false]")
		}
		schedID := args[1]
		fs := flag.NewFlagSet("schedules update", flag.ContinueOnError)
		name := fs.String("name", "", "Schedule name")
		cronExpr := fs.String("cron", "", "Cron expression")
		actionStr := fs.String("action", "", "Action type")
		payload := fs.String("payload", "", "Action payload")
		enabled := fs.Bool("enabled", true, "Schedule enabled state")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		act := v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_UNSPECIFIED
		if *actionStr != "" {
			act = parseScheduleAction(*actionStr)
			if act == v1.ScheduleActionType_SCHEDULE_ACTION_TYPE_UNSPECIFIED {
				return fmt.Errorf("invalid action type %q", *actionStr)
			}
		}
		resp, err := c.Client.Schedule.UpdateSchedule(ctx, connect.NewRequest(&v1.UpdateScheduleRequest{
			ScheduleId:     schedID,
			Name:           *name,
			CronExpression: *cronExpr,
			ActionType:     act,
			Payload:        *payload,
			Enabled:        *enabled,
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Schedule, func(w io.Writer) error {
			_, _ = fmt.Fprintf(w, "Updated schedule %s (%s, enabled: %v)\n",
				resp.Msg.Schedule.Id, resp.Msg.Schedule.Name, resp.Msg.Schedule.Enabled)
			return nil
		})

	case "delete", "rm":
		if len(args) < 2 {
			return errors.New("usage: cloudctl schedules delete <schedule-id>")
		}
		schedID := args[1]
		_, err := c.Client.Schedule.DeleteSchedule(ctx, connect.NewRequest(&v1.DeleteScheduleRequest{
			ScheduleId: schedID,
		}))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(c.Stdout, "Deleted schedule: %s\n", schedID)
		return nil

	case "run":
		if len(args) < 2 {
			return errors.New("usage: cloudctl schedules run <schedule-id>")
		}
		schedID := args[1]
		resp, err := c.Client.Schedule.RunSchedule(ctx, connect.NewRequest(&v1.RunScheduleRequest{
			ScheduleId: schedID,
		}))
		if err != nil {
			return err
		}
		exec := resp.Msg.Execution
		return c.printOutput(exec, func(w io.Writer) error {
			statusStr := "SUCCESS"
			if exec.Status == v1.ScheduleExecutionStatus_SCHEDULE_EXECUTION_STATUS_FAILED {
				statusStr = "FAILED"
			}
			_, _ = fmt.Fprintf(w, "Triggered schedule %s -> Execution %s (%s in %dms)\n",
				schedID, exec.Id, statusStr, exec.DurationMs)
			if exec.Output != "" {
				_, _ = fmt.Fprintf(w, "Output: %s\n", exec.Output)
			}
			if exec.Error != "" {
				_, _ = fmt.Fprintf(w, "Error:  %s\n", exec.Error)
			}
			return nil
		})

	case "executions", "history", "logs":
		if len(args) < 2 {
			return errors.New("usage: cloudctl schedules executions <schedule-id> [-limit <n>]")
		}
		schedID := args[1]
		fs := flag.NewFlagSet("schedules executions", flag.ContinueOnError)
		limit := fs.Int("limit", 20, "Maximum executions to return")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		resp, err := c.Client.Schedule.ListScheduleExecutions(ctx, connect.NewRequest(&v1.ListScheduleExecutionsRequest{
			ScheduleId: schedID,
			Limit:      int32(*limit),
		}))
		if err != nil {
			return err
		}
		return c.printOutput(resp.Msg.Executions, func(w io.Writer) error {
			if len(resp.Msg.Executions) == 0 {
				_, _ = fmt.Fprintln(w, "No executions recorded for this schedule.")
				return nil
			}
			tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(tw, "ID	TRIGGER	STATUS	DURATION	STARTED AT	OUTPUT / ERROR")
			for _, e := range resp.Msg.Executions {
				statusStr := "RUNNING"
				switch e.Status {
				case v1.ScheduleExecutionStatus_SCHEDULE_EXECUTION_STATUS_SUCCESS:
					statusStr = "SUCCESS"
				case v1.ScheduleExecutionStatus_SCHEDULE_EXECUTION_STATUS_FAILED:
					statusStr = "FAILED"
				}
				details := e.Output
				if e.Error != "" {
					details = "ERROR: " + e.Error
				}
				if len(details) > 60 {
					details = details[:57] + "..."
				}
				startStr := time.Unix(e.StartedAtUnix, 0).Format(time.RFC3339)
				_, _ = fmt.Fprintf(tw, "%s	%s	%s	%dms	%s	%s\n",
					e.Id, e.TriggeredBy, statusStr, e.DurationMs, startStr, details)
			}
			return tw.Flush()
		})

	default:
		return fmt.Errorf("unknown schedules command: %s (usage: list, get, create, update, delete, run, executions)", args[0])
	}
}
