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
