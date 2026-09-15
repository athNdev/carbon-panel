package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/athNdev/carbon-panel/internal/auth"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/rbac"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// NodeScanResponse is the JSON payload for GET /api/v1/nodes/scan.
type NodeScanResponse struct {
	Candidates []docker.DockerCandidate `json:"candidates"`
	KnownNodes int                      `json:"knownNodes"`
	ScannedAt  string                   `json:"scannedAt"`
}

// NewNodeScanHandler creates an HTTP handler that auto-detects active Docker
// daemons on all available network interfaces by probing the local unix
// socket plus TCP 2375/2376 across interface addresses. It complements the
// per-node PingNode RPC: the scan finds candidate daemons, PingNode verifies
// registered ones.
func NewNodeScanHandler(store *storage.Store, authManager *auth.Manager, enforcer *rbac.Enforcer, log *logger.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Auth check if enabled (nodes are read-guarded like ListNodes).
		if authManager != nil && authManager.IsAnyAuthEnabled() {
			authHeader := r.Header.Get("Authorization")
			user, err := authManager.AuthenticateFromHeader(r.Context(), authHeader)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if enforcer != nil {
				allowed, rbacErr := enforcer.Enforce(user.Roles, rbac.ResourceNodes, rbac.ActionRead, "*")
				if rbacErr != nil || !allowed {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
			}
		}

		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()

		candidates := docker.DetectDockerDaemons(ctx)

		known := 0
		if store != nil {
			if nodes, err := store.ListNodes(ctx); err == nil {
				known = len(nodes)
			} else if log != nil {
				log.Warn("Node scan: failed to list known nodes: %v", err)
			}
		}

		writeJSON(w, http.StatusOK, NodeScanResponse{
			Candidates: candidates,
			KnownNodes: known,
			ScannedAt:  time.Now().UTC().Format(time.RFC3339),
		})
	})
}
