// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package packwiz

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

func TestPrimaryFileName(t *testing.T) {
	newFiles := func() []struct {
		URL      string `json:"url"`
		FileName string `json:"filename"`
		Primary  bool   `json:"primary"`
		Size     int64  `json:"size"`
	} {
		return nil
	}

	url, name, size := primaryFileName(newFiles())
	if url != "" || name != "" || size != 0 {
		t.Errorf("empty = %q %q %d", url, name, size)
	}

	multi := []struct {
		URL      string `json:"url"`
		FileName string `json:"filename"`
		Primary  bool   `json:"primary"`
		Size     int64  `json:"size"`
	}{
		{URL: "u1", FileName: "a.jar", Size: 1},
		{URL: "u2", FileName: "b.jar", Primary: true, Size: 2},
	}
	url, name, size = primaryFileName(multi)
	if url != "u2" || name != "b.jar" || size != 2 {
		t.Errorf("primary pick = %q %q %d", url, name, size)
	}

	single := []struct {
		URL      string `json:"url"`
		FileName string `json:"filename"`
		Primary  bool   `json:"primary"`
		Size     int64  `json:"size"`
	}{
		{URL: "u1", FileName: "a.jar", Size: 1},
	}
	url, name, size = primaryFileName(single)
	if url != "u1" || name != "a.jar" || size != 1 {
		t.Errorf("single pick = %q %q %d", url, name, size)
	}
}

func TestListPacks_EmptyAndMissing(t *testing.T) {
	m := NewManager(t.TempDir(), logger.New())

	listed, err := m.ListPacks()
	if err != nil || len(listed) != 0 {
		t.Errorf("empty dir = %v, %v", listed, err)
	}

	missing := NewManager(filepath.Join(t.TempDir(), "nope"), logger.New())
	listed, err = missing.ListPacks()
	if err != nil || len(listed) != 0 {
		t.Errorf("missing dir = %v, %v", listed, err)
	}

	// Plain files are skipped; unreadable pack dirs are skipped, not fatal.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "brokenpack"), 0o755); err != nil {
		t.Fatal(err)
	}
	m2 := NewManager(dir, logger.New())
	listed, err = m2.ListPacks()
	if err != nil || len(listed) != 0 {
		t.Errorf("junk dir = %v, %v", listed, err)
	}
}
