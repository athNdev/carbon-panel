package scheduler

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createGitRepo(t *testing.T) string {
	repoDir, err := os.MkdirTemp("", "git-remote-*")
	require.NoError(t, err)

	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v failed: %s", args, string(out))
	}

	runGit("init", "-b", "main")
	runGit("config", "user.email", "test@test.com")
	runGit("config", "user.name", "Test")

	// Create initial file
	err = os.MkdirAll(filepath.Join(repoDir, "config"), 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(repoDir, "config", "modpack.json"), []byte(`{"version": "1.0.0"}`), 0644)
	require.NoError(t, err)

	runGit("add", ".")
	runGit("commit", "-m", "Initial commit")

	return repoDir
}

func TestModpackUpdate_CloningStagingAndSync(t *testing.T) {
	remoteRepo := createGitRepo(t)
	defer os.RemoveAll(remoteRepo)

	serverDir, err := os.MkdirTemp("", "server-data-*")
	require.NoError(t, err)
	defer os.RemoveAll(serverDir)

	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	log := logger.New()

	server := &storage.Server{
		ID:       uuid.New().String(),
		Name:     "modpack-server",
		DataPath: serverDir,
		Status:   storage.StatusRunning,
	}
	require.NoError(t, store.CreateServer(ctx, server))

	s := NewScheduler(store, nil, nil, nil, nil, log)

	cfg := ModpackUpdateTaskConfig{
		GitURL:              remoteRepo,
		Branch:              "main",
		RestartImmediately:  false,
		StageConfigUpdates:  true,
	}
	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	task := &storage.ScheduledTask{
		ID:       uuid.New().String(),
		ServerID: server.ID,
		Name:     "git-modpack-sync",
		TaskType: storage.TaskTypeModpackUpdate,
		Config:   string(cfgJSON),
	}

	// 1. Initial Clone & Sync
	out, err := s.executeModpackUpdateTask(ctx, server, task)
	require.NoError(t, err)
	assert.Contains(t, out, "Initial modpack clone successful")

	// Verify file synced to server data dir
	syncedFile := filepath.Join(serverDir, "config", "modpack.json")
	content, err := os.ReadFile(syncedFile)
	require.NoError(t, err)
	assert.Equal(t, `{"version": "1.0.0"}`, string(content))

	// 2. Check no updates when unchanged
	out, err = s.executeModpackUpdateTask(ctx, server, task)
	require.NoError(t, err)
	assert.Contains(t, out, "is up to date at commit")

	// 3. Make a commit in remote repository
	err = os.WriteFile(filepath.Join(remoteRepo, "config", "modpack.json"), []byte(`{"version": "1.1.0"}`), 0644)
	require.NoError(t, err)

	cmd := exec.Command("git", "add", ".")
	cmd.Dir = remoteRepo
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "Bump modpack version to 1.1.0")
	cmd.Dir = remoteRepo
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	require.NoError(t, cmd.Run())

	// 4. Run update task and verify staging + update
	out, err = s.executeModpackUpdateTask(ctx, server, task)
	require.NoError(t, err)
	assert.Contains(t, out, "Updated modpack from commit")

	// Verify server file has new version
	newContent, err := os.ReadFile(syncedFile)
	require.NoError(t, err)
	assert.Equal(t, `{"version": "1.1.0"}`, string(newContent))

	// Verify staging manifest was created
	stagedManifestPath := filepath.Join(serverDir, ".carbon-panel_modpack_staged", "staged_manifest.json")
	manifestBytes, err := os.ReadFile(stagedManifestPath)
	require.NoError(t, err)
	assert.Contains(t, string(manifestBytes), "config/modpack.json")
	assert.Contains(t, string(manifestBytes), `"status": "staged"`)
}

func TestModpackUpdate_MaintenanceWindow(t *testing.T) {
	log := logger.New()
	s := NewScheduler(nil, nil, nil, nil, nil, log)

	now := time.Now()
	// Create a cron expression that runs every minute
	everyMinute := "* * * * *"
	assert.True(t, s.isWithinMaintenanceWindow(everyMinute, now))

	// Invalid cron expression should return false
	assert.False(t, s.isWithinMaintenanceWindow("invalid cron", now))
}
