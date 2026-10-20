package files

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type centralDirectoryFailWriter struct {
	err error
}

func (w *centralDirectoryFailWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("PK\x01\x02")) {
		return 0, w.err
	}
	return len(p), nil
}

func TestCreateZipToWriterReportsFinalizeError(t *testing.T) {
	writeErr := errors.New("disk full")
	w := &centralDirectoryFailWriter{err: writeErr}
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "hello.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	count, err := CreateZipToWriter([]string{"hello.txt"}, base, w, true)
	if err == nil {
		t.Fatal("expected archive creation error")
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if !strings.Contains(err.Error(), "failed to finalize zip archive") {
		t.Fatalf("error = %q, want finalization error", err)
	}
	if !errors.Is(err, writeErr) {
		t.Fatalf("error = %q, want wrapped writer error", err)
	}
}
