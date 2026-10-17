package obs

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/config"
	"github.com/stretchr/testify/require"
)

func TestMetricsRegistrationIsIdempotent(t *testing.T) {
	t.Parallel()
	// Any number of registries must coexist: no globals, no double-register panic.
	a := NewMetrics()
	b := NewMetrics()
	d1 := Default()
	d2 := Default()
	require.NotNil(t, a)
	require.NotNil(t, b)
	require.Same(t, d1, d2, "Default must return the same registry")
	require.NotSame(t, a, b)

	for _, m := range []*Metrics{a, b, d1} {
		m.Observe("/cloud.v1.NodeService/ListNodes", time.Millisecond, "ok")
		rec := httptest.NewRecorder()
		m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		require.Equal(t, http.StatusOK, rec.Code)
	}
}

func TestMetricsHandlerServesExpectedFamilies(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	proc := "/cloud.v1.NodeService/DeleteNode"
	m.Observe(proc, 1500*time.Microsecond, "ok")
	m.Observe(proc, 2500*time.Microsecond, "permission_denied")
	m.Observe("/cloud.v1.SystemService/GetBuildInfo", time.Millisecond, "ok")

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "text/plain")

	body := rec.Body.String()
	for _, family := range []string{
		"carboncloud_rpc_requests_total",
		"carboncloud_rpc_errors_total",
		"carboncloud_rpc_duration_seconds_bucket",
		"carboncloud_rpc_duration_seconds_sum",
		"carboncloud_rpc_duration_seconds_count",
	} {
		require.Contains(t, body, family, "missing metric family")
	}
	require.Contains(t, body, `carboncloud_rpc_requests_total{procedure="/cloud.v1.NodeService/DeleteNode"} 2`)
	require.Contains(t, body, `carboncloud_rpc_errors_total{procedure="/cloud.v1.NodeService/DeleteNode",code="permission_denied"} 1`)
	require.NotContains(t, body, `code="ok"`, "success must not appear in the error family")

	// Empty registry still renders valid exposition with no samples.
	empty := httptest.NewRecorder()
	NewMetrics().Handler().ServeHTTP(empty, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, empty.Code)
	require.Contains(t, empty.Body.String(), "carboncloud_rpc_requests_total")
}

func TestMetricsObserveBoundary(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	m.Observe("", time.Second, "ok")   // empty procedure: ignored
	m.Observe("p", -time.Second, "ok") // negative duration: clamped
	var nilMetrics *Metrics
	nilMetrics.Observe("p", time.Second, "ok") // nil receiver: no panic
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.NotContains(t, rec.Body.String(), "{procedure=\"\"}")
}

func TestMetricsConcurrentObserve(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				m.Observe("proc", time.Millisecond, "ok")
			}
		}()
	}
	wg.Wait()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Contains(t, rec.Body.String(), `{procedure="proc"} 800`)
}

func TestNewLoggerLevelsAndFormats(t *testing.T) {
	t.Parallel()
	for _, level := range []string{"debug", "info", "warn", "error", "bogus"} {
		for _, format := range []string{"text", "json", "bogus"} {
			logger := NewLogger(config.Telemetry{LogLevel: level, LogFormat: format})
			require.NotNil(t, logger)
		}
	}
}

func TestLogOrgIDRedactionIsConfigurable(t *testing.T) {
	const orgID = "org_abc123xyz"
	newLoggerWithBuffer := func() (*slog.Logger, *bytes.Buffer) {
		var buf bytes.Buffer
		return slog.New(slog.NewTextHandler(&buf, nil)), &buf
	}

	SetRedactOrgID(false)
	t.Cleanup(func() { SetRedactOrgID(false) })
	logger, buf := newLoggerWithBuffer()
	WithRequest(logger, RequestFields{OrgID: orgID, Procedure: "p"}).Info("hello")
	require.Contains(t, buf.String(), orgID, "redaction off: full org id must be logged")

	SetRedactOrgID(true)
	logger, buf = newLoggerWithBuffer()
	WithRequest(logger, RequestFields{OrgID: orgID, Procedure: "p"}).Info("hello")
	require.NotContains(t, buf.String(), orgID, "redaction on: raw org id must not be logged")
	require.Contains(t, buf.String(), RedactOrgID(orgID))
	require.Equal(t, "****", RedactOrgID("ab"), "short ids fully masked")
	require.Equal(t, "", RedactOrgID(""))
}

func TestRequestFieldsRoundTripContext(t *testing.T) {
	t.Parallel()
	logger := NewLogger(config.Telemetry{LogLevel: "info", LogFormat: "text"})
	ctx := ContextWithRequest(t.Context(), RequestFields{OrgID: "o", ActorID: "u", Procedure: "p", TraceID: "tr"})
	fields, ok := RequestFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "o", fields.OrgID)
	require.NotNil(t, LoggerFromContext(ctx, logger))
	_, ok = RequestFromContext(t.Context())
	require.False(t, ok)
	require.NotNil(t, LoggerFromContext(t.Context(), nil))
}

func TestNopTracer(t *testing.T) {
	t.Parallel()
	tr := NopTracer()
	require.NotNil(t, tr)
	ctx, span := tr.Start(t.Context(), "op")
	require.NotNil(t, ctx)
	require.NotNil(t, span)
	span.SetAttr("k", "v")
	span.RecordError(nil)
	span.RecordError(errTest{})
	span.End() // must not panic

	require.Equal(t, "", TraceIDFromContext(t.Context()))
	id := NewTraceID()
	require.Len(t, id, 32)
	ctx = ContextWithTraceID(t.Context(), id)
	require.Equal(t, id, TraceIDFromContext(ctx))
}

type errTest struct{}

func (errTest) Error() string { return "test error" }

func TestHandlerLabelEscaping(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	m.Observe("weird\"proc\\name", time.Millisecond, "not_found")
	body := httptest.NewRecorder()
	m.Handler().ServeHTTP(body, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Contains(t, body.Body.String(), `weird\"proc\\name`)
}
