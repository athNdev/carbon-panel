// Package audit is the append-only audit trail for Carbon Cloud.
//
// Every mutating Connect-RPC is recorded exactly once (success or failure) via
// Interceptor. Storage goes through db.Store.Org(ctx), so events are tenant
// scoped and writes without an org are refused. Detail is redacted on the write
// path (see Redact); there is no update or delete API.
package audit

import (
	"context"
	"errors"
	"time"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	"github.com/google/uuid"
)

// Result values stored on Event.Result.
const (
	ResultOK     = "ok"
	ResultError  = "error"
	ResultDenied = "denied"
)

// Deny-by-default errors. A missing write is never silent: callers surface
// these through Options.OnError and the failure counter (see Interceptor).
var (
	ErrNoActor     = errors.New("audit: no actor in context")
	ErrNoAction    = errors.New("audit: event has no action")
	ErrOrgMismatch = errors.New("audit: event org does not match context org")
)

// Event is one audit record. Actor and OrgID are always derived from the
// request principal (see NewEvent), never from request bodies.
type Event struct {
	ID            string
	OrgID         string
	ActorKind     string
	ActorUserID   string
	ActorAPIKeyID string
	ActorNodeID   string
	Action        string
	ResourceType  string
	ResourceID    string
	Result        string
	Detail        map[string]any
	IP            string
	UserAgent     string
	CreatedAt     time.Time
}

// RequestMeta carries per-request transport metadata. Actor and org are NOT
// part of it: they always come from principal.From(ctx).
type RequestMeta struct {
	Procedure    string
	Action       string // defaults to ActionName(Procedure)
	ResourceType string
	ResourceID   string
	Result       string // defaults to ResultOK
	Detail       map[string]any
	IP           string
	UserAgent    string
}

// NewEvent derives actor and org from principal.From(ctx) and transport
// metadata from meta. It never reads identity from a request body.
func NewEvent(ctx context.Context, meta RequestMeta) Event {
	e := Event{
		ID:           uuid.NewString(),
		Action:       meta.Action,
		ResourceType: meta.ResourceType,
		ResourceID:   meta.ResourceID,
		Result:       meta.Result,
		Detail:       meta.Detail,
		IP:           meta.IP,
		UserAgent:    meta.UserAgent,
		CreatedAt:    time.Now().UTC(),
	}
	if e.Action == "" && meta.Procedure != "" {
		e.Action = ActionName(meta.Procedure)
	}
	if e.Result == "" {
		e.Result = ResultOK
	}
	if p, ok := principal.From(ctx); ok && !p.Anonymous() {
		e.OrgID = p.OrgID
		e.ActorKind = string(p.Kind)
		e.ActorUserID = p.UserID
		e.ActorAPIKeyID = p.APIKeyID
		e.ActorNodeID = p.NodeID
	}
	return e
}

// HasActor reports whether the event is attributed to someone.
func (e Event) HasActor() bool {
	return e.ActorKind != "" || e.ActorUserID != "" || e.ActorAPIKeyID != "" || e.ActorNodeID != ""
}

// Filter scopes Store.List. The org always comes from ctx; OrgID, when set,
// must match it. Limit defaults to 100 and caps at 1000. Cursor is an opaque
// offset returned by a previous List call.
type Filter struct {
	OrgID         string
	ActorUserID   string
	ActorAPIKeyID string
	Action        string
	ResourceType  string
	ResourceID    string
	Result        string
	Since         time.Time
	Until         time.Time
	Limit         int
	Cursor        string
}
