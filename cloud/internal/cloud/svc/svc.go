// Package svc implements the Connect-RPC services served by cloudcontrold.
//
// Every handler delegates to an existing domain package (node, provision,
// nodetype, provider, audit, db) and never reimplements domain logic. Where
// no domain logic exists yet (orgs, members, API keys, workloads, agent
// command fan-out) the handler works directly against db.Store with
// org scoping via Store.Org(ctx).
package svc

import (
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/audit"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/node"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/nodetype"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/provision"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Build metadata, assigned by cloudcontrold at startup (ldflags or direct
// assignment). Defaults keep local builds and tests working.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// Deps wires every Connect service to the domain layer. Provision may be nil
// in tests; provision RPCs then fail with CodeUnavailable.
type Deps struct {
	Store      *db.Store
	Nodes      *node.Service
	JoinTokens *node.JoinTokenService
	Catalog    *nodetype.Catalog
	Provision  *provision.Service
	Audits     audit.Store
	// ControlPlaneURL is advertised to agents (join command, endpoints).
	// Empty means join commands use a relative reference.
	ControlPlaneURL string
}

// Services holds one implementation per Connect service.
type Services struct {
	System    *SystemService
	Org       *OrgService
	Role      *RoleService
	NodeType  *NodeTypeService
	Node      *NodeService
	APIKey    *APIKeyService
	Session   *SessionService
	Audit     *AuditService
	Provision *ProvisionService
	Workload  *WorkloadService
	Agent     *AgentService
}

// New builds every service implementation from deps. It fails when required
// deps are missing so miswiring is a boot error, never a runtime nil panic.
func New(d Deps) (*Services, error) {
	if d.Store == nil {
		return nil, connect.NewError(connect.CodeInternal, errNoStore)
	}
	if d.Nodes == nil || d.JoinTokens == nil {
		return nil, connect.NewError(connect.CodeInternal, errNoNode)
	}
	if d.Catalog == nil {
		return nil, connect.NewError(connect.CodeInternal, errNoCatalog)
	}
	if d.Audits == nil {
		return nil, connect.NewError(connect.CodeInternal, errNoAudit)
	}
	return &Services{
		System:    &SystemService{deps: d},
		Org:       &OrgService{deps: d},
		Role:      &RoleService{deps: d},
		NodeType:  &NodeTypeService{deps: d},
		Node:      &NodeService{deps: d},
		APIKey:    &APIKeyService{deps: d},
		Session:   &SessionService{deps: d},
		Audit:     &AuditService{deps: d},
		Provision: &ProvisionService{deps: d},
		Workload:  &WorkloadService{deps: d},
		Agent:     &AgentService{deps: d},
	}, nil
}

// --- shared helpers ---

const maxPageSize = 100

// page parses a PageRequest into limit/offset. Tokens are decimal offsets.
func page(req *v1.PageRequest, def int) (limit, offset int) {
	limit = def
	if req != nil && req.PageSize > 0 {
		limit = int(req.PageSize)
	}
	if limit <= 0 {
		limit = def
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}
	if req != nil && req.PageToken != "" {
		if n, err := strconv.Atoi(req.PageToken); err == nil && n > 0 {
			offset = n
		}
	}
	return limit, offset
}

// pageResp builds the next-page envelope.
func pageResp(total int, limit, offset int) *v1.PageResponse {
	out := &v1.PageResponse{Total: int64(total)}
	if offset+limit < total {
		out.NextPageToken = strconv.Itoa(offset + limit)
	}
	return out
}

func ts(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func tsPtr(t *time.Time) *timestamppb.Timestamp {
	if t == nil || t.IsZero() {
		return nil
	}
	return timestamppb.New(*t)
}

func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r == ' ' || r == '_' || r == '.' || r == '-':
			return '-'
		default:
			return -1
		}
	}, s)
	s = strings.Trim(s, "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	if s == "" {
		s = "org"
	}
	return s
}

// roleToProto folds a control-plane role string into the proto enum.
// Unknown roles fail closed to ROLE_UNSPECIFIED.
func roleToProto(role string) v1.Role {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner":
		return v1.Role_ROLE_OWNER
	case "admin":
		return v1.Role_ROLE_ADMIN
	case "operator":
		return v1.Role_ROLE_OPERATOR
	case "viewer":
		return v1.Role_ROLE_VIEWER
	case "billing":
		return v1.Role_ROLE_BILLING
	default:
		return v1.Role_ROLE_UNSPECIFIED
	}
}

func roleToString(r v1.Role) string {
	switch r {
	case v1.Role_ROLE_OWNER:
		return "owner"
	case v1.Role_ROLE_ADMIN:
		return "admin"
	case v1.Role_ROLE_OPERATOR:
		return "operator"
	case v1.Role_ROLE_VIEWER:
		return "viewer"
	case v1.Role_ROLE_BILLING:
		return "billing"
	default:
		return ""
	}
}

func providerToProto(name string) v1.ProviderId {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "generic":
		return v1.ProviderId_PROVIDER_ID_GENERIC
	case "hetzner":
		return v1.ProviderId_PROVIDER_ID_HETZNER
	case "aws":
		return v1.ProviderId_PROVIDER_ID_AWS
	case "gcp":
		return v1.ProviderId_PROVIDER_ID_GCP
	case "digitalocean":
		return v1.ProviderId_PROVIDER_ID_DIGITALOCEAN
	case "proxmox":
		return v1.ProviderId_PROVIDER_ID_PROXMOX
	default:
		return v1.ProviderId_PROVIDER_ID_UNSPECIFIED
	}
}

func providerToString(p v1.ProviderId) string {
	switch p {
	case v1.ProviderId_PROVIDER_ID_GENERIC:
		return "generic"
	case v1.ProviderId_PROVIDER_ID_HETZNER:
		return "hetzner"
	case v1.ProviderId_PROVIDER_ID_AWS:
		return "aws"
	case v1.ProviderId_PROVIDER_ID_GCP:
		return "gcp"
	case v1.ProviderId_PROVIDER_ID_DIGITALOCEAN:
		return "digitalocean"
	case v1.ProviderId_PROVIDER_ID_PROXMOX:
		return "proxmox"
	default:
		return ""
	}
}

func provisionStatusToProto(s string) v1.ProvisionStatus {	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pending":
		return v1.ProvisionStatus_PROVISION_STATUS_PENDING
	case "planned":
		return v1.ProvisionStatus_PROVISION_STATUS_PLANNED
	case "applying":
		return v1.ProvisionStatus_PROVISION_STATUS_APPLYING
	case "applied":
		return v1.ProvisionStatus_PROVISION_STATUS_APPLIED
	case "failed":
		return v1.ProvisionStatus_PROVISION_STATUS_FAILED
	case "destroyed":
		return v1.ProvisionStatus_PROVISION_STATUS_DESTROYED
	default:
		return v1.ProvisionStatus_PROVISION_STATUS_UNSPECIFIED
	}
}

func workloadStatusToProto(s string) v1.WorkloadStatus {	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pending", "starting", "creating":
		return v1.WorkloadStatus_WORKLOAD_STATUS_PENDING
	case "running":
		return v1.WorkloadStatus_WORKLOAD_STATUS_RUNNING
	case "stopped", "stopping", "deleted", "deleting":
		return v1.WorkloadStatus_WORKLOAD_STATUS_STOPPED
	case "error", "failed":
		return v1.WorkloadStatus_WORKLOAD_STATUS_ERROR
	default:
		return v1.WorkloadStatus_WORKLOAD_STATUS_UNSPECIFIED
	}
}

func originToProto(origin string) v1.NodeOrigin {
	switch strings.ToLower(strings.TrimSpace(origin)) {
	case "byo":
		return v1.NodeOrigin_NODE_ORIGIN_BYO
	case "managed":
		return v1.NodeOrigin_NODE_ORIGIN_MANAGED
	default:
		return v1.NodeOrigin_NODE_ORIGIN_UNSPECIFIED
	}
}

func originToString(o v1.NodeOrigin) string {	switch o {
	case v1.NodeOrigin_NODE_ORIGIN_BYO:
		return "byo"
	case v1.NodeOrigin_NODE_ORIGIN_MANAGED:
		return "managed"
	default:
		return ""
	}
}

func provisionStatusToString(s v1.ProvisionStatus) string {
	switch s {
	case v1.ProvisionStatus_PROVISION_STATUS_PENDING:
		return "pending"
	case v1.ProvisionStatus_PROVISION_STATUS_PLANNED:
		return "planned"
	case v1.ProvisionStatus_PROVISION_STATUS_APPLYING:
		return "applying"
	case v1.ProvisionStatus_PROVISION_STATUS_APPLIED:
		return "applied"
	case v1.ProvisionStatus_PROVISION_STATUS_FAILED:
		return "failed"
	case v1.ProvisionStatus_PROVISION_STATUS_DESTROYED:
		return "destroyed"
	default:
		return ""
	}
}

func workloadStatusToString(s v1.WorkloadStatus) string {
	switch s {
	case v1.WorkloadStatus_WORKLOAD_STATUS_PENDING:
		return "pending"
	case v1.WorkloadStatus_WORKLOAD_STATUS_RUNNING:
		return "running"
	case v1.WorkloadStatus_WORKLOAD_STATUS_STOPPED:
		return "stopped"
	case v1.WorkloadStatus_WORKLOAD_STATUS_ERROR:
		return "error"
	default:
		return ""
	}
}

func nodeStatusToProto(s string) v1.NodeStatus {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pending":
		return v1.NodeStatus_NODE_STATUS_PENDING
	case "active", "online":
		return v1.NodeStatus_NODE_STATUS_ONLINE
	case "offline":
		return v1.NodeStatus_NODE_STATUS_OFFLINE
	case "draining":
		return v1.NodeStatus_NODE_STATUS_DRAINING
	case "error", "failed":
		return v1.NodeStatus_NODE_STATUS_ERROR
	default:
		return v1.NodeStatus_NODE_STATUS_UNSPECIFIED
	}
}

func nodeStatusToString(s v1.NodeStatus) string {
	switch s {
	case v1.NodeStatus_NODE_STATUS_PENDING:
		return "pending"
	case v1.NodeStatus_NODE_STATUS_ONLINE:
		return "active"
	case v1.NodeStatus_NODE_STATUS_OFFLINE:
		return "offline"
	case v1.NodeStatus_NODE_STATUS_DRAINING:
		return "draining"
	case v1.NodeStatus_NODE_STATUS_ERROR:
		return "error"
	default:
		return ""
	}
}
