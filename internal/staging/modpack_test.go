// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package staging

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/stretchr/testify/require"
)

func TestStageModpackWaitsForTheWindowAndJoinsConfig(t *testing.T) {
	ctx := context.Background()
	store, data := newStageStore(t)
	server := &storage.Server{ID: "pack", Name: "pack", DataPath: data, Status: storage.StatusRunning, ContainerID: "c1"}
	require.NoError(t, store.CreateServer(ctx, server))
	require.NoError(t, store.DB().Create(&storage.ServerConfig{ID: server.ID, ServerID: server.ID}).Error)
	require.NoError(t, store.DB().Create(&storage.Module{
		ID: "map", Name: "map", ServerID: server.ID, TemplateID: "tpl", Status: "stopped", Config: "old", Memory: 512,
	}).Error)

	when := time.Now().Add(-time.Minute).UTC()
	_, err := store.StageServerConfigUpdate(ctx, server.ID, map[string]any{"motd": "later"}, storage.UpdateWindowAtTime, &when, 0)
	require.NoError(t, err)

	src := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(src, "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "config", "modpack.json"), []byte(`{"version":"2"}`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(src, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644))

	row, err := StageModpackTree(ctx, store, server, src, "modpack abcdef12")
	require.NoError(t, err)
	require.Equal(t, storage.StagedKindModpack, row.Kind)
	require.NoFileExists(t, filepath.Join(data, "config", "modpack.json"))
	require.NoFileExists(t, filepath.Join(data, ".git", "HEAD"))

	_, err = StageModuleChange(ctx, store, server, "map", "module map", true, map[string]ModuleField{
		"config": {Base: "old", Value: "new"},
		"memory": {Base: 512, Value: 1024},
	})
	require.NoError(t, err)
	live, err := store.GetModule(ctx, "map")
	require.NoError(t, err)
	require.Equal(t, "old", live.Config)
	require.Equal(t, 512, live.Memory)

	var published []Plan
	err = Sweep(ctx, store, nil, time.Now(), nil, func(_ context.Context, got *storage.Server, plan Plan) error {
		published = append(published, plan)
		require.Equal(t, server.ID, got.ID)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, published, 1)
	require.True(t, published[0].Recreate)
	require.True(t, published[0].StartAfter)
	require.Equal(t, []string{"map"}, published[0].ModuleIDs)

	body, err := os.ReadFile(filepath.Join(data, "config", "modpack.json"))
	require.NoError(t, err)
	require.Equal(t, `{"version":"2"}`, string(body))
	require.NoFileExists(t, filepath.Join(data, ".git", "HEAD"))
	cfg, err := store.GetServerConfig(ctx, server.ID)
	require.NoError(t, err)
	require.NotNil(t, cfg.MOTD)
	require.Equal(t, "later", *cfg.MOTD)
	live, err = store.GetModule(ctx, "map")
	require.NoError(t, err)
	require.Equal(t, "new", live.Config)
	require.Equal(t, 1024, live.Memory)
	open, err := store.OpenUpdateBatch(ctx, server.ID)
	require.NoError(t, err)
	require.Nil(t, open)
}

func TestStageModpackOnDeepSleepDoesNotStart(t *testing.T) {
	ctx := context.Background()
	store, data := newStageStore(t)
	server := &storage.Server{ID: "asleep", Name: "asleep", DataPath: data, Status: storage.StatusDeepSleep}
	require.NoError(t, store.CreateServer(ctx, server))
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "keep.txt"), []byte("staged"), 0o644))
	_, err := StageModpackTree(ctx, store, server, src, "modpack")
	require.NoError(t, err)
	when := time.Now().Add(-time.Minute).UTC()
	require.NoError(t, store.DB().Model(&storage.ServerUpdateBatch{}).Where("server_id = ?", server.ID).Updates(map[string]any{
		"window":   string(storage.UpdateWindowAtTime),
		"apply_at": when,
	}).Error)

	published := false
	err = Sweep(ctx, store, nil, time.Now(), nil, func(context.Context, *storage.Server, Plan) error {
		published = true
		return nil
	})
	require.NoError(t, err)
	require.False(t, published)
	body, err := os.ReadFile(filepath.Join(data, "keep.txt"))
	require.NoError(t, err)
	require.Equal(t, "staged", string(body))
	stored, err := store.GetServer(ctx, server.ID)
	require.NoError(t, err)
	require.Equal(t, storage.StatusDeepSleep, stored.Status)
}

func TestStageModuleOnDeepSleepDoesNotStartTheServer(t *testing.T) {
	ctx := context.Background()
	store, data := newStageStore(t)
	server := &storage.Server{ID: "asleep-mod", Name: "asleep-mod", DataPath: data, Status: storage.StatusDeepSleep, ContainerID: "c1"}
	require.NoError(t, store.CreateServer(ctx, server))
	require.NoError(t, store.DB().Create(&storage.Module{
		ID: "map", Name: "map", ServerID: server.ID, TemplateID: "tpl", Status: "stopped", EnvOverrides: "A=1", Memory: 256,
	}).Error)
	when := time.Now().Add(-time.Minute).UTC()
	_ = openBatch(t, store, server.ID, storage.UpdateWindowAtTime, &when, 0)
	_, err := StageModuleChange(ctx, store, server, "map", "module map", true, map[string]ModuleField{
		"env_overrides": {Base: "A=1", Value: "A=2"},
		"memory":        {Base: 256, Value: 128},
	})
	require.NoError(t, err)

	var published []Plan
	err = Sweep(ctx, store, nil, time.Now(), nil, func(_ context.Context, got *storage.Server, plan Plan) error {
		published = append(published, plan)
		require.Equal(t, server.ID, got.ID)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, published, 1)
	require.False(t, published[0].StartAfter)
	require.False(t, published[0].Restart)
	require.Equal(t, []string{"map"}, published[0].ModuleIDs)
	live, err := store.GetModule(ctx, "map")
	require.NoError(t, err)
	require.Equal(t, "A=2", live.EnvOverrides)
	require.Equal(t, 128, live.Memory)
	stored, err := store.GetServer(ctx, server.ID)
	require.NoError(t, err)
	require.Equal(t, storage.StatusDeepSleep, stored.Status)
}

func TestStageModpackPublishFailureRestoresTheTreeAndModule(t *testing.T) {
	ctx := context.Background()
	store, data := newStageStore(t)
	server := &storage.Server{ID: "fail", Name: "fail", DataPath: data, Status: storage.StatusRunning, ContainerID: "c1"}
	require.NoError(t, store.CreateServer(ctx, server))
	require.NoError(t, os.WriteFile(filepath.Join(data, "keep.txt"), []byte("old"), 0o644))
	require.NoError(t, store.DB().Create(&storage.Module{
		ID: "map", Name: "map", ServerID: server.ID, TemplateID: "tpl", Status: "stopped", Config: "old",
	}).Error)
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "keep.txt"), []byte("new"), 0o644))
	when := time.Now().Add(-time.Minute).UTC()
	batchID := openBatch(t, store, server.ID, storage.UpdateWindowAtTime, &when, 0)
	_, err := StageModpackTree(ctx, store, server, src, "modpack")
	require.NoError(t, err)
	_, err = StageModuleChange(ctx, store, server, "map", "module map", true, map[string]ModuleField{
		"config": {Base: "old", Value: "new"},
	})
	require.NoError(t, err)
	_ = batchID

	err = Sweep(ctx, store, nil, time.Now(), nil, func(context.Context, *storage.Server, Plan) error {
		return os.ErrClosed
	})
	require.Error(t, err)
	body, err := os.ReadFile(filepath.Join(data, "keep.txt"))
	require.NoError(t, err)
	require.Equal(t, "old", string(body))
	live, err := store.GetModule(ctx, "map")
	require.NoError(t, err)
	require.Equal(t, "old", live.Config)
	open, err := store.OpenUpdateBatch(ctx, server.ID)
	require.NoError(t, err)
	require.NotNil(t, open)
}

func TestStageModpackRefusesASymlink(t *testing.T) {
	ctx := context.Background()
	store, data := newStageStore(t)
	server := &storage.Server{ID: "link", Name: "link", DataPath: data, Status: storage.StatusRunning}
	require.NoError(t, store.CreateServer(ctx, server))
	src := t.TempDir()
	target := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(target, []byte("secret"), 0o644))
	if err := os.Symlink(target, filepath.Join(src, "linked.txt")); err != nil {
		t.Skip("symlink is not available")
	}
	_, err := StageModpackTree(ctx, store, server, src, "modpack")
	require.Error(t, err)
	require.NoFileExists(t, filepath.Join(data, "linked.txt"))
	open, err := store.OpenUpdateBatch(ctx, server.ID)
	require.NoError(t, err)
	require.Nil(t, open)
}
