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
	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"github.com/athNdev/carbon-panel/pkg/upload"
	"github.com/stretchr/testify/require"
)

func newContentStageFixture(t *testing.T, id string, status storage.ServerStatus) (*storage.Store, *storage.Server, *upload.Manager) {
	t.Helper()
	store, err := storage.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "panel.sqlite")},
	})
	require.NoError(t, err)
	require.NoError(t, store.Migrate())
	t.Cleanup(func() { _ = store.Close() })

	server := &storage.Server{
		ID:        id,
		Name:      id,
		DataPath:  t.TempDir(),
		MCVersion: "1.21.1",
		ModLoader: storage.ModLoaderFabric,
		Status:    status,
	}
	require.NoError(t, store.CreateServer(context.Background(), server))

	um := upload.NewManager(t.TempDir(), time.Minute, 10<<20, logger.New())
	t.Cleanup(um.Stop)
	return store, server, um
}

func stagedFiles(t *testing.T, root string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, ".carbon-panel", "staged", "*", "*"))
	require.NoError(t, err)
	return matches
}

func TestStageContentLeavesLiveTreeUntouched(t *testing.T) {
	statuses := []storage.ServerStatus{storage.StatusRunning, storage.StatusPaused, storage.StatusDeepSleep}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			ctx := context.Background()
			store, server, um := newContentStageFixture(t, "stage-"+string(status), status)
			live := filepath.Join(server.DataPath, "server.properties")
			require.NoError(t, os.WriteFile(live, []byte("motd=live\n"), 0o644))
			keep := filepath.Join(server.DataPath, "keep.txt")
			require.NoError(t, os.WriteFile(keep, []byte("stay"), 0o644))

			jar := zipArchiveBytes(t, map[string]string{
				"fabric.mod.json": `{"environment":"server"}`,
			})
			session := uploadSession(t, um, "servermod.jar", jar)
			modSvc := NewModService(store, nil, um, logger.New())
			modRes, err := modSvc.ImportUploadedMod(ctx, connect.NewRequest(&v1.ImportUploadedModRequest{
				ServerId:        server.ID,
				UploadSessionId: session,
				Stage:           true,
			}))
			require.NoError(t, err)
			require.True(t, modRes.Msg.GetStaged())
			mods, err := filepath.Glob(filepath.Join(server.DataPath, "mods", "*"))
			require.NoError(t, err)
			require.Empty(t, mods, "staging a mod must not write the live mods directory")

			pack := zipArchiveBytes(t, map[string]string{"pack.mcmeta": `{"pack":{"pack_format":34}}`})
			packSession := uploadSession(t, um, "my-pack.zip", pack)
			dp := NewDatapackService(store, &config.Config{}, um, logger.New())
			dpRes, err := dp.UploadDatapack(ctx, connect.NewRequest(&v1.UploadDatapackRequest{
				ServerId:        server.ID,
				UploadSessionId: packSession,
				Stage:           true,
			}))
			require.NoError(t, err)
			require.True(t, dpRes.Msg.GetStaged())
			packs, err := filepath.Glob(filepath.Join(server.DataPath, "world", "datapacks", "*"))
			require.NoError(t, err)
			require.Empty(t, packs, "staging a datapack must not write the world")

			files := NewFileService(store, nil, nil, nil, logger.New())
			t.Cleanup(files.Stop)
			fileRes, err := files.UpdateFile(ctx, connect.NewRequest(&v1.UpdateFileRequest{
				ServerId: server.ID,
				Path:     "server.properties",
				Content:  []byte("motd=staged\n"),
				Stage:    true,
			}))
			require.NoError(t, err)
			require.True(t, fileRes.Msg.GetStaged())
			got, err := os.ReadFile(live)
			require.NoError(t, err)
			require.Equal(t, "motd=live\n", string(got))

			_, err = files.DeleteFile(ctx, connect.NewRequest(&v1.DeleteFileRequest{
				ServerId: server.ID,
				Path:     "keep.txt",
				Stage:    true,
			}))
			require.NoError(t, err)
			require.FileExists(t, keep)

			rows, err := store.ListStagedContent(ctx, server.ID)
			require.NoError(t, err)
			require.Len(t, rows, 4)
			require.Len(t, stagedFiles(t, server.DataPath), 3, "a staged delete has no bytes")
			batchID := rows[0].BatchID
			for _, row := range rows {
				require.Equal(t, batchID, row.BatchID, "content joins the one open batch")
			}

			stored, err := store.GetServer(ctx, server.ID)
			require.NoError(t, err)
			require.Equal(t, status, stored.Status)

			require.NoError(t, discardStagedContent(ctx, store, server, rows[0].ID))
			require.Len(t, stagedFiles(t, server.DataPath), 2)
			discarded, err := store.GetStagedContent(ctx, rows[0].ID)
			require.NoError(t, err)
			require.Equal(t, storage.StagedStatusDiscarded, discarded.Status)
			mods, err = filepath.Glob(filepath.Join(server.DataPath, "mods", "*"))
			require.NoError(t, err)
			require.Empty(t, mods)
		})
	}
}

func TestStageContentRejectsBadPayloadsBeforeKeepingBytes(t *testing.T) {
	ctx := context.Background()
	store, server, um := newContentStageFixture(t, "stage-reject", storage.StatusRunning)

	clientJar := zipArchiveBytes(t, map[string]string{
		"fabric.mod.json": `{"environment":"client"}`,
	})
	session := uploadSession(t, um, "clientmod.jar", clientJar)
	_, err := NewModService(store, nil, um, logger.New()).ImportUploadedMod(ctx, connect.NewRequest(&v1.ImportUploadedModRequest{
		ServerId:        server.ID,
		UploadSessionId: session,
		Stage:           true,
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	junk := zipArchiveBytes(t, map[string]string{"readme.txt": "not a pack"})
	junkSession := uploadSession(t, um, "junk.zip", junk)
	_, err = NewDatapackService(store, &config.Config{}, um, logger.New()).UploadDatapack(ctx, connect.NewRequest(&v1.UploadDatapackRequest{
		ServerId:        server.ID,
		UploadSessionId: junkSession,
		Stage:           true,
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	require.Empty(t, stagedFiles(t, server.DataPath))
	rows, err := store.ListStagedContent(ctx, server.ID)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestStageContentRefusesUnreachableRemoteStorage(t *testing.T) {
	ctx := context.Background()
	store, server, _ := newContentStageFixture(t, "stage-remote", storage.StatusDeepSleep)
	require.NoError(t, store.CreateNode(ctx, &storage.Node{
		ID:      "remote-bare",
		Name:    "remote-bare",
		Host:    "tcp://10.0.0.8:2375",
		Enabled: true,
	}))
	server.NodeID = "remote-bare"
	require.NoError(t, store.UpdateServer(ctx, server))

	files := NewFileService(store, nil, nil, nil, logger.New())
	t.Cleanup(files.Stop)
	_, err := files.UpdateFile(ctx, connect.NewRequest(&v1.UpdateFileRequest{
		ServerId: server.ID,
		Path:     "server.properties",
		Content:  []byte("motd=nope\n"),
		Stage:    true,
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err))
	require.Empty(t, stagedFiles(t, server.DataPath))
	_, statErr := os.Stat(filepath.Join(server.DataPath, "server.properties"))
	require.True(t, os.IsNotExist(statErr))
	rows, listErr := store.ListStagedContent(ctx, server.ID)
	require.NoError(t, listErr)
	require.Empty(t, rows)
}

func TestStagedContentJoinsExistingBatchWindow(t *testing.T) {
	ctx := context.Background()
	store, server, _ := newContentStageFixture(t, "stage-batch", storage.StatusPaused)
	require.NoError(t, store.DB().Create(&storage.ServerConfig{ID: server.ID, ServerID: server.ID}).Error)
	when := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	staged, err := store.StageServerConfigUpdate(ctx, server.ID, map[string]any{"motd": "later"}, storage.UpdateWindowAtTime, &when, 0)
	require.NoError(t, err)

	files := NewFileService(store, nil, nil, nil, logger.New())
	t.Cleanup(files.Stop)
	_, err = files.UpdateFile(ctx, connect.NewRequest(&v1.UpdateFileRequest{
		ServerId: server.ID,
		Path:     "ops.json",
		Content:  []byte("{}"),
		Stage:    true,
	}))
	require.NoError(t, err)

	rows, err := store.ListStagedContent(ctx, server.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, staged.BatchID, rows[0].BatchID)

	var batch storage.ServerUpdateBatch
	require.NoError(t, store.DB().Where("id = ?", staged.BatchID).First(&batch).Error)
	require.Equal(t, string(storage.UpdateWindowAtTime), batch.Window)
	require.NotNil(t, batch.ApplyAt)
	require.True(t, batch.ApplyAt.Equal(when))
	_, err = os.Stat(filepath.Join(server.DataPath, "ops.json"))
	require.True(t, os.IsNotExist(err))
}
