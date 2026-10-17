package audit

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	"github.com/google/uuid"
)

// Store is the append-only audit writer/reader. There is deliberately no
// update or delete API: audit rows are immutable.
type Store interface {
	Append(ctx context.Context, e Event) error
	List(ctx context.Context, f Filter) ([]Event, string, error)
	Get(ctx context.Context, id string) (Event, error)
}

// NewGormStore builds a Store over db.Store. All access goes through
// db.Store.Org(ctx) and refuses without an org in context.
func NewGormStore(s *db.Store) Store { return &gormStore{store: s} }

type gormStore struct {
	store *db.Store
}

func (s *gormStore) Append(ctx context.Context, e Event) error {
	scoped, err := s.store.Org(ctx)
	if err != nil {
		return err
	}
	if e.Action == "" {
		return ErrNoAction
	}
	if e.OrgID == "" {
		e.OrgID = principal.OrgID(ctx)
	}
	if e.OrgID == "" {
		return db.ErrNoOrg
	}
	if ctxOrg := principal.OrgID(ctx); ctxOrg != "" && ctxOrg != e.OrgID {
		return ErrOrgMismatch
	}
	if !e.HasActor() {
		if p, ok := principal.From(ctx); ok && !p.Anonymous() {
			e.ActorKind = string(p.Kind)
			e.ActorUserID = p.UserID
			e.ActorAPIKeyID = p.APIKeyID
			e.ActorNodeID = p.NodeID
		}
	}
	if !e.HasActor() {
		return ErrNoActor
	}
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	if e.Result == "" {
		e.Result = ResultOK
	}
	e = Redact(e)
	detail := ""
	if e.Detail != nil {
		raw, err := json.Marshal(e.Detail)
		if err != nil {
			return err
		}
		detail = string(raw)
	}
	row := db.AuditEvent{
		ID:            e.ID,
		OrgID:         e.OrgID,
		ActorUserID:   e.ActorUserID,
		ActorAPIKeyID: e.ActorAPIKeyID,
		Action:        e.Action,
		ResourceType:  e.ResourceType,
		ResourceID:    e.ResourceID,
		Result:        e.Result,
		DetailJSON:    detail,
		IP:            e.IP,
		UserAgent:     e.UserAgent,
		CreatedAt:     e.CreatedAt,
	}
	// ActorKind/ActorNodeID travel in Detail so nothing is lost; the columns
	// stay exactly as db.AuditEvent declares them.
	if e.ActorKind != "" || e.ActorNodeID != "" {
		var m map[string]any
		if detail != "" {
			_ = json.Unmarshal([]byte(detail), &m)
		}
		if m == nil {
			m = map[string]any{}
		}
		if e.ActorKind != "" {
			m["_actor_kind"] = e.ActorKind
		}
		if e.ActorNodeID != "" {
			m["_actor_node"] = e.ActorNodeID
		}
		if raw, err := json.Marshal(m); err == nil {
			row.DetailJSON = string(raw)
		}
	}
	return scoped.Create(&row).Error
}

func (s *gormStore) List(ctx context.Context, f Filter) ([]Event, string, error) {
	scoped, err := s.store.Org(ctx)
	if err != nil {
		return nil, "", err
	}
	if ctxOrg := principal.OrgID(ctx); f.OrgID != "" && f.OrgID != ctxOrg {
		return nil, "", ErrOrgMismatch
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := 0
	if f.Cursor != "" {
		offset, err = strconv.Atoi(f.Cursor)
		if err != nil || offset < 0 {
			return nil, "", errors.New("audit: invalid cursor")
		}
	}
	q := scoped.Model(&db.AuditEvent{})
	if f.ActorUserID != "" {
		q = q.Where("actor_user_id = ?", f.ActorUserID)
	}
	if f.ActorAPIKeyID != "" {
		q = q.Where("actor_api_key_id = ?", f.ActorAPIKeyID)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.ResourceType != "" {
		q = q.Where("resource_type = ?", f.ResourceType)
	}
	if f.ResourceID != "" {
		q = q.Where("resource_id = ?", f.ResourceID)
	}
	if f.Result != "" {
		q = q.Where("result = ?", f.Result)
	}
	if !f.Since.IsZero() {
		q = q.Where("created_at >= ?", f.Since)
	}
	if !f.Until.IsZero() {
		q = q.Where("created_at <= ?", f.Until)
	}
	var rows []db.AuditEvent
	if err := q.Order("created_at DESC").Offset(offset).Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = strconv.Itoa(offset + limit)
	}
	out := make([]Event, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowToEvent(r))
	}
	return out, next, nil
}

func (s *gormStore) Get(ctx context.Context, id string) (Event, error) {
	scoped, err := s.store.Org(ctx)
	if err != nil {
		return Event{}, err
	}
	var row db.AuditEvent
	if err := scoped.Where("id = ?", id).First(&row).Error; err != nil {
		return Event{}, err
	}
	return rowToEvent(row), nil
}

func rowToEvent(r db.AuditEvent) Event {
	e := Event{
		ID:            r.ID,
		OrgID:         r.OrgID,
		ActorUserID:   r.ActorUserID,
		ActorAPIKeyID: r.ActorAPIKeyID,
		Action:        r.Action,
		ResourceType:  r.ResourceType,
		ResourceID:    r.ResourceID,
		Result:        r.Result,
		IP:            r.IP,
		UserAgent:     r.UserAgent,
		CreatedAt:     r.CreatedAt,
	}
	if r.DetailJSON != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(r.DetailJSON), &m); err == nil {
			if k, ok := m["_actor_kind"].(string); ok {
				e.ActorKind = k
				delete(m, "_actor_kind")
			}
			if n, ok := m["_actor_node"].(string); ok {
				e.ActorNodeID = n
				delete(m, "_actor_node")
			}
			e.Detail = m
		}
	}
	return e
}
