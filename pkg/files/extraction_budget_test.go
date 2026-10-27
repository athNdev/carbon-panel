// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package files

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// writeZipWithFile builds a zip containing a single file of n zero bytes. Zeros
// compress extremely well, so the archive stays tiny while expanding to n bytes
// on extraction — the shape of a decompression bomb.
func writeZipWithFile(t *testing.T, path string, n int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	w, err := zw.Create("big.bin")
	if err != nil {
		t.Fatalf("zip create entry: %v", err)
	}
	if _, err := w.Write(make([]byte, n)); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
}

// TestExtractArchiveLimitRejectsDecompressionBomb locks the UP-03 defence: a
// highly-compressed archive must not expand past the budget onto disk.
func TestExtractArchiveLimitRejectsDecompressionBomb(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "bomb.zip")
	writeZipWithFile(t, zipPath, 1<<20) // expands to 1 MiB

	dest := filepath.Join(dir, "out-small")
	_, err := ExtractArchiveLimit(context.Background(), zipPath, dest, nil, 4096)
	if err == nil {
		t.Fatal("expected extraction to fail when the archive exceeds the budget")
	}
	if !strings.Contains(err.Error(), "maximum extracted size") {
		t.Fatalf("expected a size-budget error, got: %v", err)
	}

	// The bomb must not have been fully written to disk.
	if info, statErr := os.Stat(filepath.Join(dest, "big.bin")); statErr == nil {
		if info.Size() > 4096 {
			t.Fatalf("wrote %d bytes despite a 4096 byte budget", info.Size())
		}
	}
}

// TestExtractArchiveLimitAllowsWithinBudget is the positive counterpart.
func TestExtractArchiveLimitAllowsWithinBudget(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "ok.zip")
	writeZipWithFile(t, zipPath, 1<<20)

	dest := filepath.Join(dir, "out-big")
	n, err := ExtractArchiveLimit(context.Background(), zipPath, dest, nil, 1<<21)
	if err != nil {
		t.Fatalf("expected success within budget, got: %v", err)
	}
	if n != 1 {
		t.Fatalf("extracted %d files, want 1", n)
	}
	if info, statErr := os.Stat(filepath.Join(dest, "big.bin")); statErr != nil {
		t.Fatalf("extracted file missing: %v", statErr)
	} else if info.Size() != 1<<20 {
		t.Fatalf("extracted size %d, want %d", info.Size(), 1<<20)
	}
}

// TestExtractArchiveDefaultBudgetAndCounter checks the public wrapper keeps the
// default budget and increments the caller's progress counter per file.
func TestExtractArchiveDefaultBudgetAndCounter(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "small.zip")
	writeZipWithFile(t, zipPath, 128)

	var counter atomic.Int32
	dest := filepath.Join(dir, "out")
	n, err := ExtractArchive(context.Background(), zipPath, dest, &counter)
	if err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
	if n != 1 || counter.Load() != 1 {
		t.Fatalf("files=%d counter=%d, want 1 and 1", n, counter.Load())
	}
}

// TestExtractArchiveLimitNonPositiveUsesDefault guards the zero/negative budget
// path: it must fall back to the default rather than extracting nothing.
func TestExtractArchiveLimitNonPositiveUsesDefault(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "tiny.zip")
	writeZipWithFile(t, zipPath, 16)

	dest := filepath.Join(dir, "out")
	n, err := ExtractArchiveLimit(context.Background(), zipPath, dest, nil, 0)
	if err != nil || n != 1 {
		t.Fatalf("ExtractArchiveLimit(0) = %d, %v; want 1, nil", n, err)
	}
}
