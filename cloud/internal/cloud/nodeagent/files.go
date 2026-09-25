package nodeagent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
)

const (
	// MaxChunkSize is the maximum size for a streaming file chunk (64KB).
	MaxChunkSize = 64 * 1024
)

// FileManager handles sandboxed file operations for workloads on the node.
type FileManager struct {
	dataDir string
	mu      sync.Mutex
	writers map[string]*activeWriter
}

type activeWriter struct {
	file       *os.File
	tmpPath    string
	targetPath string
	written    int64
	mode       os.FileMode
}

// NewFileManager creates a FileManager rooted in dataDir.
func NewFileManager(dataDir string) *FileManager {
	return &FileManager{
		dataDir: dataDir,
		writers: make(map[string]*activeWriter),
	}
}

// resolveWorkloadPath validates and sandboxes relPath inside the workload's data root.
func (m *FileManager) resolveWorkloadPath(workloadID, relPath string) (baseDir, targetPath string, err error) {
	if strings.TrimSpace(workloadID) == "" {
		return "", "", errors.New("workload_id is required")
	}

	// Strictly reject any path containing parent directory traversal
	if strings.Contains(relPath, "..") {
		return "", "", fmt.Errorf("path traversal not permitted: %s", relPath)
	}

	baseDir = filepath.Clean(filepath.Join(m.dataDir, "workloads", workloadID, "data"))
	cleanRel := strings.TrimPrefix(filepath.Clean("/"+filepath.ToSlash(relPath)), "/")
	targetPath = filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(cleanRel)))

	// Strict containment check: targetPath must be baseDir or under baseDir
	if targetPath != baseDir && !strings.HasPrefix(targetPath, baseDir+string(filepath.Separator)) {
		return "", "", fmt.Errorf("path escapes workload sandbox: %s", relPath)
	}

	return baseDir, targetPath, nil
}

// ListFiles lists directory contents for a workload path.
func (m *FileManager) ListFiles(workloadID, relPath string) ([]*v1.FileInfo, error) {
	baseDir, targetPath, err := m.resolveWorkloadPath(workloadID, relPath)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("ensure workload root: %w", err)
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("directory not found: %s", relPath)
		}
		return nil, fmt.Errorf("stat path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", relPath)
	}

	entries, err := os.ReadDir(targetPath)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	results := make([]*v1.FileInfo, 0, len(entries))
	for _, entry := range entries {
		eInfo, err := entry.Info()
		if err != nil {
			continue
		}

		childRel := filepath.ToSlash(filepath.Join(relPath, entry.Name()))
		childRel = strings.TrimPrefix(childRel, "/")

		results = append(results, &v1.FileInfo{
			Name:           entry.Name(),
			Path:           childRel,
			IsDir:          entry.IsDir(),
			Size:           eInfo.Size(),
			ModifiedAtUnix: eInfo.ModTime().Unix(),
			Mode:           uint32(eInfo.Mode().Perm()),
		})
	}

	return results, nil
}

// Stat returns metadata for a single file or directory.
func (m *FileManager) Stat(workloadID, relPath string) (*v1.FileInfo, error) {
	_, targetPath, err := m.resolveWorkloadPath(workloadID, relPath)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return nil, err
	}

	cleanRel := strings.TrimPrefix(filepath.ToSlash(filepath.Clean("/"+relPath)), "/")

	return &v1.FileInfo{
		Name:           info.Name(),
		Path:           cleanRel,
		IsDir:          info.IsDir(),
		Size:           info.Size(),
		ModifiedAtUnix: info.ModTime().Unix(),
		Mode:           uint32(info.Mode().Perm()),
	}, nil
}

// ReadFile streams a file in bounded 64KB chunks to chunkFunc.
func (m *FileManager) ReadFile(workloadID, relPath string, chunkFunc func(chunk []byte, isLast bool, totalSize int64) error) error {
	_, targetPath, err := m.resolveWorkloadPath(workloadID, relPath)
	if err != nil {
		return err
	}

	file, err := os.Open(targetPath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}
	if stat.IsDir() {
		return fmt.Errorf("cannot read directory as file: %s", relPath)
	}

	totalSize := stat.Size()
	if totalSize == 0 {
		return chunkFunc([]byte{}, true, 0)
	}

	buf := make([]byte, MaxChunkSize)
	var readBytes int64

	for {
		n, rErr := file.Read(buf)
		if n > 0 {
			readBytes += int64(n)
			isLast := (rErr == io.EOF) || (readBytes >= totalSize)
			chunkCopy := make([]byte, n)
			copy(chunkCopy, buf[:n])
			if err := chunkFunc(chunkCopy, isLast, totalSize); err != nil {
				return err
			}
		}
		if rErr != nil {
			if errors.Is(rErr, io.EOF) {
				break
			}
			return fmt.Errorf("read file chunk: %w", rErr)
		}
	}

	return nil
}

// WriteChunk writes a chunk of data for commandID. When isLast is true, it finalizes
// the atomic write by syncing and renaming the temp file.
func (m *FileManager) WriteChunk(commandID, workloadID, relPath string, chunk []byte, isLast bool, mode uint32) (int64, error) {
	if strings.TrimSpace(commandID) == "" {
		return 0, errors.New("command_id is required")
	}

	m.mu.Lock()
	writer, exists := m.writers[commandID]
	if !exists {
		baseDir, targetPath, err := m.resolveWorkloadPath(workloadID, relPath)
		if err != nil {
			m.mu.Unlock()
			return 0, err
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			m.mu.Unlock()
			return 0, fmt.Errorf("create parent dir: %w", err)
		}

		tmpDir := filepath.Join(baseDir, ".tmp")
		if err := os.MkdirAll(tmpDir, 0700); err != nil {
			m.mu.Unlock()
			return 0, fmt.Errorf("create tmp dir: %w", err)
		}

		tmpFile, err := os.CreateTemp(tmpDir, "upload-*.tmp")
		if err != nil {
			m.mu.Unlock()
			return 0, fmt.Errorf("create temp file: %w", err)
		}

		perm := os.FileMode(0644)
		if mode > 0 {
			perm = os.FileMode(mode)
		}

		writer = &activeWriter{
			file:       tmpFile,
			tmpPath:    tmpFile.Name(),
			targetPath: targetPath,
			mode:       perm,
		}
		m.writers[commandID] = writer
	}
	m.mu.Unlock()

	// Write chunk
	if len(chunk) > 0 {
		n, err := writer.file.Write(chunk)
		if err != nil {
			m.cleanupWriter(commandID)
			return 0, fmt.Errorf("write chunk: %w", err)
		}
		writer.written += int64(n)
	}

	if isLast {
		defer m.cleanupWriter(commandID)

		if err := writer.file.Sync(); err != nil {
			return 0, fmt.Errorf("sync temp file: %w", err)
		}
		if err := writer.file.Close(); err != nil {
			return 0, fmt.Errorf("close temp file: %w", err)
		}
		writer.file = nil

		if err := os.Chmod(writer.tmpPath, writer.mode); err != nil {
			_ = os.Remove(writer.tmpPath)
			return 0, fmt.Errorf("chmod file: %w", err)
		}

		if err := os.Rename(writer.tmpPath, writer.targetPath); err != nil {
			_ = os.Remove(writer.tmpPath)
			return 0, fmt.Errorf("atomic rename to target: %w", err)
		}

		return writer.written, nil
	}

	return writer.written, nil
}

// AbortWrite cancels and removes an in-progress temp file.
func (m *FileManager) AbortWrite(commandID string) {
	m.cleanupWriter(commandID)
}

func (m *FileManager) cleanupWriter(commandID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	writer, exists := m.writers[commandID]
	if !exists {
		return
	}
	delete(m.writers, commandID)
	if writer.file != nil {
		_ = writer.file.Close()
	}
	_ = os.Remove(writer.tmpPath)
}

// DeleteFile removes a file or directory. Root directory deletion is rejected.
func (m *FileManager) DeleteFile(workloadID, relPath string, recursive bool) error {
	baseDir, targetPath, err := m.resolveWorkloadPath(workloadID, relPath)
	if err != nil {
		return err
	}

	// Strictly reject deleting the root workload directory itself
	if targetPath == baseDir {
		return errors.New("cannot delete workload root directory")
	}

	if recursive {
		return os.RemoveAll(targetPath)
	}
	return os.Remove(targetPath)
}

// CreateDirectory creates a directory and any parent directories inside the workload data root.
func (m *FileManager) CreateDirectory(workloadID, relPath string) error {
	_, targetPath, err := m.resolveWorkloadPath(workloadID, relPath)
	if err != nil {
		return err
	}

	return os.MkdirAll(targetPath, 0755)
}
