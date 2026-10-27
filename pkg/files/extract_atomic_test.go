// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package files

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func makeTestZip(t *testing.T, srcDir string, entries map[string]string) string {
	t.Helper()
	var names []string
	for rel, content := range entries {
		p := filepath.Join(srcDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		names = append(names, rel)
	}
	zipPath := filepath.Join(t.TempDir(), "archive.zip")
	if _, err := CreateZipArchive(names, srcDir, zipPath, true); err != nil {
		t.Fatal(err)
	}
	return zipPath
}

// TestExtractArchiveAtomicPurgesStaleFiles pins the OSS-235 clean-restore
// rule: a successful restore replaces the tree, so files removed between
// backup and restore never survive.
func TestExtractArchiveAtomicPurgesStaleFiles(t *testing.T) {
	src := t.TempDir()
	zipPath := makeTestZip(t, src, map[string]string{"world/level.dat": "new", "server.properties": "motd=new"})

	target := filepath.Join(t.TempDir(), "server-data")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "stale.txt"), []byte("left over"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "world", "level.dat"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	count, err := ExtractArchiveAtomic(context.Background(), zipPath, target)
	if err != nil {
		t.Fatalf("ExtractArchiveAtomic: %v", err)
	}
	if count != 2 {
		t.Fatalf("extracted %d files, want 2", count)
	}
	if _, err := os.Stat(filepath.Join(target, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("stale file survived a successful restore, stat err = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(target, "world", "level.dat"))
	if err != nil || string(data) != "new" {
		t.Fatalf("restored level.dat = %q, %v", data, err)
	}
}

// TestExtractArchiveAtomicFailedExtractKeepsCurrentWorld: a broken archive
// must leave the live tree exactly as it was.
func TestExtractArchiveAtomicFailedExtractKeepsCurrentWorld(t *testing.T) {
	target := filepath.Join(t.TempDir(), "server-data")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "world.txt"), []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}

	broken := filepath.Join(t.TempDir(), "broken.zip")
	if err := os.WriteFile(broken, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtractArchiveAtomic(context.Background(), broken, target); err == nil {
		t.Fatal("broken archive must fail")
	}

	data, err := os.ReadFile(filepath.Join(target, "world.txt"))
	if err != nil || string(data) != "precious" {
		t.Fatalf("current world must be untouched after failed extract, got %q, %v", data, err)
	}
}

// TestExtractArchiveAtomicFreshTarget restores into a target that does not
// exist yet.
func TestExtractArchiveAtomicFreshTarget(t *testing.T) {
	src := t.TempDir()
	zipPath := makeTestZip(t, src, map[string]string{"level.dat": "fresh"})

	target := filepath.Join(t.TempDir(), "server-data")
	count, err := ExtractArchiveAtomic(context.Background(), zipPath, target)
	if err != nil || count != 1 {
		t.Fatalf("ExtractArchiveAtomic fresh = %d, %v", count, err)
	}
	data, err := os.ReadFile(filepath.Join(target, "level.dat"))
	if err != nil || string(data) != "fresh" {
		t.Fatalf("restored level.dat = %q, %v", data, err)
	}
}
