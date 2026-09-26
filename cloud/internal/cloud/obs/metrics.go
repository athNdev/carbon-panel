// Package obs provides Carbon Cloud observability primitives: RED metrics,
// structured logging, and tracing.
//
// The metrics registry is stdlib-only on purpose (lane constraint: no new Go
// dependencies). It exposes the same http.Handler contract a Prometheus client
// would, emitting Prometheus text exposition format, so wave-3 apiserver code
// can swap the backend without touching call sites.
package obs

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// durationBuckets are the histogram bucket upper bounds in seconds.
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// procedureStats holds the RED counters for one procedure.
type procedureStats struct {
	requests  uint64
	errByCode map[string]uint64
	sum       float64
	count     uint64
	buckets   []uint64 // cumulative, parallel to durationBuckets
}

// Metrics is a stdlib-only RED registry keyed by Connect procedure name
// (e.g. "/cloud.v1.NodeService/ListNodes"). The zero value is unusable;
// construct with NewMetrics. It is safe for concurrent use.
type Metrics struct {
	mu    sync.Mutex
	procs map[string]*procedureStats
}

// NewMetrics returns an empty registry. Calling it any number of times never
// panics and registries never share state, so metric registration is
// idempotent by construction (no global double-register).
func NewMetrics() *Metrics {
	return &Metrics{procs: make(map[string]*procedureStats)}
}

var defaultMetrics = sync.OnceValue(func() *Metrics { return NewMetrics() })

// Default returns the process-wide registry, creating it once.
func Default() *Metrics { return defaultMetrics() }

// Observe records one completed RPC. code is "ok" on success or a
// connect.Code string on failure; anything other than "ok" (and "") counts
// as an error.
func (m *Metrics) Observe(procedure string, d time.Duration, code string) {
	if m == nil || procedure == "" {
		return
	}
	secs := d.Seconds()
	if secs < 0 {
		secs = 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.procs[procedure]
	if !ok {
		st = &procedureStats{errByCode: make(map[string]uint64), buckets: make([]uint64, len(durationBuckets))}
		m.procs[procedure] = st
	}
	st.requests++
	if code != "" && code != "ok" {
		st.errByCode[code]++
	}
	st.sum += secs
	st.count++
	for i, b := range durationBuckets {
		if secs <= b {
			st.buckets[i]++
		}
	}
}

// snapshot returns a deep copy for rendering without holding the lock.
func (m *Metrics) snapshot() map[string]procedureStats {
	out := make(map[string]procedureStats, len(m.procs))
	m.mu.Lock()
	defer m.mu.Unlock()
	for proc, st := range m.procs {
		cp := procedureStats{
			requests:  atomic.LoadUint64(&st.requests),
			errByCode: make(map[string]uint64, len(st.errByCode)),
			sum:       st.sum,
			count:     st.count,
			buckets:   append([]uint64(nil), st.buckets...),
		}
		for code, n := range st.errByCode {
			cp.errByCode[code] = n
		}
		out[proc] = cp
	}
	return out
}

// escapeLabel escapes a Prometheus label value.
func escapeLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// Handler returns an http.Handler serving the registry in Prometheus text
// exposition format (Content-Type: text/plain; version=0.0.4).
func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		snap := m.snapshot()
		procs := make([]string, 0, len(snap))
		for p := range snap {
			procs = append(procs, p)
		}
		sort.Strings(procs)
		var b strings.Builder
		b.WriteString("# HELP carboncloud_rpc_requests_total Total RPCs by procedure.\n")
		b.WriteString("# TYPE carboncloud_rpc_requests_total counter\n")
		for _, p := range procs {
			fmt.Fprintf(&b, "carboncloud_rpc_requests_total{procedure=\"%s\"} %d\n", escapeLabel(p), snap[p].requests)
		}
		b.WriteString("# HELP carboncloud_rpc_errors_total Failed RPCs by procedure and code.\n")
		b.WriteString("# TYPE carboncloud_rpc_errors_total counter\n")
		for _, p := range procs {
			codes := make([]string, 0, len(snap[p].errByCode))
			for c := range snap[p].errByCode {
				codes = append(codes, c)
			}
			sort.Strings(codes)
			for _, c := range codes {
				fmt.Fprintf(&b, "carboncloud_rpc_errors_total{procedure=\"%s\",code=\"%s\"} %d\n",
					escapeLabel(p), escapeLabel(c), snap[p].errByCode[c])
			}
		}
		b.WriteString("# HELP carboncloud_rpc_duration_seconds RPC latency histogram by procedure.\n")
		b.WriteString("# TYPE carboncloud_rpc_duration_seconds histogram\n")
		for _, p := range procs {
			st := snap[p]
			// buckets[] is already cumulative from Observe: bucket i holds the
			// count of observations <= durationBuckets[i]. Render as-is.
			for i, bound := range durationBuckets {
				fmt.Fprintf(&b, "carboncloud_rpc_duration_seconds_bucket{procedure=\"%s\",le=\"%s\"} %d\n",
					escapeLabel(p), fmt.Sprintf("%g", bound), st.buckets[i])
			}
			fmt.Fprintf(&b, "carboncloud_rpc_duration_seconds_bucket{procedure=\"%s\",le=\"%s\"} %d\n",
				escapeLabel(p), "+Inf", st.count)
			fmt.Fprintf(&b, "carboncloud_rpc_duration_seconds_sum{procedure=\"%s\"} %g\n", escapeLabel(p), st.sum)
			fmt.Fprintf(&b, "carboncloud_rpc_duration_seconds_count{procedure=\"%s\"} %d\n", escapeLabel(p), st.count)
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = io.WriteString(w, b.String())
	})
}
