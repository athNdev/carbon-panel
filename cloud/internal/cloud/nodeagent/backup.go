package nodeagent

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// BackupManager handles local on-disk creation, restoration, and deletion of workload backups.
type BackupManager struct {
	dataDir string
	mu      sync.Mutex
}

// NewBackupManager creates a new BackupManager rooted in dataDir.
func NewBackupManager(dataDir string) *BackupManager {
	return &BackupManager{
		dataDir: dataDir,
	}
}

// validateIDs ensures workloadID and backupID are non-empty and do not contain traversal characters.
func validateIDs(workloadID, backupID string) error {
	if strings.TrimSpace(workloadID) == "" || strings.Contains(workloadID, "..") || strings.ContainsAny(workloadID, "/\\") {
		return errors.New("invalid workload_id")
	}
	if strings.TrimSpace(backupID) == "" || strings.Contains(backupID, "..") || strings.ContainsAny(backupID, "/\\") {
		return errors.New("invalid backup_id")
	}
	return nil
}

// CreateBackup archives the workload's data directory into backups/<backupID>.tar.gz.
func (b *BackupManager) CreateBackup(workloadID, backupID string) (sizeBytes int64, sha256Hex string, err error) {
	if err := validateIDs(workloadID, backupID); err != nil {
		return 0, "", err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	dataDir := filepath.Join(b.dataDir, "workloads", workloadID, "data")
	backupsDir := filepath.Join(b.dataDir, "workloads", workloadID, "backups")

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return 0, "", fmt.Errorf("create data dir: %w", err)
	}
	if err := os.MkdirAll(backupsDir, 0755); err != nil {
		return 0, "", fmt.Errorf("create backups dir: %w", err)
	}

	tmpArchive := filepath.Join(backupsDir, fmt.Sprintf("%s.tmp", backupID))
	finalArchive := filepath.Join(backupsDir, fmt.Sprintf("%s.tar.gz", backupID))

	f, err := os.OpenFile(tmpArchive, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return 0, "", fmt.Errorf("create tmp archive: %w", err)
	}

	cleanupTmp := true
	defer func() {
		_ = f.Close()
		if cleanupTmp {
			_ = os.Remove(tmpArchive)
		}
	}()

	hasher := sha256.New()
	mw := io.MultiWriter(f, hasher)
	gw := gzip.NewWriter(mw)
	tw := tar.NewWriter(gw)

	walkErr := filepath.Walk(dataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dataDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return fmt.Errorf("tar header for %s: %w", rel, err)
		}
		header.Name = filepath.ToSlash(rel)

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("tar write header for %s: %w", rel, err)
		}

		if info.Mode().IsRegular() {
			src, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("open file %s: %w", rel, err)
			}
			defer func() { _ = src.Close() }()
			if _, err := io.Copy(tw, src); err != nil {
				return fmt.Errorf("copy file %s: %w", rel, err)
			}
		}
		return nil
	})

	if walkErr != nil {
		return 0, "", walkErr
	}

	if err := tw.Close(); err != nil {
		return 0, "", fmt.Errorf("close tar: %w", err)
	}
	if err := gw.Close(); err != nil {
		return 0, "", fmt.Errorf("close gzip: %w", err)
	}
	if err := f.Close(); err != nil {
		return 0, "", fmt.Errorf("close file: %w", err)
	}

	if err := os.Rename(tmpArchive, finalArchive); err != nil {
		return 0, "", fmt.Errorf("rename archive: %w", err)
	}
	cleanupTmp = false

	stat, err := os.Stat(finalArchive)
	if err != nil {
		return 0, "", fmt.Errorf("stat archive: %w", err)
	}

	return stat.Size(), hex.EncodeToString(hasher.Sum(nil)), nil
}

// RestoreBackup unpacks backups/<backupID>.tar.gz over the workload's data directory.
func (b *BackupManager) RestoreBackup(workloadID, backupID string) error {
	if err := validateIDs(workloadID, backupID); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	archivePath := filepath.Join(b.dataDir, "workloads", workloadID, "backups", fmt.Sprintf("%s.tar.gz", backupID))
	dataDir := filepath.Join(b.dataDir, "workloads", workloadID, "data")

	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open backup archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)

	stagingDir := filepath.Join(b.dataDir, "workloads", workloadID, fmt.Sprintf("data_restore_%s", backupID))
	_ = os.RemoveAll(stagingDir)
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(stagingDir) }()

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar archive: %w", err)
		}

		cleanName := filepath.Clean(hdr.Name)
		if strings.Contains(cleanName, "..") || filepath.IsAbs(cleanName) {
			return fmt.Errorf("illegal path in archive: %s", hdr.Name)
		}

		target := filepath.Join(stagingDir, filepath.FromSlash(cleanName))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("create dir %s: %w", cleanName, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("create parent dir for %s: %w", cleanName, err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, hdr.FileInfo().Mode())
			if err != nil {
				return fmt.Errorf("create file %s: %w", cleanName, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return fmt.Errorf("extract file %s: %w", cleanName, err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close extracted file %s: %w", cleanName, err)
			}
		}
	}

	backupOldData := filepath.Join(b.dataDir, "workloads", workloadID, "data_old")
	_ = os.RemoveAll(backupOldData)
	if err := os.Rename(dataDir, backupOldData); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stash old data: %w", err)
	}
	if err := os.Rename(stagingDir, dataDir); err != nil {
		_ = os.Rename(backupOldData, dataDir)
		return fmt.Errorf("swap restored data: %w", err)
	}
	_ = os.RemoveAll(backupOldData)
	return nil
}

// DeleteBackup removes backups/<backupID>.tar.gz.
func (b *BackupManager) DeleteBackup(workloadID, backupID string) error {
	if err := validateIDs(workloadID, backupID); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	archivePath := filepath.Join(b.dataDir, "workloads", workloadID, "backups", fmt.Sprintf("%s.tar.gz", backupID))
	if err := os.Remove(archivePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete backup file: %w", err)
	}
	return nil
}
