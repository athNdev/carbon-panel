// No-op tracing for Carbon Cloud. Nothing requires a trace backend at boot:
// the default tracer discards everything, and a real backend can replace it
// later without touching call sites.
package obs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// Span is one unit of traced work.
type Span interface {
	// End finishes the span.
	End()
	// RecordError records err on the span (no-op when err is nil).
	RecordError(err error)
	// SetAttr sets a string attribute on the span.
	SetAttr(key, value string)
}

// Tracer starts spans.
type Tracer interface {
	Start(ctx context.Context, name string) (context.Context, Span)
}

// nopSpan discards everything.
type nopSpan struct{}

func (nopSpan) End()                {}
func (nopSpan) RecordError(_ error) {}
func (nopSpan) SetAttr(_, _ string) {}

// nopTracer is the default: tracing disabled, zero cost, zero requirements.
type nopTracer struct{}

func (nopTracer) Start(ctx context.Context, _ string) (context.Context, Span) {
	return ctx, nopSpan{}
}

// NopTracer returns the default no-op tracer. Use it wherever a Tracer is
// required but no backend is configured.
func NopTracer() Tracer { return nopTracer{} }

type traceIDKey struct{}

// NewTraceID returns a random 16-byte hex trace id (stdlib only).
func NewTraceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}

// ContextWithTraceID stores a trace id on a context.
func ContextWithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, id)
}

// TraceIDFromContext recovers the trace id, or "" when none is set.
func TraceIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(traceIDKey{}).(string)
	return id
}
