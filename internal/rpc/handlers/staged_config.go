package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/athNdev/carbon-panel/internal/auth"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/rbac"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// StagedConfigHandler serves scheduled/staged config rollout for instances.
//
// Mounted at /api/v1/staged-config/ (see rpc/server.go):
//   - GET    /api/v1/staged-config/{serverID}/staged[?status=staged]  list changes
//   - POST   /api/v1/staged-config/{serverID}/staged                  stage a diff
//   - POST   /api/v1/staged-config/{serverID}/staged/{id}/apply       apply now
//   - DELETE /api/v1/staged-config/{serverID}/staged/{id}             discard
//
// Staging records the change regardless of the instance's operational state.
// "on_restart" changes are picked up by ApplyOnRestartStagedConfigs (call it
// from the server start/restart path); "scheduled" changes are picked up by
// the scheduler cron tick (see scheduler/staged_config.go).
type StagedConfigHandler struct {
	store       *storage.Store
	log         *logger.Logger
	authManager *auth.Manager
	enforcer    *rbac.Enforcer
}

func NewStagedConfigHandler(store *storage.Store, log *logger.Logger, authManager *auth.Manager, enforcer *rbac.Enforcer) *StagedConfigHandler {
	return &StagedConfigHandler{
		store:       store,
		log:         log,
		authManager: authManager,
		enforcer:    enforcer,
	}
}

func (h *StagedConfigHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.authManager != nil && h.authManager.IsAnyAuthEnabled() {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			if token := r.URL.Query().Get("token"); token != "" {
				authHeader = "Bearer " + token
			}
		}
		user, err := h.authManager.AuthenticateFromHeader(r.Context(), authHeader)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if h.enforcer != nil {
			action := rbac.ActionRead
			if r.Method != http.MethodGet {
				action = rbac.ActionCreate
			}
			allowed, rbacErr := h.enforcer.Enforce(user.Roles, rbac.ResourceServerConfig, action, "*")
			if rbacErr != nil || !allowed {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
	}

	if h.store == nil {
		http.Error(w, "store not initialized", http.StatusInternalServerError)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/staged-config/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// Expect: {serverID}/staged[/{id}[/apply]]
	if len(parts) < 2 || parts[0] == "" || parts[1] != "staged" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	serverID := parts[0]

	switch {
	case len(parts) == 2 && r.Method == http.MethodGet:
		h.handleList(w, r, serverID)
	case len(parts) == 2 && r.Method == http.MethodPost:
		h.handleStage(w, r, serverID)
	case len(parts) == 4 && parts[3] == "apply" && r.Method == http.MethodPost:
		h.handleApply(w, r, parts[2])
	case len(parts) == 3 && r.Method == http.MethodDelete:
		h.handleDiscard(w, r, parts[2])
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (h *StagedConfigHandler) handleList(w http.ResponseWriter, r *http.Request, serverID string) {
	status := storage.StagedStatus(strings.TrimSpace(r.URL.Query().Get("status")))
	changes, err := h.store.ListStagedConfigChanges(r.Context(), serverID, status)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list staged changes: %v", err), http.StatusInternalServerError)
		return
	}
	if changes == nil {
		changes = []*storage.StagedConfigChange{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"changes": changes, "count": len(changes)})
}

func (h *StagedConfigHandler) handleStage(w http.ResponseWriter, r *http.Request, serverID string) {
	var req struct {
		Changes   map[string]any `json:"changes"`
		ApplyMode string         `json:"apply_mode"`
		CronExpr  string         `json:"cron_expr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	mode := storage.StagedApplyMode(strings.TrimSpace(req.ApplyMode))
	if mode == "" {
		mode = storage.StagedApplyOnRestart
	}

	staged, err := h.store.StageConfigChange(r.Context(), serverID, req.Changes, mode, strings.TrimSpace(req.CronExpr))
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to stage config change: %v", err), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, staged)
}

func (h *StagedConfigHandler) handleApply(w http.ResponseWriter, r *http.Request, stagedID string) {
	applied, err := h.store.ApplyStagedConfigChange(r.Context(), stagedID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to apply staged change: %v", err), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "change": applied})
}

func (h *StagedConfigHandler) handleDiscard(w http.ResponseWriter, r *http.Request, stagedID string) {
	if err := h.store.DiscardStagedConfigChange(r.Context(), stagedID); err != nil {
		http.Error(w, fmt.Sprintf("failed to discard staged change: %v", err), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
