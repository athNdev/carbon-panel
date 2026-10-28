// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package db

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiffMaps(t *testing.T) {
	current := map[string]any{"a": "1", "b": "2", "c": nil}
	desired := map[string]any{"a": "1", "b": "changed", "d": "new"}

	diff := DiffMaps(current, desired)
	assert.Equal(t, map[string]any{"b": "changed", "d": "new"}, diff)
}

func TestDiffMaps_NoDifferences(t *testing.T) {
	current := map[string]any{"a": "1"}
	assert.Empty(t, DiffMaps(current, map[string]any{"a": "1"}))
}

func TestMergeMaps(t *testing.T) {
	base := map[string]any{"a": "1", "b": "2"}
	merged := MergeMaps(base, map[string]any{"b": "override", "c": "3"})
	assert.Equal(t, map[string]any{"a": "1", "b": "override", "c": "3"}, merged)
	// base must be untouched
	assert.Equal(t, "2", base["b"])
}

func TestValidateCronExpr(t *testing.T) {
	assert.NoError(t, ValidateCronExpr("0 3 * * *"))
	assert.Error(t, ValidateCronExpr(""))
	assert.Error(t, ValidateCronExpr("not-a-cron"))
	assert.Error(t, ValidateCronExpr("0 3 * *")) // 4 fields
}

func TestNextRunAfter(t *testing.T) {
	ref := time.Date(2026, 9, 15, 10, 30, 0, 0, time.UTC)
	next, err := NextRunAfter("0 11 * * *", ref)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC), next.In(time.UTC))

	_, err = NextRunAfter("bogus", ref)
	assert.Error(t, err)
}

func newStagedTestStore(t *testing.T, serverID string) *Store {
	t.Helper()
	ctx := context.Background()
	store := newTestStore(t)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.CreateServer(ctx, &Server{ID: serverID, Name: "staged-test"}))
	cfg := store.CreateDefaultServerConfig(serverID)
	motd := "original"
	cfg.MOTD = &motd
	require.NoError(t, store.SaveServerConfig(ctx, cfg))
	return store
}

func TestStageConfigChange_DiffVsCurrent(t *testing.T) {
	ctx := context.Background()
	store := newStagedTestStore(t, "srv-staged-1")

	// Unchanged value + one real change: only the real change is stored.
	staged, err := store.StageConfigChange(ctx, "srv-staged-1", map[string]any{
		"motd":       "original",
		"difficulty": "hard",
	}, StagedApplyOnRestart, "")
	require.NoError(t, err)
	assert.Equal(t, StagedStatusStaged, staged.Status)
	assert.Equal(t, StagedApplyOnRestart, staged.ApplyMode)
	assert.JSONEq(t, `{"difficulty":"hard"}`, staged.Payload)
}

func TestStageConfigChange_Validation(t *testing.T) {
	ctx := context.Background()
	store := newStagedTestStore(t, "srv-staged-2")

	_, err := store.StageConfigChange(ctx, "srv-staged-2", map[string]any{}, StagedApplyOnRestart, "")
	assert.Error(t, err, "empty changes rejected")

	_, err = store.StageConfigChange(ctx, "srv-staged-2", map[string]any{"motd": "x"}, "sometimes", "")
	assert.Error(t, err, "bad apply mode rejected")

	_, err = store.StageConfigChange(ctx, "srv-staged-2", map[string]any{"motd": "x"}, StagedApplyScheduled, "bogus")
	assert.Error(t, err, "bad cron rejected")

	_, err = store.StageConfigChange(ctx, "srv-staged-2", map[string]any{"motd": "original"}, StagedApplyOnRestart, "")
	assert.Error(t, err, "no-op diff rejected")

	_, err = store.StageConfigChange(ctx, "no-such-server", map[string]any{"motd": "x"}, StagedApplyOnRestart, "")
	assert.Error(t, err, "unknown server rejected")

	// Scheduled mode with a valid cron is accepted.
	staged, err := store.StageConfigChange(ctx, "srv-staged-2", map[string]any{"motd": "midnight"}, StagedApplyScheduled, "0 3 * * *")
	require.NoError(t, err)
	assert.Equal(t, "0 3 * * *", staged.CronExpr)
}

func TestStagedConfigChange_ApplyAndDiscard(t *testing.T) {
	ctx := context.Background()
	store := newStagedTestStore(t, "srv-staged-3")

	staged, err := store.StageConfigChange(ctx, "srv-staged-3", map[string]any{"motd": "staged-value"}, StagedApplyOnRestart, "")
	require.NoError(t, err)

	listed, err := store.ListStagedConfigChanges(ctx, "srv-staged-3", StagedStatusStaged)
	require.NoError(t, err)
	require.Len(t, listed, 1)

	applied, err := store.ApplyStagedConfigChange(ctx, staged.ID)
	require.NoError(t, err)
	assert.Equal(t, StagedStatusApplied, applied.Status)
	require.NotNil(t, applied.AppliedAt)

	live, err := store.GetServerConfig(ctx, "srv-staged-3")
	require.NoError(t, err)
	require.NotNil(t, live.MOTD)
	assert.Equal(t, "staged-value", *live.MOTD)
	assert.Equal(t, "srv-staged-3-config", live.ID, "identity fields preserved")

	// Re-applying is rejected.
	_, err = store.ApplyStagedConfigChange(ctx, staged.ID)
	assert.Error(t, err)

	// Discard path.
	staged2, err := store.StageConfigChange(ctx, "srv-staged-3", map[string]any{"motd": "never"}, StagedApplyOnRestart, "")
	require.NoError(t, err)
	require.NoError(t, store.DiscardStagedConfigChange(ctx, staged2.ID))
	got, err := store.GetStagedConfigChange(ctx, staged2.ID)
	require.NoError(t, err)
	assert.Equal(t, StagedStatusDiscarded, got.Status)

	live, err = store.GetServerConfig(ctx, "srv-staged-3")
	require.NoError(t, err)
	assert.Equal(t, "staged-value", *live.MOTD, "discarded change left live config alone")
}

func TestListDueScheduledStagedChanges(t *testing.T) {
	ctx := context.Background()
	store := newStagedTestStore(t, "srv-staged-4")

	// "* * * * *" fires every minute, so anything staged in the past is due.
	staged, err := store.StageConfigChange(ctx, "srv-staged-4", map[string]any{"motd": "cron"}, StagedApplyScheduled, "* * * * *")
	require.NoError(t, err)

	due, err := store.ListDueScheduledStagedChanges(ctx, time.Now().Add(2*time.Minute))
	require.NoError(t, err)
	require.Len(t, due, 1)
	assert.Equal(t, staged.ID, due[0].ID)

	// Far-future reference before any fire: not due.
	due, err = store.ListDueScheduledStagedChanges(ctx, staged.CreatedAt)
	require.NoError(t, err)
	assert.Empty(t, due)

	// on_restart records are never cron-due.
	_, err = store.StageConfigChange(ctx, "srv-staged-4", map[string]any{"motd": "restart"}, StagedApplyOnRestart, "")
	require.NoError(t, err)
	due, err = store.ListDueScheduledStagedChanges(ctx, time.Now().Add(24*time.Hour))
	require.NoError(t, err)
	for _, d := range due {
		assert.Equal(t, StagedApplyScheduled, d.ApplyMode)
	}
}

func TestStageConfigChange_RejectsUnknownAndIdentityFields(t *testing.T) {
	ctx := context.Background()
	store := newStagedTestStore(t, "srv-staged-invalid-fields")

	_, err := store.StageConfigChange(ctx, "srv-staged-invalid-fields", map[string]any{"doesNotExist": "x"}, StagedApplyOnRestart, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown server config field")

	_, err = store.StageConfigChange(ctx, "srv-staged-invalid-fields", map[string]any{"serverId": "other"}, StagedApplyOnRestart, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "identity field")
}

func TestApplyStagedConfigChange_InvalidPayloadRollsBack(t *testing.T) {
	ctx := context.Background()
	store := newStagedTestStore(t, "srv-staged-corrupt")
	staged, err := store.StageConfigChange(ctx, "srv-staged-corrupt", map[string]any{"motd": "changed"}, StagedApplyOnRestart, "")
	require.NoError(t, err)
	require.NoError(t, store.DB().Model(&StagedConfigChange{}).Where("id = ?", staged.ID).Update("payload", "{"+string([]byte{34})+"unknownField"+string([]byte{34})+":true}").Error)

	_, err = store.ApplyStagedConfigChange(ctx, staged.ID)
	require.Error(t, err)
	got, err := store.GetStagedConfigChange(ctx, staged.ID)
	require.NoError(t, err)
	assert.Equal(t, StagedStatusStaged, got.Status, "failed apply must roll back its claim")
	live, err := store.GetServerConfig(ctx, "srv-staged-corrupt")
	require.NoError(t, err)
	require.NotNil(t, live.MOTD)
	assert.Equal(t, "original", *live.MOTD, "invalid payload must not modify live config")
}

func TestStagedConfigChange_Rollback(t *testing.T) {
	ctx := context.Background()
	store := newStagedTestStore(t, "srv-staged-rollback")

	// Initially live config MOTD is "original".
	staged, err := store.StageConfigChange(ctx, "srv-staged-rollback", map[string]any{"motd": "new-motd"}, StagedApplyOnRestart, "")
	require.NoError(t, err)

	// Rolling back an unapplied (staged) change must fail.
	_, err = store.RollbackStagedConfigChange(ctx, staged.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only applied changes can be rolled back")

	// Apply the staged change.
	applied, err := store.ApplyStagedConfigChange(ctx, staged.ID)
	require.NoError(t, err)
	assert.Equal(t, StagedStatusApplied, applied.Status)
	assert.NotEmpty(t, applied.PreviousPayload)
	assert.Contains(t, applied.PreviousPayload, "original")

	live, err := store.GetServerConfig(ctx, "srv-staged-rollback")
	require.NoError(t, err)
	assert.Equal(t, "new-motd", *live.MOTD)

	// Roll back the applied change.
	rolledBack, err := store.RollbackStagedConfigChange(ctx, staged.ID)
	require.NoError(t, err)
	assert.Equal(t, StagedStatusRolledBack, rolledBack.Status)
	require.NotNil(t, rolledBack.RolledBackAt)

	// Verify live config restored to "original".
	live, err = store.GetServerConfig(ctx, "srv-staged-rollback")
	require.NoError(t, err)
	assert.Equal(t, "original", *live.MOTD)

	// Rolling back again must fail.
	_, err = store.RollbackStagedConfigChange(ctx, staged.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only applied changes can be rolled back")

	// Record with empty PreviousPayload cannot be rolled back.
	stagedEmptyPrev, err := store.StageConfigChange(ctx, "srv-staged-rollback", map[string]any{"motd": "test2"}, StagedApplyOnRestart, "")
	require.NoError(t, err)
	appliedEmptyPrev, err := store.ApplyStagedConfigChange(ctx, stagedEmptyPrev.ID)
	require.NoError(t, err)
	require.NoError(t, store.DB().Model(&StagedConfigChange{}).Where("id = ?", appliedEmptyPrev.ID).Update("previous_payload", "").Error)

	_, err = store.RollbackStagedConfigChange(ctx, appliedEmptyPrev.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no recorded previous state")
}

func TestApplyRefusesFieldThatMovedAfterStaging(t *testing.T) {
	ctx := context.Background()
	store := newStagedTestStore(t, "srv-moved")
	staged, err := store.StageConfigChange(ctx, "srv-moved", map[string]any{"motd": "staged"}, StagedApplyOnRestart, "")
	require.NoError(t, err)

	live, err := store.GetServerConfig(ctx, "srv-moved")
	require.NoError(t, err)
	moved := "edited since"
	live.MOTD = &moved
	require.NoError(t, store.SaveServerConfig(ctx, live))

	_, err = store.ApplyStagedConfigChange(ctx, staged.ID)
	require.ErrorIs(t, err, ErrStagedConfigMoved)
	got, err := store.GetStagedConfigChange(ctx, staged.ID)
	require.NoError(t, err)
	assert.Equal(t, StagedStatusStaged, got.Status)
	live, err = store.GetServerConfig(ctx, "srv-moved")
	require.NoError(t, err)
	assert.Equal(t, "edited since", *live.MOTD)
}

func TestStageServerConfigUpdateSharesOneWindow(t *testing.T) {
	ctx := context.Background()
	store := newStagedTestStore(t, "srv-window")
	first, err := store.StageServerConfigUpdate(ctx, "srv-window", map[string]any{"motd": "soon"}, UpdateWindowNextRestart, nil, 0)
	require.NoError(t, err)
	when := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	second, err := store.StageServerConfigUpdate(ctx, "srv-window", map[string]any{"difficulty": "hard"}, UpdateWindowAtTime, &when, 0)
	require.NoError(t, err)
	assert.Equal(t, first.BatchID, second.BatchID)
	assert.NotEmpty(t, first.BatchID)

	listed, err := store.ListStagedConfigChanges(ctx, "srv-window", StagedStatusStaged)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	for _, row := range listed {
		assert.Equal(t, StagedApplyAtTime, row.ApplyMode)
		require.NotNil(t, row.ApplyAt)
		assert.True(t, row.ApplyAt.Equal(when))
	}
	live, err := store.GetServerConfig(ctx, "srv-window")
	require.NoError(t, err)
	assert.Equal(t, "original", *live.MOTD)

	_, err = store.StageServerConfigUpdate(ctx, "srv-window", map[string]any{"motd": "idle"}, UpdateWindowIdle, nil, 0)
	require.Error(t, err)
}
