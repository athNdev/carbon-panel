<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->


# DEP-2: Fail Snapshot Rollback Immediately if Container Stop Fails

**Status:** Completed & Verified  
**Priority:** High  
**Category:** Reliability & Data Safety  
**Component:** `internal/snapshot/snapshot.go`  

## Problem Description
During a server snapshot rollback, `Rollback()` stops the server container before extracting snapshot archive files over the server's data directory. Previously, if `StopContainer` failed or encountered an error, the failure was only logged with a warning, and the rollback proceeded to overwrite files while the Minecraft process was still actively writing to world files, level data, or database files. This risked silent and catastrophic data corruption.

## Implementation Details
1. In `internal/snapshot/snapshot.go:Rollback()`, enforce that if the server is running and `StopContainer` fails or the docker client is unavailable, the rollback aborts immediately with an explicit error:
   ```go
   if wasRunning && server.ContainerID != "" {
       if dockerCli == nil {
           return fmt.Errorf("cannot safely rollback: container %s is running but docker client is unavailable", server.ContainerID)
       }
       e.log.Info("Stopping container %s before rollback...", server.ContainerID)
       if _, err := dockerCli.StopContainer(ctx, server.ContainerID); err != nil {
           return fmt.Errorf("cannot safely rollback: failed to stop running container %s: %w", server.ContainerID, err)
       }
   }
   ```
2. Added unit test `TestSnapshot_Rollback_RunningContainerRequiresStop` in `internal/snapshot/snapshot_test.go` ensuring rollback is aborted without touching disk when container stopping cannot proceed safely.
