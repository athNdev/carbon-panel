package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nickheyer/discopanel/pkg/logger"
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
