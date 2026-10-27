// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package files

import (
	"context"
	"fmt"
	"os"
	"time"
)

// ExtractArchiveAtomic extracts an archive into a clean staging directory
// beside targetRoot and swaps it in only after extraction fully succeeds
// (OSS-235). A restore never merges into the live tree: stale files cannot
// survive a successful restore, and a failed extraction leaves the current
// tree untouched. The previous tree is moved aside for the duration of the
// swap and removed only after the new tree is in place; an interrupted swap
// leaves both trees on disk for recovery.
func ExtractArchiveAtomic(ctx context.Context, archivePath, targetRoot string) (int, error) {
	suffix := fmt.Sprintf("-%d", time.Now().UnixNano())
	staging := targetRoot + ".restore-stage" + suffix
	previous := targetRoot + ".restore-old" + suffix

	count, err := ExtractArchive(ctx, archivePath, staging, nil)
	if err != nil {
		_ = os.RemoveAll(staging)
		return 0, err
	}

	hadPrevious := false
	if _, err := os.Stat(targetRoot); err == nil {
		if err := os.Rename(targetRoot, previous); err != nil {
			_ = os.RemoveAll(staging)
			return 0, fmt.Errorf("preserve current data before restore: %w", err)
		}
		hadPrevious = true
	}
	if err := os.Rename(staging, targetRoot); err != nil {
		_ = os.RemoveAll(staging)
		if hadPrevious {
			if rerr := os.Rename(previous, targetRoot); rerr != nil {
				return 0, fmt.Errorf("activate restored tree: %v (also failed to move the previous tree back: %w)", err, rerr)
			}
		}
		return 0, fmt.Errorf("activate restored tree: %w", err)
	}
	if hadPrevious {
		_ = os.RemoveAll(previous)
	}
	return count, nil
}
