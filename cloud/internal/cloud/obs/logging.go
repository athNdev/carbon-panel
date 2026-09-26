// Structured logging for Carbon Cloud: slog loggers built from
// config.Telemetry plus request-scoped fields and org-id redaction.
package obs

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/config"
)

// redactOrgID controls whether WithRequest masks the org id. It defaults to
// the value of CARBONCLOUD_OBS_REDACT_ORG_ID ("1"/"true" enable) and can be
// changed at runtime with SetRedactOrgID, so operators choose between
// debuggability (ids visible) and log-safety (ids masked).
var redactOrgID atomic.Bool

func init() {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("CARBONCLOUD_OBS_REDACT_ORG_ID")))
	redactOrgID.Store(v == "1" || v == "true")
}

// SetRedactOrgID toggles org-id masking in request-scoped log fields.
func SetRedactOrgID(redact bool) { redactOrgID.Store(redact) }

// RedactOrgID returns the log-safe form of an org id.
func RedactOrgID(id string) string {
	if id == "" {
		return ""
	}
	if !redactOrgID.Load() {
		return id
	}
	if len(id) <= 4 {
		return "****"
	}
	return id[:2] + "****" + id[len(id)-2:]
}

// NewLogger builds a structured slog logger from config.Telemetry. LogLevel
// accepts debug|info|warn|error (matching config validation); anything else
// falls back to info so a bad value can never silence error logs. LogFormat
// accepts json|text and falls back to text.
func NewLogger(cfg config.Telemetry) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(strings.TrimSpace(cfg.LogLevel)) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if strings.ToLower(strings.TrimSpace(cfg.LogFormat)) == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	return slog.New(handler)
}

// RequestFields are the request-scoped fields attached to every log line for
// a request. Empty values are omitted.
type RequestFields struct {
	OrgID     string
	ActorID   string
	Procedure string
	TraceID   string
}

// attrs renders the fields as slog attrs, applying org-id redaction when enabled.
func (f RequestFields) attrs() []any {
	var out []any
	if f.OrgID != "" {
		out = append(out, slog.String("org_id", RedactOrgID(f.OrgID)))
	}
	if f.ActorID != "" {
		out = append(out, slog.String("actor_id", f.ActorID))
	}
	if f.Procedure != "" {
		out = append(out, slog.String("procedure", f.Procedure))
	}
	if f.TraceID != "" {
		out = append(out, slog.String("trace_id", f.TraceID))
	}
	return out
}

// WithRequest returns a logger carrying the request-scoped fields.
func WithRequest(logger *slog.Logger, fields RequestFields) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	if attrs := fields.attrs(); len(attrs) > 0 {
		return logger.With(attrs...)
	}
	return logger
}

type requestFieldsKey struct{}

// ContextWithRequest stores request fields on a context.
func ContextWithRequest(ctx context.Context, fields RequestFields) context.Context {
	return context.WithValue(ctx, requestFieldsKey{}, fields)
}

// RequestFromContext recovers request fields stored by ContextWithRequest.
func RequestFromContext(ctx context.Context) (RequestFields, bool) {
	fields, ok := ctx.Value(requestFieldsKey{}).(RequestFields)
	return fields, ok
}

// LoggerFromContext returns the logger annotated with any request fields on ctx.
func LoggerFromContext(ctx context.Context, logger *slog.Logger) *slog.Logger {
	if fields, ok := RequestFromContext(ctx); ok {
		return WithRequest(logger, fields)
	}
	if logger == nil {
		return slog.Default()
	}
	return logger
}
