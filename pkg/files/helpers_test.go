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

func TestSanitizePathName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"My Server 1", "my_server_1"},
		{"  spaces  ", "spaces"},
		{"a/b\\c:d", "a_b_c_d"},
		{"", "DISCO_GENERIC"},
		{"   ", "DISCO_GENERIC"},
		{"UPPER-Mixed_123", "upper-mixed_123"},
	}
	for _, tc := range cases {
		if got := SanitizePathName(tc.in); got != tc.want {
			t.Errorf("SanitizePathName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsTextFile(t *testing.T) {
	dir := t.TempDir()

	text := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(text, []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsTextFile(text) {
		t.Error("plain text not detected as text")
	}

	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsTextFile(empty) {
		t.Error("empty file should count as text")
	}

	bin := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(bin, []byte{0x00, 0x01, 0x02, 0xff, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}
	if IsTextFile(bin) {
		t.Error("binary file detected as text")
	}

	if IsTextFile(filepath.Join(dir, "missing.txt")) {
		t.Error("missing file detected as text")
	}
}

func TestCopyDirAndFile(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("BB"), 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "copy")
	if err := CopyDir(src, dst); err != nil {
		t.Fatalf("CopyDir failed: %v", err)
	}
	for _, rel := range []string{"a.txt", "sub/b.txt"} {
		data, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Errorf("copied file %s missing: %v", rel, err)
		} else if len(data) == 0 {
			t.Errorf("copied file %s empty", rel)
		}
	}

	// Copy onto itself must fail, not truncate.
	self := filepath.Join(src, "a.txt")
	if err := CopyFile(self, self); err == nil {
		t.Error("self-copy accepted")
	}
	if data, _ := os.ReadFile(self); string(data) != "A" {
		t.Errorf("self-copy truncated the file: %q", data)
	}

	if err := CopyDir(filepath.Join(src, "missing"), filepath.Join(t.TempDir(), "x")); err == nil {
		t.Error("missing source accepted")
	}
}

func TestCalculateDirSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b"), make([]byte, 50), 0o644); err != nil {
		t.Fatal(err)
	}

	size, err := CalculateDirSize(dir)
	if err != nil {
		t.Fatalf("CalculateDirSize failed: %v", err)
	}
	if size != 150 {
		t.Errorf("size = %d, want 150", size)
	}
}

func TestZipRoundTrip(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "hello.txt"), []byte("hello zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "out.zip")

	n, err := CreateZipArchive([]string{"hello.txt"}, src, archive, true)
	if err != nil {
		t.Fatalf("CreateZipArchive failed: %v", err)
	}
	if n != 1 {
		t.Errorf("archived %d files, want 1", n)
	}

	dest := t.TempDir()
	count, err := ExtractArchive(t.Context(), archive, dest, nil)
	if err != nil {
		t.Fatalf("ExtractArchive failed: %v", err)
	}
	if count != 1 {
		t.Errorf("extracted %d files, want 1", count)
	}
	data, err := os.ReadFile(filepath.Join(dest, "hello.txt"))
	if err != nil || string(data) != "hello zip" {
		t.Errorf("extracted content = %q, %v", data, err)
	}
}
