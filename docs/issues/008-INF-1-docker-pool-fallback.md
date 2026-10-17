<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->


# INF-1: Docker Pool Node Fallback Hardening

## Overview
Previously, `ClientPool.GetClient(nodeID)` silently fell back to the local daemon (`defaultClient`) whenever a node could not be found in the store or client initialization failed. This could cause operations intended for a remote worker node to unintentionally execute against the local host.

## Changes Made
- Added `allowFallback bool` to `ClientPool` (defaulting to `false`).
- Provided `SetFallbackEnabled(enabled bool)`, `IsFallbackEnabled() bool`, and `GetDefaultNodeID() string`.
- Updated `GetClient` to return an explicit error on missing or failed nodes when fallback is disabled, while preserving fallback if explicitly enabled.
- Hardened `internal/rpc/services/server.go` and `internal/rpc/services/backup.go` with strict docker client resolution (`getDockerClientStrict` / `dockerForStrict`) for lifecycle mutations (`StartServer`, `StopServer`, `RestartServer`, `RecreateServer`, `MigrateServer`, async container creation, and `RestoreBackup`), returning `CodeUnavailable` rather than operating on the wrong host.
- Added comprehensive unit tests in `internal/docker/pool_test.go`.
