package s3

import (
	"context"
	"testing"

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
