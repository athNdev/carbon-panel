package nodeagent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackupManager_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	bm := NewBackupManager(tmpDir)

	workloadID := "wl-test-123"
	backupID := "bk-snapshot-456"

	// Set up initial files in workload data directory
	dataDir := filepath.Join(tmpDir, "workloads", workloadID, "data")
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "world", "data"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "server.properties"), []byte("motd=Hello World\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "world", "level.dat"), []byte("dummy-level-data"), 0644))

	// 1. Create Backup
	sizeBytes, sha, err := bm.CreateBackup(workloadID, backupID)
	require.NoError(t, err)
	require.Greater(t, sizeBytes, int64(0))
	require.Len(t, sha, 64)

	// Verify backup file exists
	archivePath := filepath.Join(tmpDir, "workloads", workloadID, "backups", backupID+".tar.gz")
	_, err = os.Stat(archivePath)
	require.NoError(t, err)

	// 2. Mutate data dir (modify one file, add another, delete one)
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "server.properties"), []byte("motd=Corrupted!\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "evil.txt"), []byte("intruder"), 0644))
	require.NoError(t, os.Remove(filepath.Join(dataDir, "world", "level.dat")))

	// 3. Restore Backup
	err = bm.RestoreBackup(workloadID, backupID)
	require.NoError(t, err)

	// Check files restored to original
	props, err := os.ReadFile(filepath.Join(dataDir, "server.properties"))
	require.NoError(t, err)
	require.Equal(t, "motd=Hello World\n", string(props))

	levelDat, err := os.ReadFile(filepath.Join(dataDir, "world", "level.dat"))
	require.NoError(t, err)
	require.Equal(t, "dummy-level-data", string(levelDat))

	// Ensure added file is gone
	_, err = os.Stat(filepath.Join(dataDir, "evil.txt"))
	require.True(t, os.IsNotExist(err))

	// 4. Delete Backup
	err = bm.DeleteBackup(workloadID, backupID)
	require.NoError(t, err)

	_, err = os.Stat(archivePath)
	require.True(t, os.IsNotExist(err))
}

func TestBackupManager_PathValidation(t *testing.T) {
	tmpDir := t.TempDir()
	bm := NewBackupManager(tmpDir)

	_, _, err := bm.CreateBackup("../etc", "bk1")
	require.Error(t, err)

	_, _, err = bm.CreateBackup("wl1", "../hack")
	require.Error(t, err)

	_, _, err = bm.CreateBackup("", "bk1")
	require.Error(t, err)

	err = bm.RestoreBackup("wl1/../../", "bk1")
	require.Error(t, err)

	err = bm.DeleteBackup("wl1", "bk1/../../evil")
	require.Error(t, err)
}
