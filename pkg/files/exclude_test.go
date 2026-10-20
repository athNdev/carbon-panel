// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package files

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestPathExcludedDefaults(t *testing.T) {
	patterns := []string{"session.lock", "logs/*.gz", "dynmap/web/tiles", "spark/tmp"}
	excluded := []string{
		"world/session.lock",
		"logs/latest.gz",
		"dynmap/web/tiles/t.png",
		"spark/tmp/a.bin",
	}
	kept := []string{"world/level.dat", "logs/latest.log", "plugins/floodgate.jar"}
	for _, rel := range excluded {
		if !PathExcluded(rel, patterns) {
			t.Errorf("expected %s to be excluded", rel)
		}
	}
	for _, rel := range kept {
		if PathExcluded(rel, patterns) {
			t.Errorf("expected %s to be kept", rel)
		}
	}
}

func TestCreateZipArchiveSkipsExcluded(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "world", "level.dat"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "world", "session.lock"), []byte("lock"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "b.zip")
	if _, err := CreateZipArchive([]string{"world"}, src, dest, true, func(rel string) bool {
		return PathExcluded(rel, []string{"session.lock"})
	}); err != nil {
		t.Fatal(err)
	}
	r, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	for _, f := range r.File {
		if filepath.Base(f.Name) == "session.lock" {
			t.Fatalf("archive contains %s", f.Name)
		}
	}
}
