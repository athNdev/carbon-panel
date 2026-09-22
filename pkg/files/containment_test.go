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
	defer f.Close()
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
