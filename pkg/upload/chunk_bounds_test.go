package upload

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

// TestInitSessionRejectsInvalidSizes covers the divide-by-zero fix: InitSession
// must reject a non-positive chunk size itself rather than relying on an
// upstream RPC caller having validated it first. A zero chunk size previously
// panicked on totalChunks = ceil(totalSize/chunkSize).
func TestInitSessionRejectsInvalidSizes(t *testing.T) {
	log := logger.New()

	for _, tc := range []struct {
		name      string
		totalSize int64
		chunkSize int32
		wantErr   error
	}{
		{"zero chunk size", 1024, 0, ErrInvalidChunkSize},
		{"negative chunk size", 1024, -1, ErrInvalidChunkSize},
		{"negative total size", -1, 64, ErrFileTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager(t.TempDir(), time.Hour, 0, log)
			// Must return an error, not panic.
			sess, err := m.InitSession("f.bin", tc.totalSize, tc.chunkSize)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got session=%v err=%v", tc.wantErr, sess, err)
			}
			if sess != nil {
				t.Fatalf("expected nil session on validation failure, got %+v", sess)
			}
		})
	}
}

// TestWriteChunkRejectsOutOfBoundsChunks covers the unbounded-chunk fix: a
// chunk must never push BytesReceived or on-disk usage past the declared
// TotalSize, and a rejected chunk must leave no partial bytes behind.
//
// The temp file is pre-allocated to TotalSize by InitSession (file.Truncate),
// so "nothing was written" is asserted on the CONTENT of the target byte
// range, not on the file length.
func TestWriteChunkRejectsOutOfBoundsChunks(t *testing.T) {
	log := logger.New()

	t.Run("chunk larger than declared chunk size", func(t *testing.T) {
		m := NewManager(t.TempDir(), time.Hour, 0, log)
		sess, err := m.InitSession("f.bin", 256, 64)
		if err != nil {
			t.Fatalf("InitSession: %v", err)
		}

		oversized := make([]byte, 65)
		if _, err := m.WriteChunk(sess.ID, 0, oversized); !errors.Is(err, ErrChunkTooLarge) {
			t.Fatalf("expected ErrChunkTooLarge, got %v", err)
		}
		assertRangeZero(t, sess, 0, len(oversized))
		if sess.BytesReceived != 0 {
			t.Fatalf("rejected chunk advanced BytesReceived to %d, want 0", sess.BytesReceived)
		}
	})

	t.Run("chunk index at or past end of file", func(t *testing.T) {
		m := NewManager(t.TempDir(), time.Hour, 0, log)
		sess, err := m.InitSession("f.bin", 256, 64)
		if err != nil {
			t.Fatalf("InitSession: %v", err)
		}

		// totalSize/chunkSize == 4, so index 4 is out of range entirely and
		// is caught by the index guard before the size guards.
		if _, err := m.WriteChunk(sess.ID, 4, make([]byte, 64)); !errors.Is(err, ErrInvalidChunk) {
			t.Fatalf("expected ErrInvalidChunk, got %v", err)
		}
		if _, err := m.WriteChunk(sess.ID, -1, make([]byte, 64)); !errors.Is(err, ErrInvalidChunk) {
			t.Fatalf("expected ErrInvalidChunk for negative index, got %v", err)
		}
		if sess.BytesReceived != 0 {
			t.Fatalf("rejected chunk advanced BytesReceived to %d, want 0", sess.BytesReceived)
		}
	})

	t.Run("write would overflow declared total size", func(t *testing.T) {
		// TotalSize deliberately not a multiple of ChunkSize so the last
		// legal index cannot hold a full chunk.
		m := NewManager(t.TempDir(), time.Hour, 0, log)
		sess, err := m.InitSession("f.bin", 100, 64)
		if err != nil {
			t.Fatalf("InitSession: %v", err)
		}

		if _, err := m.WriteChunk(sess.ID, 0, make([]byte, 64)); err != nil {
			t.Fatalf("priming chunk 0: %v", err)
		}
		if sess.BytesReceived != 64 {
			t.Fatalf("expected 64 bytes after priming, got %d", sess.BytesReceived)
		}

		// Index 1 is in range, but a full 64-byte chunk at offset 64 would
		// reach 128 > TotalSize(100).
		before := sess.BytesReceived
		if _, err := m.WriteChunk(sess.ID, 1, make([]byte, 64)); !errors.Is(err, ErrUploadOverflow) {
			t.Fatalf("expected ErrUploadOverflow, got %v", err)
		}
		if sess.BytesReceived != before {
			t.Fatalf("rejected chunk advanced BytesReceived %d -> %d", before, sess.BytesReceived)
		}
		// Offset 64..100 must still be zeros.
		assertRangeZero(t, sess, 64, 36)
	})

	t.Run("replayed chunk is idempotent and does not double count", func(t *testing.T) {
		m := NewManager(t.TempDir(), time.Hour, 0, log)
		sess, err := m.InitSession("f.bin", 256, 64)
		if err != nil {
			t.Fatalf("InitSession: %v", err)
		}

		if _, err := m.WriteChunk(sess.ID, 0, make([]byte, 64)); err != nil {
			t.Fatalf("first write: %v", err)
		}
		afterFirst := sess.BytesReceived

		// Replay is deliberately idempotent, not an error.
		if _, err := m.WriteChunk(sess.ID, 0, make([]byte, 64)); err != nil {
			t.Fatalf("replay should be idempotent, got %v", err)
		}
		if sess.BytesReceived != afterFirst {
			t.Fatalf("replay changed BytesReceived: %d -> %d", afterFirst, sess.BytesReceived)
		}
	})

	t.Run("in-bounds full upload still completes", func(t *testing.T) {
		m := NewManager(t.TempDir(), time.Hour, 0, log)
		sess, err := m.InitSession("f.bin", 256, 64)
		if err != nil {
			t.Fatalf("InitSession: %v", err)
		}

		for i := int32(0); i < 4; i++ {
			done, err := m.WriteChunk(sess.ID, i, make([]byte, 64))
			if err != nil {
				t.Fatalf("chunk %d: %v", i, err)
			}
			if i == 3 && !done {
				t.Fatalf("expected session complete after final chunk")
			}
		}
		if sess.BytesReceived != 256 {
			t.Fatalf("expected 256 bytes received, got %d", sess.BytesReceived)
		}
		fi, err := os.Stat(sess.TempPath)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if fi.Size() != 256 {
			t.Fatalf("expected 256-byte file, got %d", fi.Size())
		}
	})
}

// assertRangeZero verifies the byte range [offset, offset+length) of the
// session's temp file was never written.
func assertRangeZero(t *testing.T, sess *Session, offset int64, length int) {
	t.Helper()
	f, err := os.Open(sess.TempPath)
	if err != nil {
		t.Fatalf("open temp file: %v", err)
	}
	defer f.Close()

	buf := make([]byte, length)
	if _, err := f.ReadAt(buf, offset); err != nil {
		t.Fatalf("read at offset %d: %v", offset, err)
	}
	for i, b := range buf {
		if b != 0 {
			t.Fatalf("byte at offset %d was written by a rejected chunk (got 0x%02x)", offset+int64(i), b)
		}
	}
}
