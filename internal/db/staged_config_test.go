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
	t.Cleanup(func() { store.Close() })

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
