package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/nickheyer/discopanel/internal/packwiz"
	"github.com/nickheyer/discopanel/pkg/logger"
	"github.com/stretchr/testify/assert"
)

func TestPackwizHandler_CRUD(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "packwiz_handler_test_*")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	log := logger.New()
	mgr := packwiz.NewManager(tempDir, log)
	handler := NewPackwizHandler(mgr, nil, log, nil, nil)

	// 1. List Packs (initially empty)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/packwiz/packs", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	var listResp struct {
		Packs []packwiz.PackSummary `json:"packs"`
	}
	err = json.Unmarshal(rr.Body.Bytes(), &listResp)
	assert.NoError(t, err)
	assert.Empty(t, listResp.Packs)

	// 2. Create Pack
	newPack := packwiz.Pack{
		ID:            "my-studio-pack",
		Name:          "Studio Modpack",
		Author:        "DiscoAdmin",
		Version:       "1.0.0",
		MCVersion:     "1.20.1",
		ModLoader:     "fabric",
		LoaderVersion: "0.15.11",
	}
	body, _ := json.Marshal(newPack)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/packwiz/packs", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusCreated, rr.Code)

	// 3. Get Pack
	req = httptest.NewRequest(http.MethodGet, "/api/v1/packwiz/packs/my-studio-pack", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	var fetched packwiz.Pack
	err = json.Unmarshal(rr.Body.Bytes(), &fetched)
	assert.NoError(t, err)
	assert.Equal(t, "Studio Modpack", fetched.Name)

	// 4. Add Mod
	mod := packwiz.ModItem{
		Slug:        "lithium",
		Name:        "Lithium",
		FileName:    "lithium-fabric-mc1.20.1-0.11.2.jar",
		Side:        "both",
		Platform:    "modrinth",
		ProjectID:   "gvQqBUqZ",
		VersionID:   "abc123",
		DownloadURL: "https://cdn.modrinth.com/data/gvQqBUqZ/versions/abc123/lithium-fabric-mc1.20.1-0.11.2.jar",
	}
	modBody, _ := json.Marshal(mod)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/packwiz/packs/my-studio-pack/mods", bytes.NewReader(modBody))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// 5. Update Mod (Side & Pinned)
	updateModBody, _ := json.Marshal(map[string]any{"side": "server", "pinned": true})
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/packwiz/packs/my-studio-pack/mods/lithium", bytes.NewReader(updateModBody))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// 6. Serve raw pack.toml
	req = httptest.NewRequest(http.MethodGet, "/api/v1/packwiz/my-studio-pack/pack.toml", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "Studio Modpack")

	// 7. Delete Mod
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/packwiz/packs/my-studio-pack/mods/lithium", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// 8. Delete Pack
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/packwiz/packs/my-studio-pack", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestPackwizHandler_LoaderVersions(t *testing.T) {
	log := logger.New()
	handler := NewPackwizHandler(nil, nil, log, nil, nil)

	loaders := []string{"fabric", "forge", "neoforge", "quilt"}
	for _, l := range loaders {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/packwiz/loaders/"+l+"/versions?game_version=1.20.1", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)

		var resp struct {
			Loader      string   `json:"loader"`
			GameVersion string   `json:"game_version"`
			Versions    []string `json:"versions"`
		}
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Equal(t, l, resp.Loader)
		assert.Equal(t, "1.20.1", resp.GameVersion)
		assert.NotEmpty(t, resp.Versions)
		assert.Equal(t, "latest", resp.Versions[0])
	}
}
