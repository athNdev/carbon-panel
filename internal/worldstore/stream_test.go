// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package worldstore

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFileRel(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFileRel(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func zipFromEntries(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBackupToRestoreFromRoundTrip(t *testing.T) {
	s, id, root := testStore(t)
	writeFileRel(t, root, "world/region/r.0.0.mca", "region-data")
	writeFileRel(t, root, "world/level.dat", "level")
	writeFileRel(t, root, "server.properties", "motd=hello")
	writeFileRel(t, root, "logs/latest.log", "noisy")

	archive := filepath.Join(t.TempDir(), "backup.zip")
	sum, err := s.BackupTo(id, archive, func(rel string) bool {
		return strings.HasPrefix(rel, "logs/")
	})
	if err != nil {
		t.Fatalf("BackupTo: %v", err)
	}
	if sum == "" {
		t.Fatal("BackupTo must return the whole-archive SHA-256")
	}
	if !HasWorldstoreManifest(archive) {
		t.Fatal("backup archive must carry the worldstore manifest")
	}

	// Mutate the live tree, then restore: archived contents come back and
	// excluded paths are not resurrected.
	writeFileRel(t, root, "world/level.dat", "garbled")
	writeFileRel(t, root, "logs/latest.log", "kept-out")
	if err := s.RestoreFrom(id, archive); err != nil {
		t.Fatalf("RestoreFrom: %v", err)
	}
	if got := readFileRel(t, root, "world/level.dat"); got != "level" {
		t.Fatalf("level.dat = %q, want restored content", got)
	}
	if got := readFileRel(t, root, "world/region/r.0.0.mca"); got != "region-data" {
		t.Fatalf("region file = %q", got)
	}
	if got := readFileRel(t, root, "server.properties"); got != "motd=hello" {
		t.Fatalf("server.properties = %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "logs", "latest.log")); !os.IsNotExist(err) {
		t.Fatalf("excluded paths are not part of the restored state; stat err = %v", err)
	}
}

func TestRestoreFromRejectsTamperedContent(t *testing.T) {
	s, id, root := testStore(t)
	writeFileRel(t, root, "world/level.dat", "level")
	writeFileRel(t, root, "keep.marker", "original")

	archive := filepath.Join(t.TempDir(), "backup.zip")
	if _, err := s.BackupTo(id, archive, nil); err != nil {
		t.Fatal(err)
	}

	// Rebuild the archive with one file's content altered but the manifest
	// untouched: extraction must fail the hash check.
	zr, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	tampered := filepath.Join(t.TempDir(), "tampered.zip")
	out, err := os.Create(tampered)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	for _, f := range zr.File {
		w, err := zw.CreateHeader(&f.FileHeader)
		if err != nil {
			t.Fatal(err)
		}
		content := []byte("evil")
		if strings.TrimSuffix(f.Name, "/") == manifestName {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			if _, err := buf.ReadFrom(rc); err != nil {
				t.Fatal(err)
			}
			_ = rc.Close()
			content = buf.Bytes()
		}
		if _, err := w.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	_ = zr.Close()
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = out.Close()

	if err := s.RestoreFrom(id, tampered); err == nil {
		t.Fatal("RestoreFrom must reject tampered content")
	}
	if got := readFileRel(t, root, "keep.marker"); got != "original" {
		t.Fatalf("failed restore must leave the old tree intact, got %q", got)
	}
}

func TestRestoreFromRejectsManifestMismatch(t *testing.T) {
	s, id, root := testStore(t)
	writeFileRel(t, root, "world/level.dat", "level")

	archive := filepath.Join(t.TempDir(), "backup.zip")
	if _, err := s.BackupTo(id, archive, nil); err != nil {
		t.Fatal(err)
	}

	// An archive whose entries do not match its manifest must be refused.
	extra := filepath.Join(t.TempDir(), "extra.zip")
	zipFromEntries(t, extra, map[string]string{
		"world/level.dat":         "level",
		"world/extra-not-listed":  "x",
		manifestName:              `{"version":1,"files":[{"path":"world/level.dat","size":5,"sha256":"39b941cb3a5d4a5d9b5d1d3e6f7b2a1c9d8e7f6a5b4c3d2e1f0a9b8c7d6e5f4a"}]}`,
	})
	if err := s.RestoreFrom(id, extra); err == nil {
		t.Fatal("RestoreFrom must reject archives whose entries are not fully covered by the manifest")
	}
}

func TestHasWorldstoreManifest(t *testing.T) {
	s, id, root := testStore(t)
	writeFileRel(t, root, "world/level.dat", "level")

	archive := filepath.Join(t.TempDir(), "backup.zip")
	if _, err := s.BackupTo(id, archive, nil); err != nil {
		t.Fatal(err)
	}
	if !HasWorldstoreManifest(archive) {
		t.Fatal("backup archive must be detected as worldstore format")
	}

	plain := filepath.Join(t.TempDir(), "plain.zip")
	zipFromEntries(t, plain, map[string]string{"world/level.dat": "level"})
	if HasWorldstoreManifest(plain) {
		t.Fatal("plain zip must not be detected as worldstore format")
	}
	if HasWorldstoreManifest(filepath.Join(t.TempDir(), "missing.zip")) {
		t.Fatal("missing archive must not be detected as worldstore format")
	}
}
