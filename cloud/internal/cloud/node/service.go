// Package node is the per-org node registry, join-token issuer, and
// capacity admission check over db.Node, db.JoinToken and db.Workload.
//
// Every method except join-token Redeem is org-scoped through
// Store.Org(ctx) and refuses with db.ErrNoOrg when the context carries no
// org. Redeem is the node-join path: the caller has no org yet, so the org
// is taken from the token itself and cross-checked when the context does
// carry one.
package node

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/secrets"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Typed errors. All denials are errors; success never returns one.
var (
	ErrNodeNotFound     = errors.New("node: not found")
	ErrNodeDraining     = errors.New("node: draining")
	ErrCapacityExceeded = errors.New("node: capacity exceeded")
)

// DefaultStaleAfter is the silence threshold after which a node reads back
// as "offline" without mutating its row. Overridable per Service via Deps.
const DefaultStaleAfter = 5 * time.Minute

// Deps wires a Service, JoinTokenService and Capacity to storage, the
// join-token HMAC pepper, and the clock.
type Deps struct {
	Store *db.Store
	// Secrets resolves keys.JoinTokenSecret. Optional when Pepper is set.
	Secrets secrets.Provider
	// Pepper overrides the secrets lookup (tests, offline use).
	Pepper []byte
	// Now overrides the clock. Nil means time.Now (UTC).
	Now func() time.Time
	// StaleAfter overrides DefaultStaleAfter. Non-positive means default.
	StaleAfter time.Duration
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}

func (d Deps) staleAfter() time.Duration {
	if d.StaleAfter > 0 {
		return d.StaleAfter
	}
	return DefaultStaleAfter
}

func newUUID() string { return uuid.NewString() }

func newFingerprint() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return uuid.NewString()
	}
	return hex.EncodeToString(b[:])
}

// Service is the node registry.
type Service struct {
	deps Deps
}

// scope returns a fresh org-scoped query. Scopes are single-use: a *gorm.DB
// that already ran a query must not be reused for another statement.
func (s *Service) scope(ctx context.Context) (*gorm.DB, error) {
	return s.deps.Store.Org(ctx)
}

// NewService returns a registry over deps. Deps.Store must be non-nil.
func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// RegisterRequest carries the fields for a new node row.
type RegisterRequest struct {
	Name       string
	Origin     string
	Provider   string
	NodeTypeID string
	Region     string
	Capacity   db.NodeCapacity
	Labels     map[string]string
}

// Register creates a node row, assigns an agent fingerprint and an initial
// heartbeat of now so the node reads back live.
func (s *Service) Register(ctx context.Context, req RegisterRequest) (db.Node, error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return db.Node{}, err
	}
	if req.Name == "" {
		return db.Node{}, errors.New("node: name required")
	}
	now := s.deps.now()
	fp := newFingerprint()
	n := db.Node{
		Name:          req.Name,
		Origin:        req.Origin,
		Provider:      req.Provider,
		NodeTypeID:    req.NodeTypeID,
		Region:        req.Region,
		Status:        "active",
		LastHeartbeat: &now,
	}
	n.TenantBase = db.TenantBase{ID: newUUID(), OrgID: principal.OrgID(ctx)}
	n.AgentFingerprint = &fp
	n.SetCapacity(req.Capacity)
	n.SetLabels(req.Labels)
	if err := q.Create(&n).Error; err != nil {
		return db.Node{}, err
	}
	return s.withEffective(n), nil
}

// Heartbeat marks the node live now, assigning a fingerprint when missing.
// If the node was offline, it reactivates it back to active status.
func (s *Service) Heartbeat(ctx context.Context, id string) (db.Node, error) {
	q, err := s.scope(ctx)
	if err != nil {
		return db.Node{}, err
	}
	var n db.Node
	if err := q.Where("id = ?", id).First(&n).Error; err != nil {
		return db.Node{}, ErrNodeNotFound
	}
	now := s.deps.now()
	patch := map[string]any{"last_heartbeat": &now, "updated_at": now}
	if n.AgentFingerprint == nil || *n.AgentFingerprint == "" {
		fp := newFingerprint()
		patch["agent_fingerprint"] = &fp
	}
	if n.Status == "offline" {
		patch["status"] = "active"
	}
	uq, err := s.scope(ctx)
	if err != nil {
		return db.Node{}, err
	}
	if err := uq.Model(&db.Node{}).Where("id = ?", id).Updates(patch).Error; err != nil {
		return db.Node{}, err
	}
	return s.Get(ctx, id)
}

// Get returns one node with its effective status applied (stale -> offline).
func (s *Service) Get(ctx context.Context, id string) (db.Node, error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return db.Node{}, err
	}
	var n db.Node
	if err := q.Where("id = ?", id).First(&n).Error; err != nil {
		return db.Node{}, ErrNodeNotFound
	}
	return s.withEffective(n), nil
}

// List returns every node in the org with effective statuses applied.
func (s *Service) List(ctx context.Context) ([]db.Node, error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, err
	}
	var out []db.Node
	if err := q.Order("name ASC").Find(&out).Error; err != nil {
		return nil, err
	}
	for i := range out {
		out[i] = s.withEffective(out[i])
	}
	return out, nil
}

// NodePatch updates mutable node fields. Nil pointers are left alone.
type NodePatch struct {
	Name      *string
	Region    *string
	Status    *string
	Hostname  *string
	PublicIP  *string
	PrivateIP *string
	Capacity  *db.NodeCapacity
	Labels    *map[string]string
}

// Update applies a patch to one node.
func (s *Service) Update(ctx context.Context, id string, patch NodePatch) (db.Node, error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return db.Node{}, err
	}
	var n db.Node
	if err := q.Where("id = ?", id).First(&n).Error; err != nil {
		return db.Node{}, ErrNodeNotFound
	}
	if patch.Name != nil {
		n.Name = *patch.Name
	}
	if patch.Region != nil {
		n.Region = *patch.Region
	}
	if patch.Status != nil {
		n.Status = *patch.Status
	}
	if patch.Hostname != nil {
		n.Hostname = *patch.Hostname
	}
	if patch.PublicIP != nil {
		n.PublicIP = *patch.PublicIP
	}
	if patch.PrivateIP != nil {
		n.PrivateIP = *patch.PrivateIP
	}
	if patch.Capacity != nil {
		n.SetCapacity(*patch.Capacity)
	}
	if patch.Labels != nil {
		n.SetLabels(*patch.Labels)
	}
	sq, err := s.scope(ctx)
	if err != nil {
		return db.Node{}, err
	}
	if err := sq.Save(&n).Error; err != nil {
		return db.Node{}, err
	}
	return s.withEffective(n), nil
}

// Drain marks the node draining; the scheduler must place nothing new on it.
func (s *Service) Drain(ctx context.Context, id string) (db.Node, error) {
	return s.setDraining(ctx, id, true)
}

// Resume clears the draining flag.
func (s *Service) Resume(ctx context.Context, id string) (db.Node, error) {
	return s.setDraining(ctx, id, false)
}

func (s *Service) setDraining(ctx context.Context, id string, draining bool) (db.Node, error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return db.Node{}, err
	}
	var n db.Node
	if err := q.Where("id = ?", id).First(&n).Error; err != nil {
		return db.Node{}, ErrNodeNotFound
	}
	n.Draining = draining
	sq, err := s.scope(ctx)
	if err != nil {
		return db.Node{}, err
	}
	if err := sq.Save(&n).Error; err != nil {
		return db.Node{}, err
	}
	return s.withEffective(n), nil
}

// Delete removes the node row.
func (s *Service) Delete(ctx context.Context, id string) error {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return err
	}
	res := q.Where("id = ?", id).Delete(&db.Node{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNodeNotFound
	}
	return nil
}

// EffectiveStatus reports what a read returns for n: "draining" when the
// drain flag is set, "offline" when silent beyond StaleAfter, else Status
// ("active" when the row carries none). It never mutates the row.
func (s *Service) EffectiveStatus(n db.Node) string {
	if n.Draining {
		return "draining"
	}
	if n.LastHeartbeat == nil || s.deps.now().Sub(*n.LastHeartbeat) > s.deps.staleAfter() {
		return "offline"
	}
	if n.Status == "" {
		return "active"
	}
	return n.Status
}

func (s *Service) withEffective(n db.Node) db.Node {
	n.Status = s.EffectiveStatus(n)
	return n
}
