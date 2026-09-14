package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/nickheyer/discopanel/internal/packwiz"
	"github.com/nickheyer/discopanel/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackwizHandler_ExtendedFeatures(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "packwiz_handler_ext_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	log := logger.New()
	mgr := packwiz.NewManager(tempDir, log)
	handler := NewPackwizHandler(mgr, nil, log, nil, nil)

	// 1. Create Base Pack
	newPack := packwiz.Pack{
		ID:            "handler-pack",
		Name:          "Handler Pack",
		Author:        "DiscoAdmin",
		Version:       "1.0.0",
		MCVersion:     "1.20.1",
		ModLoader:     "fabric",
		LoaderVersion: "0.15.11",
	}
	body, _ := json.Marshal(newPack)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/packwiz/packs", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusCreated, rr.Code)

	// 2. Add Mod to Pack
	mod := packwiz.ModItem{
		Slug:        "sodium",
		Name:        "Sodium",
		FileName:    "sodium.jar",
		Side:        "both",
		Platform:    "modrinth",
		ProjectID:   "sodium-proj",
		VersionID:   "ver-1",
		DownloadURL: "https://cdn.modrinth.com/sodium.jar",
	}
	modBody, _ := json.Marshal(mod)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/packwiz/packs/handler-pack/mods", bytes.NewReader(modBody))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// 3. Clone Pack
	cloneBody, _ := json.Marshal(map[string]string{"name": "Cloned Handler Pack"})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/packwiz/packs/handler-pack/clone", bytes.NewReader(cloneBody))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusCreated, rr.Code)

	var cloned packwiz.Pack
	err = json.Unmarshal(rr.Body.Bytes(), &cloned)
	require.NoError(t, err)
	assert.Equal(t, "Cloned Handler Pack", cloned.Name)
	assert.NotEqual(t, "handler-pack", cloned.ID)

	// 4. Batch Operations (Pin & Set Side)
	batchBody, _ := json.Marshal(map[string]any{
		"action": "set_side",
		"slugs":  []string{"sodium"},
		"side":   "client",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/packwiz/packs/handler-pack/mods/batch", bytes.NewReader(batchBody))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// 5. Save & List Override Files
	fileBody, _ := json.Marshal(map[string]string{
		"path":    "config/options.txt",
		"content": "fov:90\nmusic:0.0",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/packwiz/packs/handler-pack/files", bytes.NewReader(fileBody))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/packwiz/packs/handler-pack/files", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	var filesResp struct {
		Files []packwiz.PackFileInfo `json:"files"`
	}
	err = json.Unmarshal(rr.Body.Bytes(), &filesResp)
	require.NoError(t, err)
	assert.Len(t, filesResp.Files, 1)
	assert.Equal(t, "config/options.txt", filesResp.Files[0].Path)

	// 6. Refresh Pack
	req = httptest.NewRequest(http.MethodPost, "/api/v1/packwiz/packs/handler-pack/refresh", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// 7. Export Packwiz ZIP
	req = httptest.NewRequest(http.MethodGet, "/api/v1/packwiz/packs/handler-pack/export/packwiz", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Disposition"), "attachment")

	// 8. Import Packwiz Multipart Upload
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", "imported.packwiz.zip")
	require.NoError(t, err)
	_, err = io.Copy(part, bytes.NewReader(rr.Body.Bytes()))
	require.NoError(t, err)
	_ = mw.WriteField("format", "packwiz")
	_ = mw.WriteField("name", "Re-Imported Pack")
	require.NoError(t, mw.Close())

	importReq := httptest.NewRequest(http.MethodPost, "/api/v1/packwiz/packs/import", &buf)
	importReq.Header.Set("Content-Type", mw.FormDataContentType())
	importRr := httptest.NewRecorder()
	handler.ServeHTTP(importRr, importReq)
	assert.Equal(t, http.StatusCreated, importRr.Code)

	var imported packwiz.Pack
	err = json.Unmarshal(importRr.Body.Bytes(), &imported)
	require.NoError(t, err)
	assert.Equal(t, "Re-Imported Pack", imported.Name)

	// 9. Delete File
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/packwiz/packs/handler-pack/files?path=config/options.txt", nil)
	delRr := httptest.NewRecorder()
	handler.ServeHTTP(delRr, delReq)
	assert.Equal(t, http.StatusOK, delRr.Code)
}
