// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	storage "github.com/athNdev/carbon-panel/internal/db"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"github.com/stretchr/testify/require"
)

func TestUpdateBatchListsAndDiscardsWithoutTouchingLiveFiles(t *testing.T) {
	ctx := context.Background()
	svc, store := setupConfigTestService(t)
	data := t.TempDir()
	server := &storage.Server{ID: "batch-ui", Name: "batch-ui", DataPath: data, Status: storage.StatusRunning}
	require.NoError(t, store.CreateServer(ctx, server))
	require.NoError(t, store.DB().Create(&storage.ServerConfig{ID: server.ID, ServerID: server.ID}).Error)
	require.NoError(t, os.WriteFile(filepath.Join(data, "keep.txt"), []byte("live"), 0o644))

	when := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	staged, err := store.StageServerConfigUpdate(ctx, server.ID, map[string]any{"motd": "later"}, storage.UpdateWindowAtTime, &when, 0)
	require.NoError(t, err)
	content, err := stageContentBytes(ctx, store, server, storage.StagedKindFile, storage.StagedActionReplace, "keep.txt", "keep.txt", []byte("staged"))
	require.NoError(t, err)

	got, err := svc.GetUpdateBatch(ctx, connect.NewRequest(&v1.GetUpdateBatchRequest{ServerId: server.ID}))
	require.NoError(t, err)
	require.True(t, got.Msg.GetOpen())
	require.Equal(t, v1.ConfigApplyWindow_CONFIG_APPLY_WINDOW_AT_TIME, got.Msg.GetWindow())
	require.Equal(t, when.Unix(), got.Msg.GetApplyAtUnix())
	require.Len(t, got.Msg.GetItems(), 2)

	_, err = svc.DiscardUpdateBatch(ctx, connect.NewRequest(&v1.DiscardUpdateBatchRequest{
		ServerId: server.ID,
		ItemId:   content.ID,
	}))
	require.NoError(t, err)
	body, err := os.ReadFile(filepath.Join(data, "keep.txt"))
	require.NoError(t, err)
	require.Equal(t, "live", string(body))
	require.Empty(t, stagedFiles(t, data))

	_, err = svc.DiscardUpdateBatch(ctx, connect.NewRequest(&v1.DiscardUpdateBatchRequest{
		ServerId: server.ID,
		ItemId:   staged.ID,
	}))
	require.NoError(t, err)
	after, err := svc.GetUpdateBatch(ctx, connect.NewRequest(&v1.GetUpdateBatchRequest{ServerId: server.ID}))
	require.NoError(t, err)
	require.False(t, after.Msg.GetOpen())
}
