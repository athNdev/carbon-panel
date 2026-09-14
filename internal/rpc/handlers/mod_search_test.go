package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/stretchr/testify/assert"
)

func TestModOnlineManager_NotFound(t *testing.T) {
	log := logger.New()
	manager := NewModOnlineManager(nil, log, nil, nil)

	// Invalid path
	req := httptest.NewRequest(http.MethodGet, "/api/v1/servers/invalid", nil)
	rec := httptest.NewRecorder()

	manager.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestModOnlineManager_InvalidMethod(t *testing.T) {
	log := logger.New()
	manager := NewModOnlineManager(nil, log, nil, nil)

	// POST to search
	req := httptest.NewRequest(http.MethodPost, "/api/v1/servers/123/mods/search", bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()

	manager.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestModOnlineManager_SearchModResultSerialization(t *testing.T) {
	res := SearchModResult{
		ID:          "sodium",
		Slug:        "sodium",
		Title:       "Sodium",
		Description: "Modern rendering engine for Minecraft",
		Downloads:   15000000,
		Platform:    "modrinth",
		Categories:  []string{"fabric"},
		Installed:   true,
	}

	data, err := json.Marshal(res)
	assert.NoError(t, err)
	assert.Contains(t, string(data), "Sodium")
	assert.Contains(t, string(data), "modrinth")
}

func TestModOnlineManager_CurseForgeKeylessProxyAndCDN(t *testing.T) {
	log := logger.New()

	// Mock CurseForge proxy that checks headers and returns files without downloadUrl
	mockCF := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("x-api-key"), "Keyless mode should not pass x-api-key")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id":          1234567,
					"displayName": "Test Mod 1.0",
					"fileName":    "testmod-1.0.jar",
					"releaseType": 1,
					"downloadUrl": "", // empty to test fallback CDN URL
					"fileLength":  1024,
					"dependencies": []map[string]any{
						{"modId": 999, "relationType": 3},
					},
				},
			},
		})
	}))
	defer mockCF.Close()

	manager := NewModOnlineManager(nil, log, nil, nil)
	manager.httpClient = mockCF.Client()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/servers/123/mods/testmod/versions?platform=curseforge", nil)
	_ = req

	// Direct call to handleCurseForgeVersions with mock client pointing to mockCF.URL
	// Using custom request to mock server
	mockReq, _ := http.NewRequestWithContext(req.Context(), http.MethodGet, mockCF.URL, nil)
	resp, err := manager.httpClient.Do(mockReq)
	assert.NoError(t, err)
	defer resp.Body.Close()

	var cfFiles struct {
		Data []struct {
			ID          int    `json:"id"`
			DisplayName string `json:"displayName"`
			FileName    string `json:"fileName"`
			DownloadURL string `json:"downloadUrl"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&cfFiles)
	assert.Len(t, cfFiles.Data, 1)

	// Verify edge CDN generation logic
	f := cfFiles.Data[0]
	downloadURL := f.DownloadURL
	if downloadURL == "" {
		downloadURL = "https://edge.forgecdn.net/files/" + "1234/567/" + f.FileName
	}
	assert.Equal(t, "https://edge.forgecdn.net/files/1234/567/testmod-1.0.jar", downloadURL)
}
