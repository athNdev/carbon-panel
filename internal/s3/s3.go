// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package s3

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyendpoints "github.com/aws/smithy-go/endpoints"

	appconfig "github.com/athNdev/carbon-panel/internal/config"
)

// resolver routes requests to a custom endpoint (MinIO etc.).
type resolver struct{ endpoint, region string }

func (r resolver) ResolveEndpoint(_ context.Context, params s3.EndpointParameters) (smithyendpoints.Endpoint, error) {
	if r.endpoint == "" {
		return s3.NewDefaultEndpointResolverV2().ResolveEndpoint(context.Background(), params)
	}
	u, err := url.Parse(strings.TrimSuffix(r.endpoint, "/"))
	if err != nil {
		return smithyendpoints.Endpoint{}, err
	}
	return smithyendpoints.Endpoint{URI: *u}, nil
}

// Uploader uploads backup archives to S3-compatible storage (MINE-138).
type Uploader struct {
	client *s3.Client
	bucket string
	prefix string
}

// Enabled reports whether offsite upload is configured.
func Enabled(cfg appconfig.S3Config) bool {
	return cfg.Enabled && cfg.Bucket != ""
}

// NewUploader builds an uploader from config. Returns an error when the
// config is incomplete so callers fail fast at startup, not mid-backup.
func NewUploader(cfg appconfig.S3Config) (*Uploader, error) {
	if !Enabled(cfg) {
		return nil, fmt.Errorf("s3 backup not configured (enabled + bucket required)")
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	awsCfg := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.EndpointResolverV2 = resolver{endpoint: cfg.Endpoint, region: region}
		o.UsePathStyle = cfg.ForcePathStyle
	})
	return &Uploader{client: client, bucket: cfg.Bucket, prefix: strings.Trim(cfg.Prefix, "/")}, nil
}

// Key returns the object key for a backup archive.
func (u *Uploader) Key(serverID, filename string) string {
	if u.prefix == "" {
		return path.Join(serverID, filename)
	}
	return path.Join(u.prefix, serverID, filename)
}

// Upload streams a local archive via multipart upload. Returns object key.
func (u *Uploader) Upload(ctx context.Context, serverID, localPath string) (string, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	key := u.Key(serverID, path.Base(localPath))
	tm := transfermanager.New(u.client)
	if _, err := tm.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket: &u.bucket,
		Key:    &key,
		Body:   f,
	}); err != nil {
		return "", fmt.Errorf("s3 upload: %w", err)
	}
	return key, nil
}

// Download fetches an object key to a local path.
func (u *Uploader) Download(ctx context.Context, key, localPath string) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("create download dir: %w", err)
	}
	f, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	tm := transfermanager.New(u.client)
	if _, err := tm.DownloadObject(ctx, &transfermanager.DownloadObjectInput{
		Bucket:   &u.bucket,
		Key:      &key,
		WriterAt: f,
	}); err != nil {
		_ = f.Close()
		_ = os.Remove(localPath)
		return fmt.Errorf("s3 download: %w", err)
	}
	return nil
}

// Delete removes an object key from the configured S3 bucket.
func (u *Uploader) Delete(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	if _, err := u.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &u.bucket,
		Key:    &key,
	}); err != nil {
		return fmt.Errorf("s3 delete: %w", err)
	}
	return nil
}
