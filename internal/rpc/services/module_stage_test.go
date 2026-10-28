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
	"github.com/stretchr/testify/require"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestUpdateModuleStageLeavesTheLiveModule(t *testing.T) {
	ctx := context.Background()
	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	store, err := storage.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:           fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName),
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	data := t.TempDir()
	server := &storage.Server{ID: "mod-stage", Name: "mod-stage", DataPath: data, Status: storage.StatusRunning}
	require.NoError(t, store.CreateServer(ctx, server))
	require.NoError(t, store.DB().Create(&storage.Module{
		ID: "map", Name: "map", ServerID: server.ID, TemplateID: "tpl", Status: "stopped", Config: "old",
	}).Error)

	svc := NewModuleService(store, nil, nil, nil, nil, nil, nil, logger.New())
	next := "new"
	staged, err := svc.UpdateModule(ctx, connect.NewRequest(&v1.UpdateModuleRequest{
		Id:     "map",
		Config: &next,
		Stage:  true,
	}))
	require.NoError(t, err)
	require.True(t, staged.Msg.GetStaged())
	require.Equal(t, "old", staged.Msg.GetModule().GetConfig())

	live, err := store.GetModule(ctx, "map")
	require.NoError(t, err)
	require.Equal(t, "old", live.Config)
	batch, err := store.OpenUpdateBatch(ctx, server.ID)
	require.NoError(t, err)
	require.NotNil(t, batch)
	rows, err := store.ListBatchContent(ctx, batch.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, storage.StagedKindModule, rows[0].Kind)
}
