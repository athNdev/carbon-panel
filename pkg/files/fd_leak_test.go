package files

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// openFDCount returns the number of open file descriptors, or -1 if the
// platform does not expose /proc/self/fd.
func openFDCount() int {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return -1
	}
	return len(entries)
}

// TestExtractArchiveDoesNotHoldAllHandles builds an archive with enough
// entries to exhaust a tight descriptor budget if every entry's handles were
// held open simultaneously (the defer-in-loop defect), extracts it while a
// sampler records peak FD usage, and asserts the peak stays flat.
func TestExtractArchiveDoesNotHoldAllHandles(t *testing.T) {
	if openFDCount() < 0 {
		t.Skip("no /proc/self/fd on this platform")
	}
	base := t.TempDir()
	const entries = 300

	archive := filepath.Join(base, "many.tar.gz")
	names := make([]string, 0, entries)
	for i := 0; i < entries; i++ {
		names = append(names, fmt.Sprintf("dir-%d/file-%d.txt", i%10, i))
	}
	buildArchive(t, archive, names)

	dest := filepath.Join(base, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	baseline := openFDCount()
	var peak atomic.Int32
	peak.Store(int32(baseline))
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				if n := openFDCount(); n >= 0 {
					for {
						p := peak.Load()
						if int32(n) <= p || peak.CompareAndSwap(p, int32(n)) {
							break
						}
					}
				}
				time.Sleep(time.Millisecond)
			}
		}
	}()

	var counter atomic.Int32
	n, err := ExtractArchive(context.Background(), archive, dest, &counter)
	close(stop)
	if err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
	if n != entries {
		t.Errorf("extracted %d files, want %d", n, entries)
	}
	if counter.Load() != int32(entries) {
		t.Errorf("counter = %d, want %d", counter.Load(), entries)
	}

	// Spot-check contents.
	for _, name := range []string{"dir-0/file-0.txt", "dir-9/file-299.txt"} {
		if _, err := os.Stat(filepath.Join(dest, name)); err != nil {
			t.Errorf("missing extracted file %s: %v", name, err)
		}
	}

	// With per-entry closes the peak stays near baseline; holding one
	// output handle plus one reader handle per entry would push it ~2×entries
	// above baseline and fail this assertion on the defective code.
	const slack = 32
	if got := int(peak.Load()) - baseline; got > slack {
		t.Errorf("peak FD usage %d above baseline during %d-file extraction (slack %d): handles held across entries", got, entries, slack)
	}
}

// TestCreateZipManyFilesRoundTrip covers the second defer-in-loop site
// (CreateZipToWriter's WalkDir callback): zipping a directory with many
// files must close each member file as it goes and produce a complete
// archive.
func TestCreateZipManyFilesRoundTrip(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	const files = 200
	for i := 0; i < files; i++ {
		p := filepath.Join(src, fmt.Sprintf("file-%03d.txt", i))
		if err := os.WriteFile(p, []byte(fmt.Sprintf("body-%d", i)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	zipPath := filepath.Join(base, "out.zip")
	n, err := CreateZipArchive([]string{"."}, src, zipPath, true)
	if err != nil {
		t.Fatalf("CreateZipArchive: %v", err)
	}
	if n != files {
		t.Errorf("archived %d files, want %d", n, files)
	}

	// And the produced archive must extract back completely.
	dest := filepath.Join(base, "dest")
	count, err := ExtractArchive(context.Background(), zipPath, dest, nil)
	if err != nil {
		t.Fatalf("ExtractArchive of produced zip: %v", err)
	}
	if count != files {
		t.Errorf("re-extracted %d files, want %d", count, files)
	}
}
