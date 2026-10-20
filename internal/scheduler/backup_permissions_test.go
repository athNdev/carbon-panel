// SPDX-License-Identifier: MPL-2.0

package scheduler

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSecureBackupArtifactRestrictsPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ACLs do not expose Unix permission bits")
	}
	path := filepath.Join(t.TempDir(), "backup.zip")
	if err := os.WriteFile(path, []byte("backup"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := secureBackupArtifact(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("backup permissions = %04o, want 0600", got)
	}
}
