# MOD-4: Modpack and Instance Export SHA256 Checksum Verification

**Status:** In Progress  
**Priority:** High  
**Category:** Modpacks / Packaging  
**Component:** `internal/rpc/handlers/packwiz.go`, `internal/rpc/services/modpack.go`, `proto/carbonpanel/v1/modpack.proto`  

## Problem Description
When downloading modpack archives, instance zip files, or exported Packwiz/CurseForge/Modrinth bundles, clients had no cryptographic verification mechanism to ensure integrity. Flaky network transfers could lead to corrupted downloads and broken server or launcher instances without clear error diagnosis.

## Implementation Plan
1. In `proto/carbonpanel/v1/modpack.proto`:
   - Add `string sha256 = 7;` to `Version`.
   - Add `string sha256 = 11;` to `ModpackFile`.
   - Add `string sha256 = 3;` to `ImportRemoteModpackResponse`.
2. In `internal/rpc/handlers/packwiz.go`:
   - Compute SHA-256 hash when streaming exported modpack archives (`.packwiz.zip`, `.mrpack`, `.zip`).
   - Emit `X-Checksum-SHA256` and RFC 3230 `Digest: SHA-256=...` headers.
3. In `internal/rpc/services/modpack.go`:
   - Calculate SHA-256 digest of imported remote modpacks during `ImportRemoteModpack` and return in response.
