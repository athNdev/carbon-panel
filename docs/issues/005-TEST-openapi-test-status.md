<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->


# TEST-001: Align TestOpenAPIHandler_Responds Assertion with 404 Status

**Status:** Ready to Implement  
**Priority:** High  
**Category:** Testing & Release Hygiene  
**Component:** `internal/rpc/handlers/misc_test.go`  

## Problem Description
Commit `0007cafe` updated `internal/rpc/handlers/openapi.go` to return HTTP 404 Not Found instead of 500 when no OpenAPI spec is bundled or available on disk:
```go
w.Header().Set("Content-Type", "text/plain; charset=utf-8")
http.Error(w, "OpenAPI spec is not available in this build", http.StatusNotFound)
```
However, `internal/rpc/handlers/misc_test.go:44` still asserted that `res.StatusCode` must be 200 or 500, causing `go test ./internal/rpc/handlers` to fail with:
```
--- FAIL: TestOpenAPIHandler_Responds (0.00s)
    misc_test.go:44: status = 404
```

## Implementation Plan
Update `misc_test.go` to accept `http.StatusNotFound` (404) alongside `http.StatusOK` and `http.StatusInternalServerError`.
