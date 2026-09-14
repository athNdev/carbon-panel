package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

	// Route /api/v1/packwiz/loaders/{loader}/versions
	if len(parts) >= 2 && parts[0] == "loaders" {
		loader := parts[1]
		mcVer := r.URL.Query().Get("game_version")
		versions := GetLoaderVersions(r.Context(), loader, mcVer)
		writeJSON(w, http.StatusOK, map[string]any{
			"loader":       loader,
			"game_version": mcVer,
			"versions":     versions,
		})
		return
	}

	if len(parts) >= 2 && parts[0] != "packs" && parts[0] != "loaders" {
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
		// Sub-route /api/v1/packwiz/packs/import
		if parts[1] == "import" && r.Method == http.MethodPost {
			h.handleImportPack(w, r)
			return
		}

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
		case subRoute == "clone" && r.Method == http.MethodPost:
			h.handleClonePack(w, r, packID)
		case subRoute == "refresh" && r.Method == http.MethodPost:
			h.handleRefreshPack(w, r, packID)
		case subRoute == "updates" && r.Method == http.MethodGet:
			h.handleCheckUpdates(w, r, packID)
		case subRoute == "updates" && r.Method == http.MethodPost:
			h.handleApplyUpdates(w, r, packID)
		case subRoute == "migrate" && r.Method == http.MethodPost:
			h.handleMigrate(w, r, packID)
		case subRoute == "files" && r.Method == http.MethodGet:
			h.handleListFiles(w, r, packID)
		case subRoute == "files" && r.Method == http.MethodPost:
			h.handleSaveFile(w, r, packID)
		case subRoute == "files" && r.Method == http.MethodDelete:
			h.handleDeleteFile(w, r, packID)
		case subRoute == "mods" && len(parts) >= 4 && parts[3] == "url" && r.Method == http.MethodPost:
			h.handleAddModURL(w, r, packID)
		case subRoute == "mods" && len(parts) >= 4 && parts[3] == "batch" && r.Method == http.MethodPost:
			h.handleBatchMods(w, r, packID)
		case subRoute == "mods" && len(parts) == 3 && r.Method == http.MethodPost:
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

func (h *PackwizHandler) handleClonePack(w http.ResponseWriter, r *http.Request, packID string) {
	var req struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	cloned, err := h.manager.ClonePack(packID, req.Name)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to clone pack: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, cloned)
}

func (h *PackwizHandler) handleRefreshPack(w http.ResponseWriter, r *http.Request, packID string) {
	if err := h.manager.RefreshPack(packID); err != nil {
		http.Error(w, fmt.Sprintf("failed to refresh pack: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Pack re-indexed and refreshed successfully"})
}

func (h *PackwizHandler) handleCheckUpdates(w http.ResponseWriter, r *http.Request, packID string) {
	updates, err := h.manager.CheckModUpdates(packID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to check updates: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updates": updates})
}

func (h *PackwizHandler) handleApplyUpdates(w http.ResponseWriter, r *http.Request, packID string) {
	var req struct {
		Slug             string `json:"slug"`
		TargetVersionID  string `json:"target_version_id"`
		TargetFileName   string `json:"target_file_name"`
		TargetURL        string `json:"target_url"`
		ApplyAll         bool   `json:"apply_all"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.ApplyAll {
		updates, err := h.manager.CheckModUpdates(packID)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to check updates: %v", err), http.StatusInternalServerError)
			return
		}
		updatedCount := 0
		for _, u := range updates {
			if u.UpdateAvailable && u.LatestVersionID != "" {
				if err := h.manager.ApplyModUpdate(packID, u.Slug, u.LatestVersionID, u.LatestFileName, u.LatestDownloadURL); err == nil {
					updatedCount++
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "updated_count": updatedCount})
		return
	}

	if req.Slug == "" || req.TargetVersionID == "" {
		http.Error(w, "slug and target_version_id are required", http.StatusBadRequest)
		return
	}

	if err := h.manager.ApplyModUpdate(packID, req.Slug, req.TargetVersionID, req.TargetFileName, req.TargetURL); err != nil {
		http.Error(w, fmt.Sprintf("failed to apply update: %v", err), http.StatusInternalServerError)
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

func (h *PackwizHandler) handleAddModURL(w http.ResponseWriter, r *http.Request, packID string) {
	var req packwiz.DirectURLModRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	mod, err := h.manager.AddModFromURL(packID, req)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to add mod from URL: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, mod)
}

func (h *PackwizHandler) handleBatchMods(w http.ResponseWriter, r *http.Request, packID string) {
	var req struct {
		Action string   `json:"action"` // "set_side", "pin", "unpin", "remove"
		Slugs  []string `json:"slugs"`
		Side   string   `json:"side,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	for _, slug := range req.Slugs {
		switch req.Action {
		case "set_side":
			_ = h.manager.UpdateMod(packID, slug, req.Side, false)
		case "pin":
			_ = h.manager.UpdateMod(packID, slug, "", true)
		case "unpin":
			_ = h.manager.UpdateMod(packID, slug, "", false)
		case "remove":
			_ = h.manager.DeleteMod(packID, slug)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "count": len(req.Slugs)})
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

func (h *PackwizHandler) handleListFiles(w http.ResponseWriter, r *http.Request, packID string) {
	files, err := h.manager.ListPackFiles(packID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list files: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

func (h *PackwizHandler) handleSaveFile(w http.ResponseWriter, r *http.Request, packID string) {
	// Multipart file upload or JSON payload
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, "failed to parse multipart form", http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "missing file field", http.StatusBadRequest)
			return
		}
		defer file.Close()

		relPath := r.FormValue("path")
		if relPath == "" {
			relPath = "config/" + header.Filename
		}

		if err := h.manager.SavePackFile(packID, relPath, file); err != nil {
			http.Error(w, fmt.Sprintf("failed to save file: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": relPath})
		return
	}

	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	if err := h.manager.SavePackFile(packID, req.Path, strings.NewReader(req.Content)); err != nil {
		http.Error(w, fmt.Sprintf("failed to save file: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": req.Path})
}

func (h *PackwizHandler) handleDeleteFile(w http.ResponseWriter, r *http.Request, packID string) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path query parameter is required", http.StatusBadRequest)
		return
	}

	if err := h.manager.DeletePackFile(packID, path); err != nil {
		http.Error(w, fmt.Sprintf("failed to delete file: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *PackwizHandler) handleMigrate(w http.ResponseWriter, r *http.Request, packID string) {
	var req struct {
		TargetMC     string `json:"target_mc"`
		TargetLoader string `json:"target_loader"`
		Apply        bool   `json:"apply"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.TargetMC == "" {
		http.Error(w, "target_mc is required", http.StatusBadRequest)
		return
	}
	if req.TargetLoader == "" {
		req.TargetLoader = "fabric"
	}

	if req.Apply {
		report, err := h.manager.ApplyMigration(packID, req.TargetMC, req.TargetLoader)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to apply migration: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, report)
		return
	}

	report, err := h.manager.SimulateMigration(packID, req.TargetMC, req.TargetLoader)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to simulate migration: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *PackwizHandler) handleImportPack(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			http.Error(w, "failed to parse multipart form", http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "missing file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		data, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "failed to read uploaded file", http.StatusInternalServerError)
			return
		}
		readerAt := bytes.NewReader(data)
		size := int64(len(data))

		format := r.FormValue("format")
		if format == "" {
			if strings.HasSuffix(header.Filename, ".mrpack") {
				format = "mrpack"
			} else {
				format = "curseforge"
			}
		}

		opts := packwiz.ImportOptions{
			Name:          r.FormValue("name"),
			Author:        r.FormValue("author"),
			Version:       r.FormValue("version"),
			MCVersion:     r.FormValue("mc_version"),
			ModLoader:     r.FormValue("mod_loader"),
			LoaderVersion: r.FormValue("loader_version"),
		}

		var pack *packwiz.Pack
		switch format {
		case "mrpack":
			pack, err = h.manager.ImportMrpack(readerAt, size, opts)
		case "curseforge":
			pack, err = h.manager.ImportCurseForge(readerAt, size, opts)
		case "packwiz":
			pack, err = h.manager.ImportPackwizZip(readerAt, size, opts)
		default:
			http.Error(w, "unsupported import format", http.StatusBadRequest)
			return
		}

		if err != nil {
			http.Error(w, fmt.Sprintf("import failed: %v", err), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, pack)
		return
	}

	http.Error(w, "multipart/form-data required for import", http.StatusBadRequest)
}

func (h *PackwizHandler) handleExport(w http.ResponseWriter, r *http.Request, packID, format string) {
	pack, err := h.manager.GetPack(packID)
	if err != nil {
		http.Error(w, "pack not found", http.StatusNotFound)
		return
	}

	switch format {
	case "packwiz":
		filename := fmt.Sprintf("%s-%s.packwiz.zip", pack.Name, pack.Version)
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
		if err := h.manager.ExportPackwizZip(packID, w); err != nil {
			h.log.Error("Failed to export packwiz zip: %v", err)
		}
	case "mrpack":
		filename := fmt.Sprintf("%s-%s.mrpack", pack.Name, pack.Version)
		w.Header().Set("Content-Type", "application/x-modrinth-modpack+zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
		if err := h.manager.ExportMrpack(packID, w); err != nil {
			h.log.Error("Failed to export mrpack: %v", err)
		}
	case "curseforge", "zip":
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
