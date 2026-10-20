// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package upload

import (
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

func TestSession_ChunkWriteCompletes(t *testing.T) {
	m := NewManager(t.TempDir(), time.Hour, 0, logger.New())

	sess, err := m.InitSession("data.bin", 10, 4)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if sess.TotalChunks != 3 {
		t.Fatalf("total chunks = %d, want 3", sess.TotalChunks)
	}

	// Out-of-order chunk writes must still complete.
	chunks := map[int32][]byte{0: []byte("abcd"), 1: []byte("efgh"), 2: []byte("ij")}
	for _, idx := range []int32{2, 0, 1} {
		done, err := m.WriteChunk(sess.ID, idx, chunks[idx])
		if err != nil {
			t.Fatalf("chunk %d failed: %v", idx, err)
		}
		if idx != 1 && done {
			t.Errorf("chunk %d reported completion early", idx)
		}
		if idx == 1 && !done {
			t.Error("final chunk did not report completion")
		}
	}

	got, err := m.GetSession(sess.ID)
	if err != nil || !got.Completed || got.BytesReceived != 10 {
		t.Errorf("session = %+v, %v", got, err)
	}

	missing, err := m.GetMissingChunks(sess.ID)
	if err != nil || len(missing) != 0 {
		t.Errorf("missing = %v, %v", missing, err)
	}

	status := []int64{}
	received, total, nChunks, totalChunks, completed, _, err := m.GetSessionStatus(sess.ID)
	_ = status
	if err != nil || !completed || received != 10 || total != 10 || nChunks != 3 || totalChunks != 3 {
		t.Errorf("status = %d/%d chunks %d/%d completed=%v err=%v",
			received, total, nChunks, totalChunks, completed, err)
	}
}

func TestSession_Errors(t *testing.T) {
	m := NewManager(t.TempDir(), time.Hour, 0, logger.New())

	if _, err := m.GetSession("missing"); err == nil {
		t.Error("unknown session fetched without error")
	}
	if _, err := m.WriteChunk("missing", 0, []byte("x")); err == nil {
		t.Error("chunk for unknown session accepted")
	}
	if _, err := m.GetMissingChunks("missing"); err == nil {
		t.Error("missing chunks for unknown session without error")
	}
	if _, _, _, _, _, _, err := m.GetSessionStatus("missing"); err == nil {
		t.Error("status for unknown session without error")
	}
	if err := m.Cancel("missing"); err == nil {
		t.Error("cancel unknown session without error")
	}

	sess, err := m.InitSession("data.bin", 10, 4)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if _, err := m.WriteChunk(sess.ID, 9, []byte("x")); err == nil {
		t.Error("out-of-range chunk accepted")
	}
	for idx, data := range map[int32][]byte{0: []byte("abcd"), 1: []byte("efgh"), 2: []byte("ij")} {
		if _, err := m.WriteChunk(sess.ID, idx, data); err != nil {
			t.Fatalf("chunk %d failed: %v", idx, err)
		}
	}

	path, name, err := m.GetTempPath(sess.ID)
	if err != nil || name != "data.bin" || !strings.Contains(path, sess.ID) {
		t.Errorf("temp path = %q %q, %v", path, name, err)
	}

	if err := m.Cancel(sess.ID); err != nil {
		t.Fatalf("cancel failed: %v", err)
	}
	if _, err := m.GetSession(sess.ID); err == nil {
		t.Error("cancelled session still fetchable")
	}

	// Temp path is only available after completion.
	fresh, err := m.InitSession("pending.bin", 4, 4)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if _, _, err := m.GetTempPath(fresh.ID); err == nil {
		t.Error("temp path available before completion")
	}
}

func TestSession_WriteStream(t *testing.T) {
	m := NewManager(t.TempDir(), time.Hour, 0, logger.New())

	sess, err := m.InitSession("stream.bin", 8, 8)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	n, done, err := m.WriteStream(sess.ID, strings.NewReader("12345678"), 0)
	if err != nil || !done || n != 8 {
		t.Errorf("stream = %d %v %v", n, done, err)
	}
	if _, _, err := m.WriteStream("missing", strings.NewReader("x"), 0); err == nil {
		t.Error("stream for unknown session accepted")
	}
}

func TestSession_WriteStreamRejectsInvalidRange(t *testing.T) {
	m := NewManager(t.TempDir(), time.Hour, 0, logger.New())
	defer m.Stop()

	sess, err := m.InitSession("stream.bin", 8, 8)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	for _, offset := range []int64{-1, 9} {
		if _, _, err := m.WriteStream(sess.ID, strings.NewReader("x"), offset); !errors.Is(err, ErrInvalidOffset) {
			t.Errorf("offset %d: expected ErrInvalidOffset, got %v", offset, err)
		}
	}
	if _, _, err := m.WriteStream(sess.ID, strings.NewReader("123456789"), 0); !errors.Is(err, ErrUploadOverflow) {
		t.Fatalf("expected ErrUploadOverflow for oversized stream, got %v", err)
	}
	if sess.BytesReceived != 0 {
		t.Fatalf("rejected stream advanced BytesReceived to %d", sess.BytesReceived)
	}
	if sess.Completed {
		t.Fatal("rejected stream marked session completed")
	}
}

func TestSession_WriteStreamSerializesConcurrentRequests(t *testing.T) {
	m := NewManager(t.TempDir(), time.Hour, 0, logger.New())
	defer m.Stop()

	sess, err := m.InitSession("stream.bin", 8, 8)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	release := make(chan struct{})
	started := make(chan struct{})
	reader := &blockingReader{started: started, release: release, data: []byte("12345678")}
	firstDone := make(chan error, 1)
	go func() {
		_, _, err := m.WriteStream(sess.ID, reader, 0)
		firstDone <- err
	}()
	<-started

	secondDone := make(chan error, 1)
	go func() {
		_, _, err := m.WriteStream(sess.ID, strings.NewReader("abcdefgh"), 0)
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("concurrent stream completed before first released: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first stream failed: %v", err)
	}
	if err := <-secondDone; !errors.Is(err, ErrSessionCompleted) {
		t.Fatalf("second stream error = %v, want ErrSessionCompleted", err)
	}
}

type blockingReader struct {
	started chan<- struct{}
	release <-chan struct{}
	data    []byte
	once    sync.Once
}

func (r *blockingReader) Read(p []byte) (int, error) {
	r.once.Do(func() {
		close(r.started)
		<-r.release
	})
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestCleanupSession_MoveSemantics(t *testing.T) {
	m := NewManager(t.TempDir(), time.Hour, 0, logger.New())
	defer m.Stop()

	sess, err := m.InitSession("move.bin", 4, 4)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if _, err := os.Stat(sess.TempPath); err != nil {
		t.Fatalf("temp file missing: %v", err)
	}

	// CleanupSession drops the record but keeps the file for the caller to move.
	if err := m.CleanupSession(sess.ID); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	if _, err := m.GetSession(sess.ID); err == nil {
		t.Error("cleaned session still fetchable")
	}
	if _, err := os.Stat(sess.TempPath); err != nil {
		t.Errorf("temp file removed by CleanupSession: %v", err)
	}
	// Idempotent on missing sessions.
	if err := m.CleanupSession(sess.ID); err != nil {
		t.Errorf("second cleanup failed: %v", err)
	}
}

func TestStop_ClearsSessionsAndFiles(t *testing.T) {
	m := NewManager(t.TempDir(), time.Hour, 0, logger.New())

	sess, err := m.InitSession("stop.bin", 4, 4)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	m.Stop()

	if _, err := m.GetSession(sess.ID); err == nil {
		t.Error("session survives Stop")
	}
	if _, err := os.Stat(sess.TempPath); !os.IsNotExist(err) {
		t.Errorf("temp file survives Stop: %v", err)
	}
}

func TestCleanupExpired_SweepsStale(t *testing.T) {
	m := NewManager(t.TempDir(), time.Hour, 0, logger.New())
	defer m.Stop()

	stale, err := m.InitSession("stale.bin", 4, 4)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	stale.ExpiresAt = time.Now().Add(-time.Minute)
	fresh, err := m.InitSession("fresh.bin", 4, 4)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	m.cleanupExpired()

	if _, err := m.GetSession(stale.ID); err == nil {
		t.Error("stale session survives sweep")
	}
	if _, err := os.Stat(stale.TempPath); !os.IsNotExist(err) {
		t.Error("stale temp file survives sweep")
	}
	if _, err := m.GetSession(fresh.ID); err != nil {
		t.Errorf("fresh session swept: %v", err)
	}
	// Unknown IDs are safe no-ops.
	m.cleanupSession("missing")
}
