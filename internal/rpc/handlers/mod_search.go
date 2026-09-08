package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickheyer/discopanel/internal/auth"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/minecraft"
	"github.com/nickheyer/discopanel/internal/rbac"
	"github.com/nickheyer/discopanel/pkg/logger"
)

type SearchModResult struct {
	ID          string   `json:"id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	IconURL     string   `json:"icon_url"`
	Author      string   `json:"author"`
	Downloads   int64    `json:"downloads"`
	Categories  []string `json:"categories"`
	Platform    string   `json:"platform"`
	ClientSide  string   `json:"client_side,omitempty"`
	ServerSide  string   `json:"server_side,omitempty"`
	Installed   bool     `json:"installed"`
}

type ModDependencyItem struct {
	ProjectID      string `json:"project_id"`
	VersionID      string `json:"version_id,omitempty"`
	DependencyType string `json:"dependency_type"` // "required", "optional"
	Title          string `json:"title,omitempty"`
}

type ModVersionItem struct {
	ID            string              `json:"id"`
	VersionNumber string              `json:"version_number"`
	Name          string              `json:"name"`
	VersionType   string              `json:"version_type"`
	FileName      string              `json:"file_name"`
	DownloadURL   string              `json:"download_url"`
	FileSize      int64               `json:"file_size"`
	Dependencies  []ModDependencyItem `json:"dependencies"`
}

type InstallModItem struct {
	Name        string `json:"name"`
	FileName    string `json:"file_name"`
	DownloadURL string `json:"download_url"`
}

type InstallModsRequest struct {
	Items []InstallModItem `json:"items"`
}

type InstallModsResponse struct {
	Success   bool     `json:"success"`
	Installed []string `json:"installed"`
	Errors    []string `json:"errors,omitempty"`
	Message   string   `json:"message"`
}

// ModOnlineManager handles searching, fetching versions and 1-click installing mods
type ModOnlineManager struct {
	store       *storage.Store
	log         *logger.Logger
	authManager *auth.Manager
	enforcer    *rbac.Enforcer
	httpClient  *http.Client
}

func NewModOnlineManager(store *storage.Store, log *logger.Logger, authManager *auth.Manager, enforcer *rbac.Enforcer) *ModOnlineManager {
	return &ModOnlineManager{
		store:       store,
		log:         log,
		authManager: authManager,
		enforcer:    enforcer,
		httpClient:  &http.Client{Timeout: 20 * time.Second},
	}
}

func (m *ModOnlineManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Auth check if enabled
	if m.authManager != nil && m.authManager.IsAnyAuthEnabled() {
		authHeader := r.Header.Get("Authorization")
		user, err := m.authManager.AuthenticateFromHeader(r.Context(), authHeader)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if m.enforcer != nil {
			action := rbac.ActionRead
			if r.Method == http.MethodPost {
				action = rbac.ActionCreate
			}
			allowed, rbacErr := m.enforcer.Enforce(user.Roles, rbac.ResourceMods, action, "*")
			if rbacErr != nil || !allowed {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
	}

	// Path parsing: /api/v1/servers/{id}/mods/...
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/servers/")
	parts := strings.Split(path, "/")
	if len(parts) < 3 || parts[1] != "mods" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	serverID := parts[0]
	action := parts[2]

	if m.store == nil {
		http.Error(w, "store not initialized", http.StatusInternalServerError)
		return
	}

	server, err := m.store.GetServer(r.Context(), serverID)
	if err != nil {
		http.Error(w, "server not found", http.StatusNotFound)
		return
	}

	switch {
	case action == "search" && r.Method == http.MethodGet:
		m.handleSearch(w, r, server)
	case action == "install" && r.Method == http.MethodPost:
		m.handleInstall(w, r, server)
	case len(parts) >= 4 && parts[3] == "versions" && r.Method == http.MethodGet:
		slug := parts[2]
		m.handleVersions(w, r, server, slug)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (m *ModOnlineManager) handleSearch(w http.ResponseWriter, r *http.Request, server *storage.Server) {
	query := r.URL.Query().Get("query")
	platform := strings.ToLower(r.URL.Query().Get("platform"))
	if platform == "" {
		platform = "modrinth"
	}

	loader := strings.ToLower(r.URL.Query().Get("loader"))
	if loader == "" {
		loader = strings.ToLower(string(server.ModLoader))
	}
	mcVersion := r.URL.Query().Get("mc_version")
	if mcVersion == "" {
		mcVersion = server.MCVersion
	}

	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	installedMods := make(map[string]bool)
	if files, err := os.ReadDir(modsDir); err == nil {
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".jar") {
				lower := strings.ToLower(f.Name())
				installedMods[lower] = true
			}
		}
	}

	globalSettings, _, _ := m.store.GetGlobalSettings(r.Context())

	var results []SearchModResult

	if platform == "curseforge" {
		apiKey := ""
		if globalSettings != nil && globalSettings.CFAPIKey != nil {
			apiKey = *globalSettings.CFAPIKey
		}
		if apiKey == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"results": []SearchModResult{},
				"error":   "CurseForge API key not configured. Please set it in Settings -> API Keys.",
			})
			return
		}

		results = m.searchCurseForge(r.Context(), apiKey, query, loader, mcVersion, installedMods)
	} else {
		// Modrinth
		token := ""
		ua := "MineServer/1.0 (discopanel-admin)"
		if globalSettings != nil {
			if globalSettings.ModrinthToken != nil && *globalSettings.ModrinthToken != "" {
				token = *globalSettings.ModrinthToken
			}
			if globalSettings.ModrinthUserAgent != nil && *globalSettings.ModrinthUserAgent != "" {
				ua = *globalSettings.ModrinthUserAgent
			}
		}
		results = m.searchModrinth(r.Context(), token, ua, query, loader, mcVersion, installedMods)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"results": results,
		"count":   len(results),
	})
}

func (m *ModOnlineManager) searchModrinth(ctx context.Context, token, ua, query, loader, mcVersion string, installed map[string]bool) []SearchModResult {
	facets := [][]string{{"project_type:mod"}}
	if loader != "" && loader != "vanilla" {
		facets = append(facets, []string{fmt.Sprintf("categories:%s", loader)})
	}
	if mcVersion != "" {
		facets = append(facets, []string{fmt.Sprintf("versions:%s", mcVersion)})
	}

	facetsBytes, _ := json.Marshal(facets)
	reqURL := fmt.Sprintf("https://api.modrinth.com/v2/search?query=%s&facets=%s&limit=25",
		url.QueryEscape(query), url.QueryEscape(string(facetsBytes)))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return []SearchModResult{}
	}
	req.Header.Set("User-Agent", ua)
	if token != "" {
		req.Header.Set("Authorization", token)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return []SearchModResult{}
	}
	defer resp.Body.Close()

	var mrResp struct {
		Hits []struct {
			ProjectID   string   `json:"project_id"`
			Slug        string   `json:"slug"`
			Title       string   `json:"title"`
			Description string   `json:"description"`
			IconURL     string   `json:"icon_url"`
			Author      string   `json:"author"`
			Downloads   int64    `json:"downloads"`
			Categories  []string `json:"categories"`
			ClientSide  string   `json:"client_side"`
			ServerSide  string   `json:"server_side"`
		} `json:"hits"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&mrResp); err != nil {
		return []SearchModResult{}
	}

	var results []SearchModResult
	for _, hit := range mrResp.Hits {
		isInstalled := false
		lowerSlug := strings.ToLower(hit.Slug)
		for k := range installed {
			if strings.Contains(k, lowerSlug) {
				isInstalled = true
				break
			}
		}

		results = append(results, SearchModResult{
			ID:          hit.ProjectID,
			Slug:        hit.Slug,
			Title:       hit.Title,
			Description: hit.Description,
			IconURL:     hit.IconURL,
			Author:      hit.Author,
			Downloads:   hit.Downloads,
			Categories:  hit.Categories,
			Platform:    "modrinth",
			ClientSide:  hit.ClientSide,
			ServerSide:  hit.ServerSide,
			Installed:   isInstalled,
		})
	}

	return results
}

func (m *ModOnlineManager) searchCurseForge(ctx context.Context, apiKey, query, loader, mcVersion string, installed map[string]bool) []SearchModResult {
	cfLoaderType := 0
	switch loader {
	case "forge":
		cfLoaderType = 1
	case "fabric":
		cfLoaderType = 4
	case "quilt":
		cfLoaderType = 5
	case "neoforge":
		cfLoaderType = 6
	}

	reqURL := fmt.Sprintf("https://api.curseforge.com/v1/mods/search?gameId=432&classId=6&searchFilter=%s&pageSize=25", url.QueryEscape(query))
	if mcVersion != "" {
		reqURL += fmt.Sprintf("&gameVersion=%s", url.QueryEscape(mcVersion))
	}
	if cfLoaderType > 0 {
		reqURL += fmt.Sprintf("&modLoaderType=%d", cfLoaderType)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return []SearchModResult{}
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return []SearchModResult{}
	}
	defer resp.Body.Close()

	var cfResp struct {
		Data []struct {
			ID      int    `json:"id"`
			Slug    string `json:"slug"`
			Name    string `json:"name"`
			Summary string `json:"summary"`
			Logo    struct {
				ThumbnailURL string `json:"thumbnailUrl"`
			} `json:"logo"`
			Authors []struct {
				Name string `json:"name"`
			} `json:"authors"`
			DownloadCount int64 `json:"downloadCount"`
			Categories    []struct {
				Name string `json:"name"`
			} `json:"categories"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&cfResp); err != nil {
		return []SearchModResult{}
	}

	var results []SearchModResult
	for _, mod := range cfResp.Data {
		author := ""
		if len(mod.Authors) > 0 {
			author = mod.Authors[0].Name
		}
		var cats []string
		for _, c := range mod.Categories {
			cats = append(cats, c.Name)
		}

		isInstalled := false
		lowerSlug := strings.ToLower(mod.Slug)
		for k := range installed {
			if strings.Contains(k, lowerSlug) {
				isInstalled = true
				break
			}
		}

		results = append(results, SearchModResult{
			ID:          fmt.Sprintf("%d", mod.ID),
			Slug:        mod.Slug,
			Title:       mod.Name,
			Description: mod.Summary,
			IconURL:     mod.Logo.ThumbnailURL,
			Author:      author,
			Downloads:   mod.DownloadCount,
			Categories:  cats,
			Platform:    "curseforge",
			Installed:   isInstalled,
		})
	}

	return results
}

func (m *ModOnlineManager) handleVersions(w http.ResponseWriter, r *http.Request, server *storage.Server, slug string) {
	platform := strings.ToLower(r.URL.Query().Get("platform"))
	if platform == "" {
		platform = "modrinth"
	}

	loader := strings.ToLower(r.URL.Query().Get("loader"))
	if loader == "" {
		loader = strings.ToLower(string(server.ModLoader))
	}
	mcVersion := r.URL.Query().Get("mc_version")
	if mcVersion == "" {
		mcVersion = server.MCVersion
	}

	globalSettings, _, _ := m.store.GetGlobalSettings(r.Context())

	if platform == "curseforge" {
		apiKey := ""
		if globalSettings != nil && globalSettings.CFAPIKey != nil {
			apiKey = *globalSettings.CFAPIKey
		}
		m.handleCurseForgeVersions(w, r, apiKey, slug, loader, mcVersion)
	} else {
		token := ""
		ua := "MineServer/1.0 (discopanel-admin)"
		if globalSettings != nil {
			if globalSettings.ModrinthToken != nil && *globalSettings.ModrinthToken != "" {
				token = *globalSettings.ModrinthToken
			}
			if globalSettings.ModrinthUserAgent != nil && *globalSettings.ModrinthUserAgent != "" {
				ua = *globalSettings.ModrinthUserAgent
			}
		}
		m.handleModrinthVersions(w, r, token, ua, slug, loader, mcVersion)
	}
}

func (m *ModOnlineManager) handleModrinthVersions(w http.ResponseWriter, r *http.Request, token, ua, slug, loader, mcVersion string) {
	reqURL := fmt.Sprintf("https://api.modrinth.com/v2/project/%s/version", slug)
	params := url.Values{}
	if loader != "" && loader != "vanilla" {
		loadersJSON, _ := json.Marshal([]string{loader})
		params.Set("loaders", string(loadersJSON))
	}
	if mcVersion != "" {
		versionsJSON, _ := json.Marshal([]string{mcVersion})
		params.Set("game_versions", string(versionsJSON))
	}
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, reqURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("User-Agent", ua)
	if token != "" {
		req.Header.Set("Authorization", token)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to fetch versions: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("modrinth returned %d", resp.StatusCode), resp.StatusCode)
		return
	}

	var rawVersions []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		VersionNumber string `json:"version_number"`
		VersionType   string `json:"version_type"`
		Files         []struct {
			URL      string `json:"url"`
			Filename string `json:"filename"`
			Primary  bool   `json:"primary"`
			Size     int64  `json:"size"`
		} `json:"files"`
		Dependencies []struct {
			ProjectID      *string `json:"project_id"`
			VersionID      *string `json:"version_id"`
			DependencyType string  `json:"dependency_type"`
		} `json:"dependencies"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&rawVersions); err != nil {
		http.Error(w, "failed to parse versions", http.StatusInternalServerError)
		return
	}

	var versions []ModVersionItem
	for _, v := range rawVersions {
		if len(v.Files) == 0 {
			continue
		}

		selectedFile := v.Files[0]
		for _, f := range v.Files {
			if f.Primary {
				selectedFile = f
				break
			}
		}

		var deps []ModDependencyItem
		for _, d := range v.Dependencies {
			pID := ""
			if d.ProjectID != nil {
				pID = *d.ProjectID
			}
			vID := ""
			if d.VersionID != nil {
				vID = *d.VersionID
			}
			deps = append(deps, ModDependencyItem{
				ProjectID:      pID,
				VersionID:      vID,
				DependencyType: d.DependencyType,
			})
		}

		versions = append(versions, ModVersionItem{
			ID:            v.ID,
			VersionNumber: v.VersionNumber,
			Name:          v.Name,
			VersionType:   v.VersionType,
			FileName:      selectedFile.Filename,
			DownloadURL:   selectedFile.URL,
			FileSize:      selectedFile.Size,
			Dependencies:  deps,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"versions": versions,
		"count":    len(versions),
	})
}

func (m *ModOnlineManager) handleCurseForgeVersions(w http.ResponseWriter, r *http.Request, apiKey, modID, loader, mcVersion string) {
	if apiKey == "" {
		http.Error(w, "curseforge api key not configured", http.StatusBadRequest)
		return
	}

	reqURL := fmt.Sprintf("https://api.curseforge.com/v1/mods/%s/files?pageSize=20", modID)
	if mcVersion != "" {
		reqURL += fmt.Sprintf("&gameVersion=%s", url.QueryEscape(mcVersion))
	}
	cfLoaderType := 0
	switch loader {
	case "forge":
		cfLoaderType = 1
	case "fabric":
		cfLoaderType = 4
	case "quilt":
		cfLoaderType = 5
	case "neoforge":
		cfLoaderType = 6
	}
	if cfLoaderType > 0 {
		reqURL += fmt.Sprintf("&modLoaderType=%d", cfLoaderType)
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, reqURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to fetch curseforge files: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("curseforge returned %d", resp.StatusCode), resp.StatusCode)
		return
	}

	var cfFiles struct {
		Data []struct {
			ID           int    `json:"id"`
			DisplayName  string `json:"displayName"`
			FileName     string `json:"fileName"`
			ReleaseType  int    `json:"releaseType"` // 1: release, 2: beta, 3: alpha
			DownloadURL  string `json:"downloadUrl"`
			FileLength   int64  `json:"fileLength"`
			Dependencies []struct {
				ModID        int `json:"modId"`
				RelationType int `json:"relationType"` // 3: required
			} `json:"dependencies"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&cfFiles); err != nil {
		http.Error(w, "failed to parse curseforge files", http.StatusInternalServerError)
		return
	}

	var versions []ModVersionItem
	for _, f := range cfFiles.Data {
		vType := "release"
		if f.ReleaseType == 2 {
			vType = "beta"
		} else if f.ReleaseType == 3 {
			vType = "alpha"
		}

		var deps []ModDependencyItem
		for _, d := range f.Dependencies {
			depType := "optional"
			if d.RelationType == 3 {
				depType = "required"
			}
			deps = append(deps, ModDependencyItem{
				ProjectID:      fmt.Sprintf("%d", d.ModID),
				DependencyType: depType,
			})
		}

		versions = append(versions, ModVersionItem{
			ID:            fmt.Sprintf("%d", f.ID),
			VersionNumber: f.DisplayName,
			Name:          f.DisplayName,
			VersionType:   vType,
			FileName:      f.FileName,
			DownloadURL:   f.DownloadURL,
			FileSize:      f.FileLength,
			Dependencies:  deps,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"versions": versions,
		"count":    len(versions),
	})
}

func (m *ModOnlineManager) handleInstall(w http.ResponseWriter, r *http.Request, server *storage.Server) {
	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	if modsDir == "" {
		http.Error(w, "server does not support mods", http.StatusBadRequest)
		return
	}

	if err := os.MkdirAll(modsDir, 0755); err != nil {
		http.Error(w, fmt.Sprintf("failed to create mods directory: %v", err), http.StatusInternalServerError)
		return
	}

	var req InstallModsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Items) == 0 {
		http.Error(w, "no mods specified to install", http.StatusBadRequest)
		return
	}

	resp := InstallModsResponse{
		Success:   true,
		Installed: make([]string, 0),
		Errors:    make([]string, 0),
	}

	for _, item := range req.Items {
		if item.DownloadURL == "" || item.FileName == "" {
			resp.Errors = append(resp.Errors, fmt.Sprintf("skipped %s: missing url or filename", item.Name))
			continue
		}

		targetPath := filepath.Join(modsDir, item.FileName)

		// Download directly to target file
		res, err := m.httpClient.Get(item.DownloadURL)
		if err != nil {
			resp.Errors = append(resp.Errors, fmt.Sprintf("failed to download %s: %v", item.Name, err))
			continue
		}

		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			resp.Errors = append(resp.Errors, fmt.Sprintf("download %s failed with HTTP %d", item.Name, res.StatusCode))
			continue
		}

		out, err := os.Create(targetPath)
		if err != nil {
			res.Body.Close()
			resp.Errors = append(resp.Errors, fmt.Sprintf("failed to save %s: %v", item.Name, err))
			continue
		}

		_, copyErr := io.Copy(out, res.Body)
		res.Body.Close()
		out.Close()

		if copyErr != nil {
			_ = os.Remove(targetPath)
			resp.Errors = append(resp.Errors, fmt.Sprintf("incomplete download for %s: %v", item.Name, copyErr))
			continue
		}

		resp.Installed = append(resp.Installed, item.FileName)
	}

	if len(resp.Installed) == 0 && len(resp.Errors) > 0 {
		resp.Success = false
		resp.Message = "Failed to install selected mods."
	} else {
		resp.Message = fmt.Sprintf("Successfully installed %d mod(s).", len(resp.Installed))
	}

	writeJSON(w, http.StatusOK, resp)
}
