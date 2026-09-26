package nodeagent

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFileManagerSandboxing(t *testing.T) {
	tempDir := t.TempDir()
	fm := NewFileManager(tempDir)
	workloadID := "test-workload"

	// 1. Should reject empty workload ID
	_, _, err := fm.resolveWorkloadPath("", "test.txt")
	if err == nil {
		t.Fatal("expected error for empty workload ID")
	}

	// 2. Traversal attempt escaping sandbox
	_, _, err = fm.resolveWorkloadPath(workloadID, "../../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal escape")
	}

	// 3. Normal path should resolve inside workloads/<id>/data
	base, target, err := fm.resolveWorkloadPath(workloadID, "server.properties")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedBase := filepath.Join(tempDir, "workloads", workloadID, "data")
	if base != expectedBase {
		t.Fatalf("expected base %s, got %s", expectedBase, base)
	}
	expectedTarget := filepath.Join(expectedBase, "server.properties")
	if target != expectedTarget {
		t.Fatalf("expected target %s, got %s", expectedTarget, target)
	}
}

func TestFileManagerLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	fm := NewFileManager(tempDir)
	workloadID := "test-workload"

	// 1. Create directory
	if err := fm.CreateDirectory(workloadID, "plugins/my-plugin"); err != nil {
		t.Fatalf("create directory failed: %v", err)
	}

	// 2. Write file atomically across chunks
	content := []byte("motd=Welcome to Carbon Cloud!\nmax-players=20\n")
	chunk1 := content[:10]
	chunk2 := content[10:]

	cmdID := "cmd-write-1"
	n1, err := fm.WriteChunk(cmdID, workloadID, "server.properties", chunk1, false, 0644)
	if err != nil {
		t.Fatalf("write chunk 1 failed: %v", err)
	}
	if n1 != int64(len(chunk1)) {
		t.Fatalf("expected %d written, got %d", len(chunk1), n1)
	}

	n2, err := fm.WriteChunk(cmdID, workloadID, "server.properties", chunk2, true, 0644)
	if err != nil {
		t.Fatalf("write chunk 2 failed: %v", err)
	}
	if n2 != int64(len(content)) {
		t.Fatalf("expected total %d written, got %d", len(content), n2)
	}

	// Verify file exists on disk with full content
	targetPath := filepath.Join(tempDir, "workloads", workloadID, "data", "server.properties")
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read target file: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Fatalf("content mismatch: got %q, want %q", string(data), string(content))
	}

	// 3. Stat file
	stat, err := fm.Stat(workloadID, "server.properties")
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if stat.Name != "server.properties" || stat.Size != int64(len(content)) || stat.IsDir {
		t.Fatalf("unexpected stat: %+v", stat)
	}

	// 4. Read file via stream chunks
	var readBuf bytes.Buffer
	var chunksReceived int
	err = fm.ReadFile(workloadID, "server.properties", func(chunk []byte, isLast bool, totalSize int64) error {
		chunksReceived++
		readBuf.Write(chunk)
		if totalSize != int64(len(content)) {
			t.Errorf("totalSize mismatch: got %d, want %d", totalSize, len(content))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}
	if !bytes.Equal(readBuf.Bytes(), content) {
		t.Fatalf("read content mismatch: got %q, want %q", readBuf.String(), string(content))
	}
	if chunksReceived == 0 {
		t.Fatal("expected at least 1 chunk")
	}

	// 5. List files
	files, err := fm.ListFiles(workloadID, "")
	if err != nil {
		t.Fatalf("list files failed: %v", err)
	}
	if len(files) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(files))
	}

	// 6. Delete file
	if err := fm.DeleteFile(workloadID, "server.properties", false); err != nil {
		t.Fatalf("delete file failed: %v", err)
	}

	// 7. Verify cannot delete root
	if err := fm.DeleteFile(workloadID, "", true); err == nil {
		t.Fatal("expected error deleting root directory")
	}
}
