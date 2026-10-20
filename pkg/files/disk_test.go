// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package files

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetDiskSpace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "caf"+string(rune(0x00e9)))
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	space, err := GetDiskSpace(dir)
	if err != nil {
		t.Fatalf("GetDiskSpace(%q): %v", dir, err)
	}
	if space <= 0 {
		t.Fatalf("GetDiskSpace(%q) = %d, want positive capacity", dir, space)
	}
}

func TestGetDiskSpaceMissingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	if _, err := GetDiskSpace(path); err == nil {
		t.Fatalf("GetDiskSpace(%q) succeeded for missing path", path)
	}
}
