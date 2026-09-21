package handlers

import (
	"encoding/json"
	"net/http"
	"time"
)

// NewHealthHandler returns a JSON health endpoint.
//
// ready is optional: when it is nil the endpoint is a pure liveness check and
// always reports ok; when non-nil it is invoked (e.g. a database ping) and a
// failure produces 503 with status "degraded". The endpoints are intentionally
// unauthenticated but expose no sensitive data.
func NewHealthHandler(started time.Time, ready func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		status := "ok"
		code := http.StatusOK
		if ready != nil {
			if err := ready(); err != nil {
				status = "degraded"
				code = http.StatusServiceUnavailable
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":        status,
			"uptimeSeconds": int(time.Since(started).Seconds()),
		})
	}
}
