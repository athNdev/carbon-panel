package scheduler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/pkg/logger"
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

	handled, err := recreateForPendingStagedConfig(ctx, store, client, nil, server, true, true)
	require.True(t, handled)
	require.Error(t, err)
	require.NotNil(t, store.server)
	assert.Equal(t, "replacement-id", store.server.ContainerID)
	assert.Equal(t, storage.StatusError, store.server.Status)
	assert.Empty(t, client.starts)
	assert.Nil(t, server.LastStarted, "failed recreate must leave staged recovery pending")
}

func TestPrepareDeepSleepWakeRecreatesWithoutStartingOrAdvancingLastStarted(t *testing.T) {
	ctx := context.Background()
	store := &fakeLifecycleStore{
		fakeStagedStore: &fakeStagedStore{changes: []*storage.StagedConfigChange{{
			ID: "restart-config", ServerID: "srv-1", ApplyMode: storage.StagedApplyOnRestart, Status: storage.StagedStatusStaged,
		}}},
		serverConfig: &storage.ServerConfig{},
	}
	client := &fakeLifecycleDocker{result: &docker.RecreateContainerResult{NewContainerID: "replacement-id"}}
	server := &storage.Server{ID: "srv-1", ContainerID: "old-id", Status: storage.StatusDeepSleep}

	handled, err := recreateForPendingStagedConfig(ctx, store, client, nil, server, false, false)
	require.NoError(t, err)
	require.True(t, handled)
	assert.Equal(t, "replacement-id", server.ContainerID)
	assert.Equal(t, "replacement-id", store.server.ContainerID)
	assert.Empty(t, client.starts, "DeepSleepManager owns the actual start")
	assert.Nil(t, server.LastStarted, "wake preparation must not mark a start before Docker is ready")
}

func TestSchedulerWakePathsRejectOvercommittedNode(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateNode(ctx, &storage.Node{
		ID: "small-node", Name: "Small Node", Host: "tcp://127.0.0.1:2375",
		Enabled: true, Status: storage.NodeStatusOnline,
	}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	server := &storage.Server{
		ID: "oversized-wake", Name: "Oversized Wake", NodeID: "small-node",
		Port: 25565, Memory: 6144, ContainerID: "container-id", Status: storage.StatusDeepSleep,
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}

	dockerAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("Api-Version", "1.45")
			w.Header().Set("Ostype", "linux")
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/info"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"MemTotal":4294967296}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer dockerAPI.Close()
	dockerClient, err := docker.NewClient("tcp://"+strings.TrimPrefix(dockerAPI.URL, "http://"), logger.New())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = dockerClient.Close() }()
	s := NewScheduler(store, dockerClient, nil, nil, nil, logger.New())

	if err := s.PrepareDeepSleepWake(ctx, server); err == nil || !strings.Contains(err.Error(), "lacks memory capacity") {
		t.Fatalf("PrepareDeepSleepWake error = %v, want memory capacity rejection", err)
	}
	if err := s.StartServerForBoot(ctx, server); err == nil || !strings.Contains(err.Error(), "lacks memory capacity") {
		t.Fatalf("StartServerForBoot error = %v, want memory capacity rejection", err)
	}
}
