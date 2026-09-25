package db

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func newID() string { return uuid.NewString() }

// TenantBase carries the columns every tenant-owned table shares.
type TenantBase struct {
	ID        string `gorm:"primaryKey;size:36"`
	OrgID     string `gorm:"size:36;not null;index"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsTenantOwned marks tenant-owned models. A reflection test enforces that
// every model with an OrgID field implements this and vice versa.
func (TenantBase) IsTenantOwned() bool { return true }

// decodeJSON is the shared helper behind the typed JSON accessors.
func decodeJSON[T any](raw string) (out T) {
	if raw == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func encodeJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// Org is a tenant.
type Org struct {
	ID         string  `gorm:"primaryKey;size:36"`
	Name       string  `gorm:"not null"`
	Slug       string  `gorm:"uniqueIndex;not null"`
	ClerkOrgID *string `gorm:"uniqueIndex;size:64"`
	Plan       string  `gorm:"not null;default:''"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (Org) TableName() string              { return "orgs" }
func (Org) IsTenantOwned() bool            { return true }
func (o *Org) BeforeCreate(*gorm.DB) error { o.setID(); return nil }
func (o *Org) setID() {
	if o.ID == "" {
		o.ID = newID()
	}
}

// Member links a user to an org with a role and lifecycle status.
type Member struct {
	TenantBase
	UserID      string `gorm:"size:64;not null;index"`
	Email       string `gorm:"not null;default:''"`
	DisplayName string `gorm:"not null;default:''"`
	Role        string `gorm:"not null;default:''"`
	Status      string `gorm:"not null;default:''"`
}

func (Member) TableName() string { return "members" }

// ApiKey is an org-scoped long-lived credential. Only the hash is stored.
type ApiKey struct {
	TenantBase
	Name        string `gorm:"not null"`
	Prefix      string `gorm:"index;not null;default:''"`
	Hash        string `gorm:"uniqueIndex;not null"`
	Permissions string `gorm:"type:text;not null;default:''"`
	CreatedBy   string `gorm:"not null;default:''"`
	LastUsedAt  *time.Time
	ExpiresAt   *time.Time
	RevokedAt   *time.Time
}

func (ApiKey) TableName() string { return "api_keys" }

// PermissionList returns the decoded permissions JSON.
func (k *ApiKey) PermissionList() []string { return decodeJSON[[]string](k.Permissions) }

// SetPermissions encodes permissions into the JSON column.
func (k *ApiKey) SetPermissions(perms []string) { k.Permissions = encodeJSON(perms) }

// JoinToken is a single-use org-bound node onboarding secret. Only the hash
// of the presented secret is stored.
type JoinToken struct {
	TenantBase
	Name         string `gorm:"not null"`
	NodeTypeID   string `gorm:"not null;default:''"`
	Origin       string `gorm:"not null;default:''"`
	ProvisionID  string `gorm:"size:36;not null;default:''"`
	SecretHash   string `gorm:"uniqueIndex;not null"`
	ExpiresAt    *time.Time
	UsedAt       *time.Time
	UsedByNodeID string `gorm:"size:36;not null;default:''"`
	RevokedAt    *time.Time
}

func (JoinToken) TableName() string { return "join_tokens" }

// NodeCapacity is the resource envelope reported by / declared for a node.
type NodeCapacity struct {
	VCPU   int `json:"vcpu"`
	RAMMB  int `json:"ram_mb"`
	DiskGB int `json:"disk_gb"`
}

// Node is one Docker host registered to an org, BYO or managed.
type Node struct {
	TenantBase
	Name             string  `gorm:"not null"`
	Origin           string  `gorm:"not null;default:''"`
	Provider         string  `gorm:"not null;default:''"`
	NodeTypeID       string  `gorm:"not null;default:''"`
	Region           string  `gorm:"not null;default:''"`
	Status           string  `gorm:"not null;default:''"`
	Hostname         string  `gorm:"not null;default:''"`
	PublicIP         string  `gorm:"not null;default:''"`
	PrivateIP        string  `gorm:"not null;default:''"`
	Capacity         string  `gorm:"type:text;not null;default:''"`
	Labels           string  `gorm:"type:text;not null;default:''"`
	AgentFingerprint *string `gorm:"uniqueIndex;size:128"`
	ProvisionID      string  `gorm:"size:36;not null;default:''"`
	IsSystem         bool    `gorm:"not null;default:false"`
	Draining         bool    `gorm:"not null;default:false"`
	LastHeartbeat    *time.Time
}

func (Node) TableName() string { return "nodes" }

// CapacityDecoded returns the decoded capacity JSON.
func (n *Node) CapacityDecoded() NodeCapacity { return decodeJSON[NodeCapacity](n.Capacity) }

// SetCapacity encodes the capacity envelope.
func (n *Node) SetCapacity(c NodeCapacity) { n.Capacity = encodeJSON(c) }

// LabelMap returns the decoded labels JSON.
func (n *Node) LabelMap() map[string]string { return decodeJSON[map[string]string](n.Labels) }

// SetLabels encodes labels.
func (n *Node) SetLabels(l map[string]string) { n.Labels = encodeJSON(l) }

// NodeType is the global node-type catalog. It is NOT tenant-owned.
type NodeType struct {
	ID              string  `gorm:"primaryKey"`
	Name            string  `gorm:"not null"`
	VCPU            int     `gorm:"not null;default:0"`
	RAMMB           int     `gorm:"not null;default:0"`
	DiskGB          int     `gorm:"not null;default:0"`
	MonthlyPriceUSD float64 `gorm:"not null;default:0"`
	Description     string  `gorm:"not null;default:''"`
	SortOrder       int     `gorm:"not null;default:0"`
	Enabled         bool    `gorm:"not null;default:true"`
	InstanceTypes   string  `gorm:"type:text;not null;default:''"`
}

func (NodeType) TableName() string { return "node_types" }

// InstanceTypeMap returns the per-provider instance-type mapping.
func (t *NodeType) InstanceTypeMap() map[string]string {
	return decodeJSON[map[string]string](t.InstanceTypes)
}

// SetInstanceTypes encodes the per-provider instance-type mapping.
func (t *NodeType) SetInstanceTypes(m map[string]string) { t.InstanceTypes = encodeJSON(m) }

// Provision tracks one Terraform-provisioned managed node lifecycle.
type Provision struct {
	TenantBase
	Name        string `gorm:"not null"`
	Provider    string `gorm:"not null;default:''"`
	Region      string `gorm:"not null;default:''"`
	NodeTypeID  string `gorm:"not null;default:''"`
	Status      string `gorm:"not null;default:''"`
	Workspace   string `gorm:"uniqueIndex;not null"`
	PlanSummary string `gorm:"type:text;not null;default:''"`
	PlanDiff    string `gorm:"type:text;not null;default:''"`
	PlanHash    string `gorm:"not null;default:''"`
	Outputs     string `gorm:"type:text;not null;default:''"`
	Error       string `gorm:"type:text;not null;default:''"`
	NodeID      string `gorm:"size:36;not null;default:''"`
	CreatedBy   string `gorm:"not null;default:''"`
}

func (Provision) TableName() string { return "provisions" }

// OutputMap returns the decoded Terraform outputs JSON.
func (p *Provision) OutputMap() map[string]string {
	return decodeJSON[map[string]string](p.Outputs)
}

// SetOutputs encodes Terraform outputs.
func (p *Provision) SetOutputs(m map[string]string) { p.Outputs = encodeJSON(m) }

// Workload is one container scheduled onto a node for an org.
type Workload struct {
	TenantBase
	NodeID       string `gorm:"size:36;index;not null;default:''"`
	Name         string `gorm:"not null"`
	Spec         string `gorm:"type:text;not null;default:''"`
	Status       string `gorm:"not null;default:''"`
	ContainerID  string `gorm:"not null;default:''"`
	HostPort     int    `gorm:"not null;default:0"`
	Hostname     string `gorm:"not null;default:''"`
	StatusDetail string `gorm:"type:text;not null;default:''"`
	CreatedBy    string `gorm:"not null;default:''"`
}

func (Workload) TableName() string { return "workloads" }

// SpecMap returns the decoded workload spec JSON.
func (w *Workload) SpecMap() map[string]any { return decodeJSON[map[string]any](w.Spec) }

// SetSpec encodes the workload spec.
func (w *Workload) SetSpec(m map[string]any) { w.Spec = encodeJSON(m) }

// WorkloadEvent is an append-only status breadcrumb for a workload.
type WorkloadEvent struct {
	TenantBase
	WorkloadID string `gorm:"size:36;index;not null"`
	Kind       string `gorm:"not null;default:''"`
	Message    string `gorm:"type:text;not null;default:''"`
}

func (WorkloadEvent) TableName() string { return "workload_events" }

// AuditEvent is an append-only audit record. Written by the audit lane.
type AuditEvent struct {
	ID            string    `gorm:"primaryKey;size:36"`
	OrgID         string    `gorm:"size:36;not null;index"`
	ActorUserID   string    `gorm:"not null;default:''"`
	ActorAPIKeyID string    `gorm:"size:36;not null;default:''"`
	Action        string    `gorm:"not null"`
	ResourceType  string    `gorm:"not null;default:''"`
	ResourceID    string    `gorm:"not null;default:''"`
	Result        string    `gorm:"not null;default:''"`
	DetailJSON    string    `gorm:"type:text;not null;default:''"`
	IP            string    `gorm:"not null;default:''"`
	UserAgent     string    `gorm:"not null;default:''"`
	CreatedAt     time.Time `gorm:"index"`
}

func (AuditEvent) TableName() string   { return "audit_events" }
func (AuditEvent) IsTenantOwned() bool { return true }

// RoleBinding grants permissions on a resource to a subject.
type RoleBinding struct {
	TenantBase
	SubjectType  string `gorm:"not null"`
	SubjectID    string `gorm:"not null"`
	ResourceType string `gorm:"not null;default:''"`
	ResourceID   string `gorm:"not null;default:''"`
	Permissions  string `gorm:"type:text;not null;default:''"`
}

func (RoleBinding) TableName() string { return "role_bindings" }

// PermissionList returns the decoded permissions JSON.
func (r *RoleBinding) PermissionList() []string { return decodeJSON[[]string](r.Permissions) }

// SetPermissions encodes permissions into the JSON column.
func (r *RoleBinding) SetPermissions(perms []string) { r.Permissions = encodeJSON(perms) }

// WorkloadBackup represents an atomic snapshot archive of a workload's persistent files.
type WorkloadBackup struct {
	TenantBase
	WorkloadID string `gorm:"size:36;index;not null"`
	NodeID     string `gorm:"size:36;not null;default:''"`
	Name       string `gorm:"size:128;not null;default:''"`
	SizeBytes  int64  `gorm:"not null;default:0"`
	SHA256     string `gorm:"size:64;not null;default:''"`
	Locked     bool   `gorm:"not null;default:false"`
	Status     string `gorm:"size:32;not null;default:'pending'"`
	Error      string `gorm:"type:text;not null;default:''"`
}

func (WorkloadBackup) TableName() string { return "workload_backups" }

// Blueprint is an organization-owned custom server blueprint/preset (MINE-162).
type Blueprint struct {
	TenantBase
	Name                 string `gorm:"not null"`
	Description          string `gorm:"default:''"`
	Loader               string `gorm:"not null"`
	MinecraftVersion     string `gorm:"not null"`
	DockerImage          string `gorm:"default:''"`
	DefaultMemoryMB      int64  `gorm:"not null;default:2048"`
	DefaultCPUMillicores int64  `gorm:"not null;default:1000"`
	DefaultEnv           string `gorm:"type:text;default:'{}'"` // JSON map[string]string
	DefaultJVMFlags      string `gorm:"type:text;default:'[]'"` // JSON []string
}

func (Blueprint) TableName() string              { return "blueprints" }
func (b *Blueprint) BeforeCreate(*gorm.DB) error { b.setID(); return nil }
func (b *Blueprint) setID() {
	if b.ID == "" {
		b.ID = newID()
	}
}


// WorkloadSchedule represents a recurring cron task for a workload (MINE-164).
type WorkloadSchedule struct {
	TenantBase
	WorkloadID     string     `gorm:"size:36;index;not null"`
	Name           string     `gorm:"size:128;not null"`
	CronExpression string     `gorm:"size:64;not null"`
	ActionType     string     `gorm:"size:32;not null"` // command, restart, start, stop, backup
	Payload        string     `gorm:"type:text;not null;default:''"`
	Enabled        bool       `gorm:"not null;default:true"`
	LastRunAt      *time.Time `gorm:"index"`
	NextRunAt      *time.Time `gorm:"index"`
}

func (WorkloadSchedule) TableName() string { return "workload_schedules" }
func (s *WorkloadSchedule) BeforeCreate(*gorm.DB) error { s.setID(); return nil }
func (s *WorkloadSchedule) setID() {
	if s.ID == "" {
		s.ID = newID()
	}
}

// ScheduleExecution represents an execution record of a scheduled or manually triggered task (MINE-164).
type ScheduleExecution struct {
	ID          string     `gorm:"primaryKey;size:36"`
	OrgID       string     `gorm:"size:36;not null;index"`
	ScheduleID  string     `gorm:"size:36;not null;index"`
	WorkloadID  string     `gorm:"size:36;not null;index"`
	TriggeredBy string     `gorm:"size:32;not null;default:'cron'"` // "cron", "manual"
	Status      string     `gorm:"size:32;not null;default:'running'"` // "running", "success", "failed"
	Output      string     `gorm:"type:text;not null;default:''"`
	Error       string     `gorm:"type:text;not null;default:''"`
	DurationMs  int64      `gorm:"not null;default:0"`
	StartedAt   time.Time  `gorm:"index;not null"`
	FinishedAt  *time.Time `gorm:"index"`
}

func (ScheduleExecution) TableName() string { return "schedule_executions" }
func (ScheduleExecution) IsTenantOwned() bool { return true }
func (e *ScheduleExecution) BeforeCreate(*gorm.DB) error {
	if e.ID == "" {
		e.ID = newID()
	}
	return nil
}
