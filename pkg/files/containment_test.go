// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package files

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// buildArchive writes a tar.gz with entries in the given order.
func buildArchive(t *testing.T, path string, names []string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer func() { _ = f.Close() }()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, name := range names {
		content := "x"
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write body: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
}

// TestWithinBounds is the unit-level guard for the containment primitive.
func TestWithinBounds(t *testing.T) {
	root := "/data/srv"
	cases := []struct {
		target string
		want   bool
	}{
		{"/data/srv", true},
		{"/data/srv/a/b.txt", true},
		{"/data/srv-evil/x", false}, // sibling sharing the string prefix
		{"/data/srv2", false},       // sibling sharing the string prefix
		{"/data", false},            // parent
		{"/etc/passwd", false},      // unrelated absolute
	}
	for _, tc := range cases {
		if got := Within(root, tc.target); got != tc.want {
			t.Errorf("Within(%q, %q) = %v, want %v", root, tc.target, got, tc.want)
		}
	}
}

// TestWithinRealBounds verifies that WithinReal enforces containment even
// across symlinks (e.g. symlinks pointing outside root are rejected, while
// internal symlinks and legitimate non-existent child paths are allowed).
func TestWithinRealBounds(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "srv")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	insideFile := filepath.Join(root, "regular.txt")
	if err := os.WriteFile(insideFile, []byte("regular"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Symlink to outside file
	linkOutsideFile := filepath.Join(root, "link_to_outside_file")
	if err := os.Symlink(outsideFile, linkOutsideFile); err != nil {
		t.Fatal(err)
	}

	// Symlink to outside directory
	linkOutsideDir := filepath.Join(root, "link_to_outside_dir")
	if err := os.Symlink(outside, linkOutsideDir); err != nil {
		t.Fatal(err)
	}

	// Symlink to inside file
	linkInsideFile := filepath.Join(root, "link_to_inside_file")
	if err := os.Symlink(insideFile, linkInsideFile); err != nil {
		t.Fatal(err)
	}

	// Broken symlink to non-existent outside file
	brokenLinkOutside := filepath.Join(root, "broken_link_outside")
	if err := os.Symlink(filepath.Join(outside, "ghost.txt"), brokenLinkOutside); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		target string
		want   bool
	}{
		{"root itself", root, true},
		{"regular inside file", insideFile, true},
		{"non-existent inside file", filepath.Join(root, "nonexistent.txt"), true},
		{"nested non-existent inside path", filepath.Join(root, "a", "b", "nonexistent.txt"), true},
		{"link to inside file", linkInsideFile, true},
		{"link to outside file", linkOutsideFile, false},
		{"link to outside directory", linkOutsideDir, false},
		{"path through outside directory symlink", filepath.Join(linkOutsideDir, "secret.txt"), false},
		{"non-existent path through outside directory symlink", filepath.Join(linkOutsideDir, "new.txt"), false},
		{"broken symlink pointing outside", brokenLinkOutside, false},
		{"sibling directory", filepath.Join(base, "srv-evil"), false},
		{"unrelated absolute", outsideFile, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WithinReal(root, tc.target); got != tc.want {
				t.Errorf("WithinReal(%q, %q) = %v, want %v", root, tc.target, got, tc.want)
			}
		})
	}
}

// TestExtractArchiveOverwritingExistingSymlink verifies that extracting an archive
// over an existing symlink never writes through to the target file.
func TestExtractArchiveOverwritingExistingSymlink(t *testing.T) {
	t.Run("outside symlink rejected", func(t *testing.T) {
		base := t.TempDir()
		dest := filepath.Join(base, "dest")
		outside := filepath.Join(base, "outside")
		if err := os.MkdirAll(dest, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(outside, 0o755); err != nil {
			t.Fatal(err)
		}

		outsideTarget := filepath.Join(outside, "victim.txt")
		if err := os.WriteFile(outsideTarget, []byte("original content"), 0o644); err != nil {
			t.Fatal(err)
		}

		// Create symlink inside dest pointing to outside victim
		symlinkPath := filepath.Join(dest, "target.txt")
		if err := os.Symlink(outsideTarget, symlinkPath); err != nil {
			t.Fatal(err)
		}

		archive := filepath.Join(base, "overwrite.tar.gz")
		buildArchive(t, archive, []string{"target.txt"})

		// Extraction must be rejected due to destination resolving outside
		_, err := ExtractArchive(context.Background(), archive, dest, nil)
		if err == nil {
			t.Fatalf("expected ExtractArchive to fail for outside symlink target, got nil")
		}

		// Verify outside file was NOT overwritten
		content, err := os.ReadFile(outsideTarget)
		if err != nil {
			t.Fatalf("read outside target: %v", err)
		}
		if string(content) != "original content" {
			t.Fatalf("outside file was overwritten through symlink! content = %q", string(content))
		}
	})

	t.Run("inside symlink replaced", func(t *testing.T) {
		base := t.TempDir()
		dest := filepath.Join(base, "dest")
		if err := os.MkdirAll(dest, 0o755); err != nil {
			t.Fatal(err)
		}

		insideTarget := filepath.Join(dest, "legit.txt")
		if err := os.WriteFile(insideTarget, []byte("original inside"), 0o644); err != nil {
			t.Fatal(err)
		}

		// Create symlink pointing to inside file
		symlinkPath := filepath.Join(dest, "target.txt")
		if err := os.Symlink(insideTarget, symlinkPath); err != nil {
			t.Fatal(err)
		}

		archive := filepath.Join(base, "overwrite_inside.tar.gz")
		buildArchive(t, archive, []string{"target.txt"})

		// Extraction should succeed by removing symlink and creating regular file
		_, err := ExtractArchive(context.Background(), archive, dest, nil)
		if err != nil {
			t.Fatalf("ExtractArchive failed: %v", err)
		}

		// The target pointed to should NOT have been overwritten
		content, err := os.ReadFile(insideTarget)
		if err != nil {
			t.Fatalf("read inside target: %v", err)
		}
		if string(content) != "original inside" {
			t.Fatalf("inside target file was overwritten through symlink! content = %q", string(content))
		}

		// And target.txt should now be a regular file, not a symlink
		fi, err := os.Lstat(symlinkPath)
		if err != nil {
			t.Fatalf("lstat symlink path: %v", err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("target.txt is still a symlink")
		}
	})
}

// TestExtractArchiveRejectsTraversal is the security regression test: a crafted
// archive must not be able to write outside the destination, including to a
// sibling directory that shares the destination's string prefix.
func TestExtractArchiveRejectsTraversal(t *testing.T) {
	base := t.TempDir()
	dest := filepath.Join(base, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(base, "evil.tar.gz")
	buildArchive(t, archive, []string{"good.txt", "../dest-evil/pwned.txt"})

	if _, err := ExtractArchive(context.Background(), archive, dest, nil); err == nil {
		t.Log("ExtractArchive returned nil error (traversal entry may have been skipped rather than fatal)")
	}

	if _, err := os.Stat(filepath.Join(base, "dest-evil", "pwned.txt")); err == nil {
		t.Fatalf("zip-slip: archive wrote outside the destination at %s", filepath.Join(base, "dest-evil", "pwned.txt"))
	}
	if _, err := os.Stat(filepath.Join(dest, "good.txt")); err != nil {
		t.Errorf("benign entry should still be extracted: %v", err)
	}
}

// TestExtractArchiveRejectsSymlinkEntries ensures link entries are refused
// rather than materialised as files that later reads could follow.
func TestExtractArchiveRejectsSymlinkEntries(t *testing.T) {
	base := t.TempDir()
	dest := filepath.Join(base, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(base, "link.tar.gz")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := "/etc/passwd"
	if err := tw.WriteHeader(&tar.Header{Name: "escape", Typeflag: tar.TypeSymlink, Linkname: body, Mode: 0o777}); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()

	if _, err := ExtractArchive(context.Background(), archive, dest, nil); err == nil {
		t.Log("symlink archive returned nil error")
	}

	info, statErr := os.Lstat(filepath.Join(dest, "escape"))
	if statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("symlink entry was materialised as a link to %s", body)
		}
		t.Fatalf("symlink entry was written as a file (mode %v)", info.Mode())
	}
}
