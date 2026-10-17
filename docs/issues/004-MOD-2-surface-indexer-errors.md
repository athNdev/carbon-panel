# MOD-2: Surface Modpack Indexer and Sync Errors in Model and RPC

**Status:** In Progress  
**Priority:** Medium  
**Category:** Modpacks / Observability  
**Component:** `proto/carbonpanel/v1/modpack.proto`, `internal/db/models.go`, `internal/rpc/services/modpack.go`  

## Problem Description
When modpack indexers or external API syncs fail (e.g. rate limits, network outages, bad API credentials), the error was logged on the server daemon, but the modpack database record remained in an ambiguous state without surfacing the specific failure message to the frontend UI or API consumers.

## Implementation Plan
1. In `proto/carbonpanel/v1/modpack.proto`:
   - Add `string index_error = 23;` to `IndexedModpack`.
2. In `internal/db/models.go`:
   - Add `IndexError string` column to `IndexedModpack` struct.
3. In `internal/rpc/services/modpack.go`:
   - Propagate `modpack.IndexError` into `protoModpack.IndexError`.
   - Update `IndexError` when `SyncModpacks` or `SyncModpackFiles` encounters an error, and clear it on successful sync.
