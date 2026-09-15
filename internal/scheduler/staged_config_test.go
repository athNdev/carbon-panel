package scheduler

import (
	"context"
	"fmt"
	"testing"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStagedStore is an in-memory stagedConfigStore for hook tests.
type fakeStagedStore struct {
	changes []*storage.StagedConfigChange
	applied []string
	failOn  map[string]bool
}

func (f *fakeStagedStore) ListStagedConfigChanges(_ context.Context, serverID string, status storage.StagedStatus) ([]*storage.StagedConfigChange, error) {
	var out []*storage.StagedConfigChange
	for _, c := range f.changes {
		if c.ServerID == serverID && (status == "" || c.Status == status) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeStagedStore) ApplyStagedConfigChange(_ context.Context, id string) (*storage.StagedConfigChange, error) {
	if f.failOn[id] {
		return nil, fmt.Errorf("boom")
	}
	for _, c := range f.changes {
		if c.ID == id {
			c.Status = storage.StagedStatusApplied
			f.applied = append(f.applied, id)
			return c, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func TestApplyOnRestartStagedConfigs_OnlyOnRestart(t *testing.T) {
	ctx := context.Background()
	store := &fakeStagedStore{changes: []*storage.StagedConfigChange{
		{ID: "a", ServerID: "srv-1", ApplyMode: storage.StagedApplyOnRestart, Status: storage.StagedStatusStaged},
		{ID: "b", ServerID: "srv-1", ApplyMode: storage.StagedApplyScheduled, Status: storage.StagedStatusStaged},
		{ID: "c", ServerID: "srv-1", ApplyMode: storage.StagedApplyOnRestart, Status: storage.StagedStatusApplied},
		{ID: "d", ServerID: "srv-2", ApplyMode: storage.StagedApplyOnRestart, Status: storage.StagedStatusStaged},
	}}

	n, err := ApplyOnRestartStagedConfigs(ctx, store, "srv-1")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []string{"a"}, store.applied)
}

func TestApplyOnRestartStagedConfigs_StopsOnError(t *testing.T) {
	ctx := context.Background()
	store := &fakeStagedStore{
		changes: []*storage.StagedConfigChange{
			{ID: "a", ServerID: "srv-1", ApplyMode: storage.StagedApplyOnRestart, Status: storage.StagedStatusStaged},
			{ID: "b", ServerID: "srv-1", ApplyMode: storage.StagedApplyOnRestart, Status: storage.StagedStatusStaged},
		},
		failOn: map[string]bool{"a": true},
	}

	n, err := ApplyOnRestartStagedConfigs(ctx, store, "srv-1")
	assert.Error(t, err)
	assert.Equal(t, 0, n)
}
