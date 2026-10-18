// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package services

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func setupConfigTestService(t *testing.T) (*ConfigService, *storage.Store) {
	t.Helper()

	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	store, err := storage.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:           fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName),
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return NewConfigService(store, &config.Config{}, nil, nil, logger.New()), store
}

type configRecreatorFake struct {
	result *docker.RecreateContainerResult
	err    error
}

func (f configRecreatorFake) RecreateContainer(context.Context, string, *storage.Server, *storage.ServerConfig) (*docker.RecreateContainerResult, error) {
	return f.result, f.err
}

func TestConfigRecreatePersistsPartialReplacementOnStartFailure(t *testing.T) {
	ctx := context.Background()
	_, store := setupConfigTestService(t)
	server := &storage.Server{
		ID: "config-recreate-test", Name: "Config Recreate", NodeID: "node-1",
		Port: 25565, Memory: 2048, ContainerID: "old-container", Status: storage.StatusRunning,
	}
	require.NoError(t, store.CreateServer(ctx, server))
	dockerErr := fmt.Errorf("replacement failed to start")
	recreator := configRecreatorFake{
		result: &docker.RecreateContainerResult{NewContainerID: "replacement-container", WasRunning: true},
		err:    dockerErr,
	}

	err := recreateServerContainerWithClient(ctx, store, recreator, logger.New(), server, &storage.ServerConfig{ServerID: server.ID})
	require.ErrorIs(t, err, dockerErr)
	updated, getErr := store.GetServer(ctx, server.ID)
	require.NoError(t, getErr)
	assert.Equal(t, "replacement-container", updated.ContainerID)
	assert.Equal(t, storage.StatusError, updated.Status)
}

func TestConfigRecreateClearsStaleContainerIDWhenReplacementIsAbsent(t *testing.T) {
	ctx := context.Background()
	_, store := setupConfigTestService(t)
	server := &storage.Server{
		ID: "config-recreate-empty", Name: "Config Recreate Empty", NodeID: "node-1",
		Port: 25566, Memory: 2048, ContainerID: "old-container", Status: storage.StatusRunning,
	}
	require.NoError(t, store.CreateServer(ctx, server))

	err := recreateServerContainerWithClient(ctx, store, configRecreatorFake{err: fmt.Errorf("create failed")}, logger.New(), server, &storage.ServerConfig{ServerID: server.ID})
	require.Error(t, err)
	updated, getErr := store.GetServer(ctx, server.ID)
	require.NoError(t, getErr)
	assert.Empty(t, updated.ContainerID)
	assert.Equal(t, storage.StatusError, updated.Status)
}

func TestConfigRecreateRetainsOriginalContainerIDWhenStopFails(t *testing.T) {
	ctx := context.Background()
	_, store := setupConfigTestService(t)
	server := &storage.Server{
		ID: "config-recreate-stop-failure", Name: "Config Recreate Stop Failure", NodeID: "node-1",
		Port: 25567, Memory: 2048, ContainerID: "old-container", Status: storage.StatusRunning,
	}
	require.NoError(t, store.CreateServer(ctx, server))

	err := recreateServerContainerWithClient(ctx, store, configRecreatorFake{
		result: &docker.RecreateContainerResult{NewContainerID: "old-container", WasRunning: true},
		err:    fmt.Errorf("docker stop failed"),
	}, logger.New(), server, &storage.ServerConfig{ServerID: server.ID})
	require.Error(t, err)
	updated, getErr := store.GetServer(ctx, server.ID)
	require.NoError(t, getErr)
	assert.Equal(t, "old-container", updated.ContainerID)
	assert.Equal(t, storage.StatusError, updated.Status)
}

func TestConfigService_GetGlobalSettings(t *testing.T) {
	svc, _ := setupConfigTestService(t)

	res, err := svc.GetGlobalSettings(context.Background(), connect.NewRequest(&v1.GetGlobalSettingsRequest{}))
	require.NoError(t, err)
	assert.NotEmpty(t, res.Msg.Categories)
}

func TestConfigService_GetServerConfig_NotFound(t *testing.T) {
	svc, _ := setupConfigTestService(t)

	_, err := svc.GetServerConfig(context.Background(), connect.NewRequest(&v1.GetServerConfigRequest{
		ServerId: "no-such-server",
	}))
	require.Error(t, err)
	connectErr, ok := err.(*connect.Error)
	require.True(t, ok, "expected connect error, got %T", err)
	assert.Equal(t, connect.CodeNotFound, connectErr.Code())
}

func TestApplyConfigUpdates(t *testing.T) {
	type sample struct {
		Name    string  `json:"name"`
		Port    int     `json:"port"`
		Enabled *bool   `json:"enabled"`
		Ratio   float64 `json:"ratio"`
	}

	cfg := &sample{}
	require.NoError(t, applyConfigUpdates(cfg, map[string]string{
		"name":    "srv",
		"port":    "25565",
		"enabled": "true",
		"ratio":   "1.5",
		"unknown": "ignored",
	}))
	assert.Equal(t, "srv", cfg.Name)
	assert.Equal(t, 25565, cfg.Port)
	require.NotNil(t, cfg.Enabled)
	assert.True(t, *cfg.Enabled)
	assert.Equal(t, 1.5, cfg.Ratio)

	// Empty string unsets (zeroes) the field.
	require.NoError(t, applyConfigUpdates(cfg, map[string]string{"name": "", "enabled": ""}))
	assert.Equal(t, "", cfg.Name)
	assert.Nil(t, cfg.Enabled)

	// Bad values are errors, not silent coercions.
	assert.Error(t, applyConfigUpdates(cfg, map[string]string{"port": "not-a-number"}))
}

func TestGetSelectOptionsAndCategoryIndex(t *testing.T) {
	assert.Equal(t, []string{"peaceful", "easy", "normal", "hard"}, getSelectOptions("difficulty"))
	assert.Empty(t, getSelectOptions("no-such-key"))

	assert.Equal(t, 0, getCategoryIndex("memory"))
	assert.Equal(t, -1, getCategoryIndex("no-such-key"))
}
