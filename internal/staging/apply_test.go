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

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/stretchr/testify/require"
)

func TestDecide(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	shortIdle := now.Add(-5 * time.Minute)
	longIdle := now.Add(-11 * time.Minute)

	if got := Decide(now, storage.ServerUpdateBatch{Window: string(storage.UpdateWindowNextRestart)}, PlayerSample{}); got.Due {
		t.Fatal("next restart is not due from the clock")
	}
	if got := Decide(now, storage.ServerUpdateBatch{Window: string(storage.UpdateWindowAtTime), ApplyAt: &future}, PlayerSample{}); got.Due {
		t.Fatal("a future timestamp is not due")
	}
	if got := Decide(now, storage.ServerUpdateBatch{Window: string(storage.UpdateWindowAtTime), ApplyAt: &past}, PlayerSample{}); !got.Due {
		t.Fatal("a missed timestamp fires once")
	}
	if got := Decide(now, storage.ServerUpdateBatch{Window: string(storage.UpdateWindowIdle), IdleMinutes: 10}, PlayerSample{}); got.Due || got.SetIdle {
		t.Fatal("idle does not start without a player provider")
	}
	reset := Decide(now, storage.ServerUpdateBatch{Window: string(storage.UpdateWindowIdle), IdleMinutes: 10, IdleSince: &shortIdle}, PlayerSample{Known: true, Count: 2})
	if reset.Due || !reset.SetIdle || reset.IdleSince != nil {
		t.Fatalf("a player joining clears idle time, got %+v", reset)
	}
	started := Decide(now, storage.ServerUpdateBatch{Window: string(storage.UpdateWindowIdle), IdleMinutes: 10}, PlayerSample{Known: true, Count: 0})
	if started.Due || !started.SetIdle || started.IdleSince == nil {
		t.Fatalf("the first empty reading starts the idle clock, got %+v", started)
	}
	waiting := Decide(now, storage.ServerUpdateBatch{Window: string(storage.UpdateWindowIdle), IdleMinutes: 10, IdleSince: &shortIdle}, PlayerSample{Known: true, Count: 0})
	if waiting.Due {
		t.Fatal("five minutes of a ten minute window is not enough")
	}
	ready := Decide(now, storage.ServerUpdateBatch{Window: string(storage.UpdateWindowIdle), IdleMinutes: 10, IdleSince: &longIdle}, PlayerSample{Known: true, Count: 0})
	if !ready.Due {
		t.Fatal("idle minutes elapsed")
	}
}

func TestPlanFor(t *testing.T) {
	running := PlanFor(storage.StatusRunning, false)
	if !running.SaveAll || !running.Restart || running.Recreate || !running.StartAfter {
		t.Fatalf("running files restart once, got %+v", running)
	}
	runningCfg := PlanFor(storage.StatusRunning, true)
	if !runningCfg.Recreate || runningCfg.Restart || !runningCfg.StartAfter {
		t.Fatalf("running config recreates, got %+v", runningCfg)
	}
	frozen := PlanFor(storage.StatusPaused, false)
	if frozen.SaveAll || !frozen.Restart || !frozen.StartAfter {
		t.Fatalf("freeze gets a real stop and start, got %+v", frozen)
	}
	asleep := PlanFor(storage.StatusDeepSleep, true)
	if !asleep.Recreate || asleep.StartAfter || asleep.Restart {
		t.Fatalf("deep sleep stays stopped, got %+v", asleep)
	}
	filesOnly := PlanFor(storage.StatusDeepSleep, false)
	if filesOnly.TouchesContainer() {
		t.Fatalf("deep sleep file commit does not start the JVM, got %+v", filesOnly)
	}
}

func TestSweepPublishesDueBatchAndLeavesFutureBatch(t *testing.T) {
	ctx := context.Background()
	store, data := newStageStore(t)
	server := &storage.Server{ID: "due", Name: "due", DataPath: data, Status: storage.StatusDeepSleep}
	require.NoError(t, store.CreateServer(ctx, server))
	require.NoError(t, os.WriteFile(filepath.Join(data, "keep.txt"), []byte("old"), 0o644))

	when := time.Now().Add(-time.Minute).UTC()
	batchID := openBatch(t, store, server.ID, storage.UpdateWindowAtTime, &when, 0)
	stageFile(t, store, data, batchID, server.ID, "keep.txt", "new")

	futureServer := &storage.Server{ID: "later", Name: "later", DataPath: t.TempDir(), Status: storage.StatusRunning}
	require.NoError(t, store.CreateServer(ctx, futureServer))
	later := time.Now().Add(time.Hour).UTC()
	futureBatch := openBatch(t, store, futureServer.ID, storage.UpdateWindowAtTime, &later, 0)
	require.NoError(t, os.WriteFile(filepath.Join(futureServer.DataPath, "keep.txt"), []byte("stay"), 0o644))
	stageFile(t, store, futureServer.DataPath, futureBatch, futureServer.ID, "keep.txt", "nope")

	var published []Plan
	err := Sweep(ctx, store, nil, time.Now(), nil, func(_ context.Context, got *storage.Server, plan Plan) error {
		published = append(published, plan)
		require.Equal(t, "due", got.ID)
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, published, "deep sleep does not restart")

	got, err := os.ReadFile(filepath.Join(data, "keep.txt"))
	require.NoError(t, err)
	require.Equal(t, "new", string(got))
	stored, err := store.GetServer(ctx, server.ID)
	require.NoError(t, err)
	require.Equal(t, storage.StatusDeepSleep, stored.Status)
	rows, err := store.ListStagedContent(ctx, server.ID)
	require.NoError(t, err)
	require.Empty(t, rows)
	open, err := store.OpenUpdateBatch(ctx, server.ID)
	require.NoError(t, err)
	require.Nil(t, open)

	untouched, err := os.ReadFile(filepath.Join(futureServer.DataPath, "keep.txt"))
	require.NoError(t, err)
	require.Equal(t, "stay", string(untouched))
	still, err := store.OpenUpdateBatch(ctx, futureServer.ID)
	require.NoError(t, err)
	require.NotNil(t, still)
}

func TestApplyRollsBackWhenPublishFails(t *testing.T) {
	ctx := context.Background()
	store, data := newStageStore(t)
	server := &storage.Server{ID: "run", Name: "run", DataPath: data, Status: storage.StatusRunning, ContainerID: "c1"}
	require.NoError(t, store.CreateServer(ctx, server))
	require.NoError(t, os.WriteFile(filepath.Join(data, "keep.txt"), []byte("old"), 0o644))
	when := time.Now().Add(-time.Minute).UTC()
	batchID := openBatch(t, store, server.ID, storage.UpdateWindowAtTime, &when, 0)
	stageFile(t, store, data, batchID, server.ID, "keep.txt", "new")

	err := Sweep(ctx, store, nil, time.Now(), nil, func(context.Context, *storage.Server, Plan) error {
		return os.ErrClosed
	})
	require.Error(t, err)
	got, readErr := os.ReadFile(filepath.Join(data, "keep.txt"))
	require.NoError(t, readErr)
	require.Equal(t, "old", string(got))
	open, err := store.OpenUpdateBatch(ctx, server.ID)
	require.NoError(t, err)
	require.NotNil(t, open)
	rows, err := store.ListStagedContent(ctx, server.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestApplyStopsBeforeRestartWhenConfigMoved(t *testing.T) {
	ctx := context.Background()
	store, data := newStageStore(t)
	server := &storage.Server{ID: "moved", Name: "moved", DataPath: data, Status: storage.StatusRunning, ContainerID: "c1"}
	require.NoError(t, store.CreateServer(ctx, server))
	require.NoError(t, store.DB().Create(&storage.ServerConfig{ID: server.ID, ServerID: server.ID}).Error)
	when := time.Now().Add(-time.Minute).UTC()
	_, err := store.StageServerConfigUpdate(ctx, server.ID, map[string]any{"motd": "staged"}, storage.UpdateWindowAtTime, &when, 0)
	require.NoError(t, err)
	require.NoError(t, store.DB().Model(&storage.ServerConfig{}).Where("server_id = ?", server.ID).Update("motd", "someone-else").Error)
	require.NoError(t, os.WriteFile(filepath.Join(data, "keep.txt"), []byte("old"), 0o644))
	batch, err := store.OpenUpdateBatch(ctx, server.ID)
	require.NoError(t, err)
	stageFile(t, store, data, batch.ID, server.ID, "keep.txt", "new")

	published := false
	err = Sweep(ctx, store, nil, time.Now(), nil, func(context.Context, *storage.Server, Plan) error {
		published = true
		return nil
	})
	require.ErrorIs(t, err, storage.ErrStagedConfigMoved)
	require.False(t, published)
	got, readErr := os.ReadFile(filepath.Join(data, "keep.txt"))
	require.NoError(t, readErr)
	require.Equal(t, "old", string(got))
}

func TestIdleClockSurvivesAndDoesNotFireEarly(t *testing.T) {
	ctx := context.Background()
	store, data := newStageStore(t)
	server := &storage.Server{ID: "idle", Name: "idle", DataPath: data, Status: storage.StatusPaused, ContainerID: "c1"}
	require.NoError(t, store.CreateServer(ctx, server))
	batch := openBatch(t, store, server.ID, storage.UpdateWindowIdle, nil, 10)
	stageFile(t, store, data, batch, server.ID, "keep.txt", "later")

	now := time.Now().UTC()
	require.NoError(t, Sweep(ctx, store, nil, now, func(string) (int, bool) { return 0, true }, nil))
	stored, err := store.OpenUpdateBatch(ctx, server.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.IdleSince)
	require.NoFileExists(t, filepath.Join(data, "keep.txt"))

	require.NoError(t, Sweep(ctx, store, nil, now.Add(9*time.Minute), func(string) (int, bool) { return 0, true }, func(context.Context, *storage.Server, Plan) error {
		t.Fatal("idle window fired early")
		return nil
	}))

	var plan Plan
	require.NoError(t, Sweep(ctx, store, nil, now.Add(10*time.Minute), func(string) (int, bool) { return 0, true }, func(_ context.Context, _ *storage.Server, got Plan) error {
		plan = got
		return nil
	}))
	require.True(t, plan.Restart)
	require.True(t, plan.StartAfter)
	require.False(t, plan.SaveAll)
	body, err := os.ReadFile(filepath.Join(data, "keep.txt"))
	require.NoError(t, err)
	require.Equal(t, "later", string(body))
}

func TestNextRestartContentIsNotAppliedByTheSweep(t *testing.T) {
	ctx := context.Background()
	store, data := newStageStore(t)
	server := &storage.Server{ID: "next", Name: "next", DataPath: data, Status: storage.StatusRunning, ContainerID: "c1"}
	require.NoError(t, store.CreateServer(ctx, server))
	batch := openBatch(t, store, server.ID, storage.UpdateWindowNextRestart, nil, 0)
	stageFile(t, store, data, batch, server.ID, "mod.jar", "bytes")

	require.NoError(t, Sweep(ctx, store, nil, time.Now(), nil, func(context.Context, *storage.Server, Plan) error {
		t.Fatal("sweep published a next-restart batch")
		return nil
	}))
	require.NoFileExists(t, filepath.Join(data, "mod.jar"))

	undo, err := CommitNextRestartFiles(ctx, store, server, nil)
	require.NoError(t, err)
	require.NotNil(t, undo)
	body, err := os.ReadFile(filepath.Join(data, "mod.jar"))
	require.NoError(t, err)
	require.Equal(t, "bytes", string(body))
	undo()
	require.NoFileExists(t, filepath.Join(data, "mod.jar"))
	rows, err := store.ListStagedContent(ctx, server.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func newStageStore(t *testing.T) (*storage.Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "panel.sqlite")},
	})
	require.NoError(t, err)
	require.NoError(t, store.Migrate())
	t.Cleanup(func() { _ = store.Close() })
	return store, dir
}

func openBatch(t *testing.T, store *storage.Store, serverID string, window storage.UpdateWindow, at *time.Time, idle int) string {
	t.Helper()
	batch, err := store.AddStagedContent(context.Background(), &storage.StagedContentChange{
		ServerID:   serverID,
		Kind:       storage.StagedKindFile,
		Action:     storage.StagedActionInstall,
		TargetPath: "placeholder",
		StagePath:  ".carbon-panel/staged/placeholder/placeholder",
		FileName:   "placeholder",
	})
	require.NoError(t, err)
	require.NoError(t, store.DB().Where("id = ?", batch.ID).Delete(&storage.StagedContentChange{}).Error)
	require.NoError(t, store.DB().Model(&storage.ServerUpdateBatch{}).Where("id = ?", batch.BatchID).Updates(map[string]any{
		"window":       string(window),
		"apply_at":     at,
		"idle_minutes": idle,
	}).Error)
	return batch.BatchID
}

func stageFile(t *testing.T, store *storage.Store, dataRoot, batchID, serverID, name, body string) {
	t.Helper()
	id := serverID + "-" + name
	stageRel := ".carbon-panel/staged/" + id + "/" + name
	require.NoError(t, os.MkdirAll(filepath.Join(dataRoot, ".carbon-panel", "staged", id), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dataRoot, ".carbon-panel", "staged", id, name), []byte(body), 0o644))
	require.NoError(t, store.DB().Create(&storage.StagedContentChange{
		ID:         id,
		ServerID:   serverID,
		BatchID:    batchID,
		Kind:       storage.StagedKindFile,
		Action:     storage.StagedActionInstall,
		TargetPath: name,
		StagePath:  stageRel,
		FileName:   name,
		Status:     storage.StagedStatusStaged,
	}).Error)
}
