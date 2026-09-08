package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/nickheyer/discopanel/internal/auth"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/minecraft"
	"github.com/nickheyer/discopanel/internal/packwiz"
	"github.com/nickheyer/discopanel/internal/rbac"
	"github.com/nickheyer/discopanel/pkg/logger"
)

type PackwizHandler struct {
	manager     *packwiz.Manager
	store       *storage.Store
	log         *logger.Logger
	authManager *auth.Manager
	enforcer    *rbac.Enforcer
}

func NewPackwizHandler(manager *packwiz.Manager, store *storage.Store, log *logger.Logger, authManager *auth.Manager, enforcer *rbac.Enforcer) *PackwizHandler {
	return &PackwizHandler{
		manager:     manager,
		store:       store,
		log:         log,
		authManager: authManager,
		enforcer:    enforcer,
	}
}

type DeployPackRequest struct {
	ServerID string `json:"server_id"`
	Mode     string `json:"mode"` // "sync" (Mode A) or "bake" (Mode B)
}

func (h *PackwizHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Raw pack file serving for container bootstrapping (e.g. /api/v1/packwiz/{id}/pack.toml)
	// Does not require auth so container runtime on host/docker network can fetch pack.toml
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/packwiz/")
	parts := strings.Split(path, "/")

	if len(parts) >= 2 && parts[0] != "packs" {
		packID := parts[0]
		relPath := strings.Join(parts[1:], "/")
		data, contentType, err := h.manager.ServePackFile(packID, relPath)
		if err != nil {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}

	// Auth check for studio management routes
	if h.authManager != nil && h.authManager.IsAnyAuthEnabled() {
		authHeader := r.Header.Get("Authorization")
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
			allowed, rbacErr := h.enforcer.Enforce(user.Roles, rbac.ResourceModpacks, action, "*")
			if rbacErr != nil || !allowed {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
	}

	// Route /api/v1/packwiz/packs...
	if len(parts) == 1 && parts[0] == "packs" {
		if r.Method == http.MethodGet {
			h.handleListPacks(w, r)
			return
		} else if r.Method == http.MethodPost {
			h.handleCreatePack(w, r)
			return
		}
	}

	if len(parts) >= 2 && parts[0] == "packs" {
		packID := parts[1]

		if len(parts) == 2 {
			switch r.Method {
			case http.MethodGet:
				h.handleGetPack(w, r, packID)
			case http.MethodPut:
				h.handleUpdatePack(w, r, packID)
			case http.MethodDelete:
				h.handleDeletePack(w, r, packID)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}

		subRoute := parts[2]
		switch {
		case subRoute == "mods" && r.Method == http.MethodPost:
			h.handleAddMod(w, r, packID)
		case subRoute == "mods" && len(parts) >= 4 && r.Method == http.MethodPatch:
			modSlug := parts[3]
			h.handleUpdateMod(w, r, packID, modSlug)
		case subRoute == "mods" && len(parts) >= 4 && r.Method == http.MethodDelete:
			modSlug := parts[3]
			h.handleDeleteMod(w, r, packID, modSlug)
		case subRoute == "export" && len(parts) >= 4:
			exportFormat := parts[3]
			h.handleExport(w, r, packID, exportFormat)
		case subRoute == "deploy" && r.Method == http.MethodPost:
			h.handleDeploy(w, r, packID)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}

func (h *PackwizHandler) handleListPacks(w http.ResponseWriter, r *http.Request) {
	packs, err := h.manager.ListPacks()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list packs: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"packs": packs})
}

func (h *PackwizHandler) handleGetPack(w http.ResponseWriter, r *http.Request, packID string) {
	pack, err := h.manager.GetPack(packID)
	if err != nil {
		http.Error(w, "pack not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, pack)
}

func (h *PackwizHandler) handleCreatePack(w http.ResponseWriter, r *http.Request) {
	var pack packwiz.Pack
	if err := json.NewDecoder(r.Body).Decode(&pack); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	created, err := h.manager.CreatePack(&pack)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to create pack: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *PackwizHandler) handleUpdatePack(w http.ResponseWriter, r *http.Request, packID string) {
	var pack packwiz.Pack
	if err := json.NewDecoder(r.Body).Decode(&pack); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	pack.ID = packID

	if err := h.manager.UpdatePack(&pack); err != nil {
		http.Error(w, fmt.Sprintf("failed to update pack: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *PackwizHandler) handleDeletePack(w http.ResponseWriter, r *http.Request, packID string) {
	if err := h.manager.DeletePack(packID); err != nil {
		http.Error(w, fmt.Sprintf("failed to delete pack: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *PackwizHandler) handleAddMod(w http.ResponseWriter, r *http.Request, packID string) {
	var mod packwiz.ModItem
	if err := json.NewDecoder(r.Body).Decode(&mod); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.manager.AddMod(packID, mod); err != nil {
		http.Error(w, fmt.Sprintf("failed to add mod: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *PackwizHandler) handleUpdateMod(w http.ResponseWriter, r *http.Request, packID, slug string) {
	var req struct {
		Side   string `json:"side"`
		Pinned bool   `json:"pinned"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.manager.UpdateMod(packID, slug, req.Side, req.Pinned); err != nil {
		http.Error(w, fmt.Sprintf("failed to update mod: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *PackwizHandler) handleDeleteMod(w http.ResponseWriter, r *http.Request, packID, slug string) {
	if err := h.manager.DeleteMod(packID, slug); err != nil {
		http.Error(w, fmt.Sprintf("failed to remove mod: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *PackwizHandler) handleExport(w http.ResponseWriter, r *http.Request, packID, format string) {
	pack, err := h.manager.GetPack(packID)
	if err != nil {
		http.Error(w, "pack not found", http.StatusNotFound)
		return
	}

	switch format {
	case "mrpack":
		filename := fmt.Sprintf("%s-%s.mrpack", pack.Name, pack.Version)
		w.Header().Set("Content-Type", "application/x-modrinth-modpack+zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
		if err := h.manager.ExportMrpack(packID, w); err != nil {
			h.log.Error("Failed to export mrpack: %v", err)
		}
	case "curseforge":
		filename := fmt.Sprintf("%s-%s.zip", pack.Name, pack.Version)
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
		if err := h.manager.ExportCurseForge(packID, w); err != nil {
			h.log.Error("Failed to export curseforge pack: %v", err)
		}
	default:
		http.Error(w, "unsupported export format", http.StatusBadRequest)
	}
}

func (h *PackwizHandler) handleDeploy(w http.ResponseWriter, r *http.Request, packID string) {
	var req DeployPackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	pack, err := h.manager.GetPack(packID)
	if err != nil {
		http.Error(w, "pack not found", http.StatusNotFound)
		return
	}

	server, err := h.store.GetServer(r.Context(), req.ServerID)
	if err != nil {
		http.Error(w, "server not found", http.StatusNotFound)
		return
	}

	cfg, err := h.store.GetServerConfig(r.Context(), req.ServerID)
	if err != nil {
		http.Error(w, "failed to get server config", http.StatusInternalServerError)
		return
	}

	// Align Minecraft version and mod loader
	server.MCVersion = pack.MCVersion
	server.ModLoader = storage.ModLoader(strings.ToLower(pack.ModLoader))
	_ = h.store.UpdateServer(r.Context(), server)

	if req.Mode == "sync" {
		// Mode A: Live Sync URL
		host := r.Host
		if host == "" {
			host = "localhost:8080"
		}
		packURL := fmt.Sprintf("http://%s/api/v1/packwiz/%s/pack.toml", host, packID)
		cfg.PackwizURL = &packURL
		typeStr := strings.ToUpper(pack.ModLoader)
		cfg.Type = &typeStr
		cfg.Version = &pack.MCVersion

		if err := h.store.UpdateServerConfig(r.Context(), cfg); err != nil {
			http.Error(w, fmt.Sprintf("failed to update config: %v", err), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"mode":    "sync",
			"url":     packURL,
			"message": fmt.Sprintf("Server configured with PACKWIZ_URL=%s", packURL),
		})
		return
	}

	// Mode B: Bake directly to Server Data (/data/mods)
	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	installedCount, err := h.manager.BakeToServer(packID, modsDir)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to bake to server: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"mode":      "bake",
		"installed": installedCount,
		"message":   fmt.Sprintf("Baked %d server-side mod(s) directly into %s", installedCount, modsDir),
	})
}
