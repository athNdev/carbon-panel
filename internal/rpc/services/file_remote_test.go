package services

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func createTestZip(t *testing.T, filename, content string) ([]byte, string) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	f, err := zw.Create(filename)
	if err != nil {
		t.Fatalf("failed to create zip entry: %v", err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatalf("failed to write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}

	data := buf.Bytes()
	h := sha256.Sum256(data)
	return data, hex.EncodeToString(h[:])
}

func TestFileService_DownloadRemoteArchive_AutoExtract(t *testing.T) {
	t.Setenv("DISCO_ALLOW_LOOPBACK_TEST", "1")
	store := setupTestStore(t)
	defer store.Close()

	tempDataDir, err := os.MkdirTemp("", "file_service_test_data_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDataDir)

	srvDataDir := filepath.Join(tempDataDir, "server1")
	if err := os.MkdirAll(srvDataDir, 0755); err != nil {
		t.Fatalf("failed to create server data dir: %v", err)
	}

	server := &storage.Server{
		ID:       "srv-test-1",
		Name:     "Test Server",
		DataPath: srvDataDir,
		NodeID:   "local",
	}
	if err := store.CreateServer(context.Background(), server); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	zipData, expectedSHA := createTestZip(t, "plugins/hello.txt", "world from archive")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(zipData)
	}))
	defer ts.Close()

	svc := NewFileService(store, nil, nil, nil, logger.New())

	req := connect.NewRequest(&v1.DownloadRemoteArchiveRequest{
		ServerId:        server.ID,
		Url:             ts.URL + "/test.zip",
		DestinationPath: "",
		Sha256Checksum:  expectedSHA,
		AutoExtract:     true,
	})

	resp, err := svc.DownloadRemoteArchive(context.Background(), req)
	if err != nil {
		t.Fatalf("DownloadRemoteArchive failed: %v", err)
	}

	taskID := resp.Msg.TaskId
	if taskID == "" {
		t.Fatal("empty task_id returned")
	}

	// Poll until completed
	deadline := time.Now().Add(5 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		pResp, err := svc.GetRemoteArchiveProgress(context.Background(), connect.NewRequest(&v1.GetRemoteArchiveProgressRequest{
			ServerId: server.ID,
			TaskId:   taskID,
		}))
		if err != nil {
			t.Fatalf("failed to get progress: %v", err)
		}
		if pResp.Msg.Status == "completed" {
			completed = true
			break
		}
		if pResp.Msg.Status == "failed" {
			t.Fatalf("download task failed: %s", pResp.Msg.Error)
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !completed {
		t.Fatal("download task did not complete within timeout")
	}

	// Verify extracted file exists
	extractedFile := filepath.Join(srvDataDir, "plugins", "hello.txt")
	content, err := os.ReadFile(extractedFile)
	if err != nil {
		t.Fatalf("extracted file not found: %v", err)
	}
	if string(content) != "world from archive" {
		t.Fatalf("unexpected content: %q", string(content))
	}
}

func TestFileService_DownloadRemoteArchive_ChecksumMismatch(t *testing.T) {
	t.Setenv("DISCO_ALLOW_LOOPBACK_TEST", "1")
	store := setupTestStore(t)
	defer store.Close()

	tempDataDir, err := os.MkdirTemp("", "file_service_test_data_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDataDir)

	srvDataDir := filepath.Join(tempDataDir, "server2")
	_ = os.MkdirAll(srvDataDir, 0755)

	server := &storage.Server{
		ID:       "srv-test-2",
		Name:     "Test Server 2",
		DataPath: srvDataDir,
		NodeID:   "local",
	}
	_ = store.CreateServer(context.Background(), server)

	zipData, _ := createTestZip(t, "test.txt", "data")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(zipData)
	}))
	defer ts.Close()

	svc := NewFileService(store, nil, nil, nil, logger.New())

	req := connect.NewRequest(&v1.DownloadRemoteArchiveRequest{
		ServerId:        server.ID,
		Url:             ts.URL + "/test.zip",
		Sha256Checksum:  "0000000000000000000000000000000000000000000000000000000000000000",
		AutoExtract:     true,
	})

	resp, err := svc.DownloadRemoteArchive(context.Background(), req)
	if err != nil {
		t.Fatalf("DownloadRemoteArchive failed: %v", err)
	}

	taskID := resp.Msg.TaskId
	deadline := time.Now().Add(5 * time.Second)
	failed := false
	for time.Now().Before(deadline) {
		pResp, _ := svc.GetRemoteArchiveProgress(context.Background(), connect.NewRequest(&v1.GetRemoteArchiveProgressRequest{
			ServerId: server.ID,
			TaskId:   taskID,
		}))
		if pResp.Msg.Status == "failed" {
			failed = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !failed {
		t.Fatal("expected download task to fail on checksum mismatch, but it did not")
	}
}
