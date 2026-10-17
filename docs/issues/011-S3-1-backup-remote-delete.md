<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->


# Issue 011: S3-1 - S3 Backup Remote Object Lifecycle Deletion

## Context & Problem
When backups are created with offsite S3 storage enabled (`internal/s3`), the archive is pushed to S3 and its object key is recorded in `BackupRecord.RemoteKey`. However, when a backup is removed via `BackupService.DeleteBackup`:
1. The local file on disk was removed.
2. The database record was removed.
3. The remote S3 object was never deleted because `internal/s3.Uploader` lacked a `Delete` method and `DeleteBackup` had no call to remove the remote object.

Over time, deleted backups accumulated indefinitely in the S3 bucket, causing storage bloat and unnecessary cloud costs.

## Solution
1. Added `Delete(ctx context.Context, key string) error` to `internal/s3.Uploader`:
   - Safely no-ops if `key == ""`.
   - Dispatches `s3.DeleteObject` against the configured S3 bucket.
2. Updated `BackupService.DeleteBackup` in `internal/rpc/services/backup.go`:
   - Checks if `rec.RemoteKey != ""` and `s3uploader.Enabled(s.s3cfg)`.
   - Initializes an uploader and invokes `up.Delete(ctx, rec.RemoteKey)`.
   - Warns on remote deletion errors without blocking local cleanup.
3. Added unit tests in `internal/s3/uploader_test.go` and `internal/rpc/services/backup_test.go`.
