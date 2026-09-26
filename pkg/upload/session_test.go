// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package upload

import (
	"strings"
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
