// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package scheduler

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	w := zip.NewWriter(f)
	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip entry: %v", err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("file close: %v", err)
	}
}

func TestVerifyZipArchive(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.zip")
	writeTestZip(t, good, map[string]string{"world/level.dat": "data", "server.properties": "x=1"})
	if err := verifyZipArchive(good); err != nil {
		t.Fatalf("valid archive rejected: %v", err)
	}
	if err := verifyZipArchive(filepath.Join(dir, "missing.zip")); err == nil {
		t.Fatalf("missing archive accepted")
	}
	bad := filepath.Join(dir, "bad.zip")
	if err := os.WriteFile(bad, []byte("not a zip"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyZipArchive(bad); err == nil {
		t.Fatalf("corrupt archive accepted")
	}
}

func TestSha256File(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(p, []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}
	sum, err := sha256File(p)
	if err != nil {
		t.Fatal(err)
	}
	if sum != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("unexpected sha256: %s", sum)
	}
}

func TestPruneBackupsLockedExempt(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-72 * time.Hour)
	mk := func(name string, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		ts := base.Add(age)
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	mk("srv_20200101-000000.zip", 0)
	mk("srv_20200102-000000.zip", time.Hour)
	mk("srv_20200103-000000.locked.zip", 2*time.Hour)
	n, err := pruneBackups(dir, "srv_", 1, 1, 1)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 pruned, got %d", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "srv_20200103-000000.locked.zip")); err != nil {
		t.Fatalf("locked backup pruned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "srv_20200102-000000.zip")); err != nil {
		t.Fatalf("minBackups survivor pruned: %v", err)
	}
}

func TestPruneBackupsLockedNamesAndSha256Cleaned(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-72 * time.Hour)
	mk := func(name string, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p+".sha256", []byte("sum"), 0644); err != nil {
			t.Fatal(err)
		}
		ts := base.Add(age)
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	mk("srv_1.zip", 0)
	mk("srv_2.zip", time.Hour)
	mk("srv_3.zip", 2*time.Hour)

	locked := map[string]bool{"srv_1.zip": true}
	n, err := pruneBackups(dir, "srv_", 1, 1, 1, locked)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 pruned, got %d", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "srv_1.zip")); err != nil {
		t.Fatalf("locked backup pruned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "srv_1.zip.sha256")); err != nil {
		t.Fatalf("locked backup sha256 removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "srv_2.zip")); !os.IsNotExist(err) {
		t.Fatalf("expected srv_2.zip to be pruned")
	}
	if _, err := os.Stat(filepath.Join(dir, "srv_2.zip.sha256")); !os.IsNotExist(err) {
		t.Fatalf("expected srv_2.zip.sha256 to be pruned")
	}
}
