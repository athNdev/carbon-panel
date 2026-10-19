// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package s3

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	appconfig "github.com/athNdev/carbon-panel/internal/config"
)

func TestEnabledFlag(t *testing.T) {
	if Enabled(appconfig.S3Config{}) {
		t.Error("zero config enabled")
	}
	if Enabled(appconfig.S3Config{Enabled: true}) {
		t.Error("enabled without bucket")
	}
	if !Enabled(appconfig.S3Config{Enabled: true, Bucket: "b"}) {
		t.Error("enabled+bucket not enabled")
	}
}

func TestNewUploader_ValidationAndPrefix(t *testing.T) {
	if _, err := NewUploader(appconfig.S3Config{}); err == nil {
		t.Error("empty config accepted")
	}

	u, err := NewUploader(appconfig.S3Config{
		Enabled: true, Bucket: "b", Prefix: "/backups/",
		AccessKey: "ak", SecretKey: "sk",
		Endpoint: "https://minio.local:9000/",
	})
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if got := u.Key("srv-1", "nightly.zip"); got != "backups/srv-1/nightly.zip" {
		t.Errorf("key = %q", got)
	}

	plain, err := NewUploader(appconfig.S3Config{Enabled: true, Bucket: "b"})
	if err != nil {
		t.Fatalf("minimal config rejected: %v", err)
	}
	if got := plain.Key("srv-1", "a.zip"); got != "srv-1/a.zip" {
		t.Errorf("key without prefix = %q", got)
	}
}

func TestUpload_MissingFile(t *testing.T) {
	u, err := NewUploader(appconfig.S3Config{Enabled: true, Bucket: "b"})
	if err != nil {
		t.Fatalf("config rejected: %v", err)
	}
	if _, err := u.Upload(context.Background(), "srv-1", "/nonexistent/archive.zip"); err == nil {
		t.Error("missing file uploaded without error")
	}
}

func TestDelete_EmptyKey(t *testing.T) {
	u, err := NewUploader(appconfig.S3Config{Enabled: true, Bucket: "b"})
	if err != nil {
		t.Fatalf("config rejected: %v", err)
	}
	if err := u.Delete(context.Background(), ""); err != nil {
		t.Errorf("empty key delete returned error: %v", err)
	}
}

func TestDownload_FailureCleansUpLocalFile(t *testing.T) {
	u, err := NewUploader(appconfig.S3Config{
		Enabled:   true,
		Bucket:    "nonexistent-bucket",
		AccessKey: "ak",
		SecretKey: "sk",
		Endpoint:  "http://127.0.0.1:54321",
	})
	if err != nil {
		t.Fatalf("config rejected: %v", err)
	}

	nestedDir := filepath.Join(t.TempDir(), "deeply", "nested", "backups")
	localPath := filepath.Join(nestedDir, "test.zip")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = u.Download(ctx, "srv-1/test.zip", localPath)
	if err == nil {
		t.Fatal("expected download error against unreachable endpoint, got nil")
	}

	// Verify the directory was created
	if fi, statErr := os.Stat(nestedDir); statErr != nil || !fi.IsDir() {
		t.Errorf("expected download directory to be created: %v", statErr)
	}

	// Verify the failed local file was cleaned up and does not linger
	if _, statErr := os.Stat(localPath); !os.IsNotExist(statErr) {
		t.Errorf("expected failed local file to be cleaned up: %v", statErr)
	}
}
