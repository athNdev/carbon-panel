package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStagedStore is an in-memory stagedConfigStore for hook tests.
type fakeStagedStore struct {
	changes []*storage.StagedConfigChange
	applied []string
	failOn  map[string]bool
}

type fakeLifecycleStore struct {
	*fakeStagedStore
	serverConfig *storage.ServerConfig
	server       *storage.Server
}

func (f *fakeLifecycleStore) GetServerConfig(context.Context, string) (*storage.ServerConfig, error) {
	return f.serverConfig, nil
}

func (f *fakeLifecycleStore) GetGlobalSettings(context.Context) (*storage.ServerConfig, bool, error) {
	return &storage.ServerConfig{}, false, nil
}

func (f *fakeLifecycleStore) UpdateServer(_ context.Context, server *storage.Server) error {
	copy := *server
	f.server = &copy
	return nil
}

func (f *fakeLifecycleStore) ClearEphemeralConfigFields(context.Context, string) error { return nil }

type fakeLifecycleDocker struct {
	result *docker.RecreateContainerResult
	err    error
	starts []string
}

func (f *fakeLifecycleDocker) RecreateContainer(context.Context, string, *storage.Server, *storage.ServerConfig) (*docker.RecreateContainerResult, error) {
	return f.result, f.err
}

func (f *fakeLifecycleDocker) StartContainer(_ context.Context, id string) error {
	f.starts = append(f.starts, id)
	return nil
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

func TestHasPendingRestartStagedConfigRecreate(t *testing.T) {
	ctx := context.Background()
	lastStarted := time.Date(2026, 10, 18, 7, 0, 0, 0, time.UTC)
	appliedAfterStart := lastStarted.Add(time.Second)
	store := &fakeStagedStore{changes: []*storage.StagedConfigChange{
		{ID: "pending", ServerID: "srv-1", ApplyMode: storage.StagedApplyOnRestart, Status: storage.StagedStatusApplied, AppliedAt: &appliedAfterStart},
		{ID: "scheduled", ServerID: "srv-1", ApplyMode: storage.StagedApplyScheduled, Status: storage.StagedStatusApplied, AppliedAt: &appliedAfterStart},
		{ID: "old", ServerID: "srv-1", ApplyMode: storage.StagedApplyOnRestart, Status: storage.StagedStatusApplied, AppliedAt: &lastStarted},
	}}
	pending, err := HasPendingRestartStagedConfigRecreate(ctx, store, "srv-1", &lastStarted)
	require.NoError(t, err)
	assert.True(t, pending)
	pending, err = HasPendingRestartStagedConfigRecreate(ctx, store, "srv-1", nil)
	require.NoError(t, err)
	assert.True(t, pending)
	store.changes = store.changes[1:]
	pending, err = HasPendingRestartStagedConfigRecreate(ctx, store, "srv-1", &lastStarted)
	require.NoError(t, err)
	assert.False(t, pending)
}

func TestRecreateForPendingStagedConfigPersistsPartialContainerID(t *testing.T) {
	ctx := context.Background()
	store := &fakeLifecycleStore{
		fakeStagedStore: &fakeStagedStore{changes: []*storage.StagedConfigChange{{
			ID: "restart-config", ServerID: "srv-1", ApplyMode: storage.StagedApplyOnRestart, Status: storage.StagedStatusStaged,
		}}},
		serverConfig: &storage.ServerConfig{},
	}
	client := &fakeLifecycleDocker{
		result: &docker.RecreateContainerResult{NewContainerID: "replacement-id", WasRunning: true},
		err:    fmt.Errorf("replacement start failed"),
	}
	server := &storage.Server{ID: "srv-1", ContainerID: "old-id", Status: storage.StatusRunning}

	handled, err := recreateForPendingStagedConfig(ctx, store, client, nil, server)
	require.True(t, handled)
	require.Error(t, err)
	require.NotNil(t, store.server)
	assert.Equal(t, "replacement-id", store.server.ContainerID)
	assert.Equal(t, storage.StatusError, store.server.Status)
	assert.Empty(t, client.starts)
	assert.Nil(t, server.LastStarted, "failed recreate must leave staged recovery pending")
}
