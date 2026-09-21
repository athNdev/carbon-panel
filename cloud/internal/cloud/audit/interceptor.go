package audit

import (
	"context"
	"sync/atomic"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
)

// Audit-write failure policy (documented choice): the interceptor must never
// fail a mutating RPC just because its audit write failed — failing the RPC
// would let a broken audit store wedge the whole control plane. Instead a
// failed write is surfaced through Options.OnError and increments the failure
// counter (see Failures). A missing write for a mutating action is therefore
// an operator-alertable event, not a silent gap: alert on
// OnError invocations / Failures() > 0, and treat a rising counter as a
// paging-severity signal that audit coverage is degraded.

// Options configures Interceptor.
type Options struct {
	// OnError is called for every failed audit write, with the event that
	// could not be stored. If nil, failures only increment the counter.
	OnError func(ctx context.Context, evt Event, err error)
	// Resource overrides the recorded resource type/id per procedure.
	// When nil, the resource type defaults from the action table and the id
	// is empty.
	Resource func(procedure string, req connect.AnyRequest) (resourceType, resourceID string)
}

// Interceptor returns a connect.Interceptor that writes exactly one audit
// event per mutating RPC, success or failure. Read-only and unknown
// procedures pass through untouched.
func Interceptor(store Store, opts Options) connect.Interceptor {
	return &auditInterceptor{store: store, opts: opts}
}

type auditInterceptor struct {
	store    Store
	opts     Options
	failures atomic.Int64
}

func (i *auditInterceptor) Failures() int64 { return i.failures.Load() }

// Failures returns the number of failed audit writes for an interceptor built
// by Interceptor. The operator should alert when this rises.
func Failures(in connect.Interceptor) int64 {
	if a, ok := in.(*auditInterceptor); ok {
		return a.Failures()
	}
	return -1
}

func (i *auditInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		procedure := req.Spec().Procedure
		if !IsMutating(procedure) {
			return next(ctx, req)
		}
		res, err := next(ctx, req)
		var peer, ua string
		if req != nil {
			peer = req.Peer().Addr
			ua = req.Header().Get("User-Agent")
		}
		i.record(ctx, procedure, req, peer, ua, err)
		return res, err
	}
}

func (i *auditInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *auditInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		procedure := conn.Spec().Procedure
		if !IsMutating(procedure) {
			return next(ctx, conn)
		}
		err := next(ctx, conn)
		i.record(ctx, procedure, nil, conn.Peer().Addr, conn.RequestHeader().Get("User-Agent"), err)
		return err
	}
}

func (i *auditInterceptor) record(ctx context.Context, procedure string, req connect.AnyRequest, peer, userAgent string, err error) {
	result := ResultOK
	detail := map[string]any{}
	if err != nil {
		code := connect.CodeOf(err)
		detail["error_code"] = code.String()
		switch code {
		case connect.CodePermissionDenied, connect.CodeUnauthenticated:
			result = ResultDenied
		default:
			result = ResultError
		}
		// Only the error class is stored, never the message: messages may
		// leak internal detail.
	}
	resourceType := defaultResourceType(procedure)
	resourceID := ""
	if i.opts.Resource != nil {
		resourceType, resourceID = i.opts.Resource(procedure, req)
	}
	evt := NewEvent(ctx, RequestMeta{
		Procedure:    procedure,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Result:       result,
		Detail:       detail,
		IP:           peer,
		UserAgent:    userAgent,
	})
	if _, ok := principal.From(ctx); !ok {
		// No actor, no org: refuse to write an unattributed event, and make
		// the miss visible instead of silent.
		i.fail(ctx, evt, ErrNoActor)
		return
	}
	if werr := i.store.Append(ctx, evt); werr != nil {
		i.fail(ctx, evt, werr)
	}
}

func (i *auditInterceptor) fail(ctx context.Context, evt Event, err error) {
	i.failures.Add(1)
	if i.opts.OnError != nil {
		i.opts.OnError(ctx, evt, err)
	}
}
