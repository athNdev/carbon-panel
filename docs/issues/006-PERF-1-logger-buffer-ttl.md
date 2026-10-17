<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->


# PERF-1: Bound Logger Memory Buffer with TTL and Pruning

**Status:** Completed & Verified  
**Priority:** Medium  
**Category:** Performance / Stability  
**Component:** `pkg/logger/logger.go`  

## Problem Description
Previously, the in-memory logger ring buffer kept up to 1000 lines unbounded by age. On long-running instances or low-traffic services, log lines from days or weeks ago remained in memory and would be bundled into diagnostic support dumps regardless of age.

## Implementation Details
1. In `pkg/logger/logger.go`:
   - Replaced plain string slice with timestamped `bufferEntry` records.
   - Enforced a default 24-hour retention TTL (`DefaultBufferTTL`) alongside the 1000-line capacity limit (`DefaultMaxBuffer`).
   - Implemented `pruneLocked(now time.Time)` which evicts entries older than `bufferTTL` during logging and when querying logs.
   - Added `GetRecentLogsSince(since time.Duration)` for retrieving log slices over a relative time window.
   - Added `SetBufferTTL(ttl time.Duration)` to allow dynamic configuration.
2. In `pkg/logger/logger_test.go`:
   - Added unit test suite verifying `MaxBuffer` bound enforcement, `BufferTTL` automatic eviction, `GetRecentLogsSince` filtering, `SetBufferTTL` runtime updates, and concurrent logging and reading under race detector.

## Verification
- `go test -v -race ./pkg/logger/...` passed with 0 race detections and 0 errors.
