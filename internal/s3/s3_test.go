package s3

import (
	"testing"

	appconfig "github.com/athNdev/carbon-panel/internal/config"
)

func TestEnabled(t *testing.T) {
	if Enabled(appconfig.S3Config{}) {
		t.Fatalf("empty config must be disabled")
	}
	if Enabled(appconfig.S3Config{Enabled: true}) {
		t.Fatalf("missing bucket must be disabled")
	}
	if !Enabled(appconfig.S3Config{Enabled: true, Bucket: "b"}) {
		t.Fatalf("complete config must be enabled")
	}
}

func TestNewUploaderValidation(t *testing.T) {
	if _, err := NewUploader(appconfig.S3Config{}); err == nil {
		t.Fatalf("expected error for empty config")
	}
	up, err := NewUploader(appconfig.S3Config{Enabled: true, Bucket: "b", Region: "eu-west-1", Prefix: "/backups/"})
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if got := up.Key("srv1", "a.zip"); got != "backups/srv1/a.zip" {
		t.Fatalf("unexpected key: %s", got)
	}
	up2, err := NewUploader(appconfig.S3Config{Enabled: true, Bucket: "b", Endpoint: "http://minio:9000", ForcePathStyle: true})
	if err != nil {
		t.Fatalf("endpoint config rejected: %v", err)
	}
	if got := up2.Key("srv1", "a.zip"); got != "srv1/a.zip" {
		t.Fatalf("unexpected key without prefix: %s", got)
	}
}
