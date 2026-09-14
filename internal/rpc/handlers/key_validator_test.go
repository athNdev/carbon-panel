package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeyValidator_CurseForge(t *testing.T) {
	log := logger.New()

	// Handler under test (no auth manager needed for open/anonymous mode)
	handler := NewKeyValidatorHandler(nil, nil, log)

	// 1. Test empty key
	reqBody, err := json.Marshal(ValidateKeyRequest{
		Provider: "curseforge",
		APIKey:   "",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-key", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ValidateKeyResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.False(t, resp.Valid)
	assert.Contains(t, resp.Message, "empty")
}

func TestKeyValidator_InvalidMethod(t *testing.T) {
	log := logger.New()
	handler := NewKeyValidatorHandler(nil, nil, log)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/validate-key", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestKeyValidator_UnsupportedProvider(t *testing.T) {
	log := logger.New()
	handler := NewKeyValidatorHandler(nil, nil, log)

	reqBody, err := json.Marshal(ValidateKeyRequest{
		Provider: "unknown-provider",
		APIKey:   "secret",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-key", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ValidateKeyResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.False(t, resp.Valid)
	assert.Contains(t, resp.Message, "Unsupported provider")
}
