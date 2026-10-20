// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewBackupDownloadPathIsPrivateAndIndependentOfRecordName(t *testing.T) {
	path, cleanup, err := newBackupDownloadPath()
	if err != nil {
		t.Fatalf("newBackupDownloadPath: %v", err)
	}
	defer cleanup()

	dir := filepath.Dir(path)
	if filepath.Base(path) != "archive.zip" {
		t.Fatalf("staging archive name = %q, want archive.zip", filepath.Base(path))
	}
	if info, err := os.Stat(dir); err != nil {
		t.Fatalf("stat staging directory: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("staging path is not a directory: %s", dir)
	}

	if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
		t.Fatalf("write staging archive: %v", err)
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("cleanup left staging directory %s (err=%v)", dir, err)
	}
}
