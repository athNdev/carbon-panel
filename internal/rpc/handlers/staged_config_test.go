// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) *storage.Store {
	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Path:           fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName),
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	}
	store, err := storage.NewSQLiteStore(cfg)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestStagedConfigHandler_Lifecycle(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	serverID := "srv-handler-test"

	server := &storage.Server{
		ID: serverID, Name: "Handler Test Server", NodeID: "local",
		Port: 25565, Memory: 2048, MaxPlayers: 20, Status: storage.StatusStopped,
	}
	require.NoError(t, store.CreateServer(ctx, server))
	cfg := store.CreateDefaultServerConfig(serverID)
	motd := "initial-motd"
	cfg.MOTD = &motd
	require.NoError(t, store.SaveServerConfig(ctx, cfg))

	h := NewStagedConfigHandler(store, logger.New(), nil, nil)

	// 1. List - initially empty
	req := httptest.NewRequest(http.MethodGet, "/api/v1/staged-config/"+serverID+"/staged", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var listResp struct {
		Changes []*storage.StagedConfigChange `json:"changes"`
		Count   int                           `json:"count"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&listResp))
	assert.Equal(t, 0, listResp.Count)

	// 2. Stage a change
	stageBody := `{"changes":{"motd":"handler-updated-motd"},"apply_mode":"on_restart"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/staged-config/"+serverID+"/staged", bytes.NewBufferString(stageBody))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)
	var stagedChange storage.StagedConfigChange
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&stagedChange))
	assert.NotEmpty(t, stagedChange.ID)
	assert.Equal(t, storage.StagedStatusStaged, stagedChange.Status)

	// 3. Apply the change
	req = httptest.NewRequest(http.MethodPost, "/api/v1/staged-config/"+serverID+"/staged/"+stagedChange.ID+"/apply", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var applyResp struct {
		Success bool                        `json:"success"`
		Change  *storage.StagedConfigChange `json:"change"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&applyResp))
	assert.True(t, applyResp.Success)
	assert.Equal(t, storage.StagedStatusApplied, applyResp.Change.Status)
	assert.Contains(t, applyResp.Change.PreviousPayload, "initial-motd")

	// Verify live config modified
	live, err := store.GetServerConfig(ctx, serverID)
	require.NoError(t, err)
	assert.Equal(t, "handler-updated-motd", *live.MOTD)

	// 4. Rollback the change
	req = httptest.NewRequest(http.MethodPost, "/api/v1/staged-config/"+serverID+"/staged/"+stagedChange.ID+"/rollback", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var rollbackResp struct {
		Success bool                        `json:"success"`
		Change  *storage.StagedConfigChange `json:"change"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&rollbackResp))
	assert.True(t, rollbackResp.Success)
	assert.Equal(t, storage.StagedStatusRolledBack, rollbackResp.Change.Status)
	require.NotNil(t, rollbackResp.Change.RolledBackAt)

	// Verify live config restored
	live, err = store.GetServerConfig(ctx, serverID)
	require.NoError(t, err)
	assert.Equal(t, "initial-motd", *live.MOTD)

	// 5. Rollback again must fail (400 Bad Request)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/staged-config/"+serverID+"/staged/"+stagedChange.ID+"/rollback", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// 6. Stage and Discard
	stageBody2 := `{"changes":{"motd":"will-discard"},"apply_mode":"on_restart"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/staged-config/"+serverID+"/staged", bytes.NewBufferString(stageBody2))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)
	var stagedChange2 storage.StagedConfigChange
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&stagedChange2))

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/staged-config/"+serverID+"/staged/"+stagedChange2.ID, nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}
