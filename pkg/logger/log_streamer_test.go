package logger

import (
	"testing"
	"time"

	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestLogStreamer_StopStreaming_FreesMemory(t *testing.T) {
	log := New()
	ls := NewLogStreamer(nil, log, 100)

	containerID := "test-container-1"
	// Manually create a stream with logs
	stream := &ContainerLogStream{
		containerID: containerID,
		logs: []*v1.LogEntry{
			{Message: "entry 1", Timestamp: timestamppb.Now()},
			{Message: "entry 2", Timestamp: timestamppb.Now()},
		},
		maxEntries: 100,
		active:     true,
	}
	ls.streams[containerID] = stream

	if len(ls.GetLogs(containerID, 0)) != 2 {
		t.Fatalf("expected 2 log entries before StopStreaming")
	}

	// Stop streaming
	ls.StopStreaming(containerID)

	// Stream should be removed from ls.streams and logs freed
	if len(ls.GetLogs(containerID, 0)) != 0 {
		t.Errorf("expected 0 log entries after StopStreaming")
	}

	ls.mu.RLock()
	_, exists := ls.streams[containerID]
	ls.mu.RUnlock()

	if exists {
		t.Errorf("expected stream to be deleted from ls.streams")
	}
	if stream.logs != nil {
		t.Errorf("expected stream.logs to be nil after StopStreaming")
	}
}

func TestLogStreamer_RemoveContainer_CleansSubscribers(t *testing.T) {
	log := New()
	ls := NewLogStreamer(nil, log, 100)

	containerID := "test-container-2"
	subCh := ls.Subscribe(containerID)

	// Verify subscriber exists
	ls.subMu.RLock()
	subs, ok := ls.subscribers[containerID]
	ls.subMu.RUnlock()
	if !ok || len(subs) != 1 {
		t.Fatalf("expected 1 subscriber registered")
	}

	// Remove container
	ls.RemoveContainer(containerID)

	// Subscriber map should be removed
	ls.subMu.RLock()
	_, ok = ls.subscribers[containerID]
	ls.subMu.RUnlock()
	if ok {
		t.Errorf("expected subscribers to be deleted for container")
	}

	// Channel should be closed
	select {
	case _, open := <-subCh:
		if open {
			t.Errorf("expected subCh to be closed, but received value")
		}
	default:
		// Try reading with timeout
		select {
		case _, open := <-subCh:
			if open {
				t.Errorf("expected subCh to be closed")
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("timed out waiting for subCh closure")
		}
	}
}

func TestLogStreamer_ClearLogs(t *testing.T) {
	log := New()
	ls := NewLogStreamer(nil, log, 100)

	containerID := "test-container-3"
	stream := &ContainerLogStream{
		containerID: containerID,
		logs: []*v1.LogEntry{
			{Message: "log 1", Timestamp: timestamppb.Now()},
		},
		maxEntries: 100,
		active:     true,
	}
	ls.streams[containerID] = stream

	ls.ClearLogs(containerID)

	if stream.logs != nil {
		t.Errorf("expected stream.logs to be nil after ClearLogs, got len %d", len(stream.logs))
	}
}

func TestLogStreamer_MigrateSubscribers(t *testing.T) {
	log := New()
	ls := NewLogStreamer(nil, log, 100)

	oldID := "old-container"
	newID := "new-container"

	ch := ls.Subscribe(oldID)

	ls.MigrateSubscribers(oldID, newID)

	ls.subMu.RLock()
	_, oldExists := ls.subscribers[oldID]
	newSubs, newExists := ls.subscribers[newID]
	ls.subMu.RUnlock()

	if oldExists {
		t.Errorf("expected old container subscribers to be deleted")
	}
	if !newExists || !newSubs[ch] {
		t.Errorf("expected subscriber channel to be migrated to new container")
	}
}
