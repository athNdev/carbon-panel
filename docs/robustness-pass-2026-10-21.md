<!-- SPDX-License-Identifier: MPL-2.0 -->
# Robustness Pass — 2026-10-21

Scope: carbon-panel (OSS) + carbon-cloud web UIs after mobile-responsive pass.
Method: go vet/test, bun check/test, Vite production build, targeted source scans, prior QOL-REPORT.md.

## Completed in this pass

- Responsive/mobile UI: committed and pushed to main for both repos.
  - carbon-panel: `6474f86b`
  - carbon-cloud: `e830bf1a`
- Validation:
  - `go vet ./...` clean (after removing stray `CUsersDaybreak8458` directory).
  - `go test ./...` green except Windows-only upload temp-file lock + disk-space test environment failures.
  - `bun run check` 0 errors/0 warnings.
  - `bun test` 33 pass / 0 fail.
  - Vite production build success.

## Findings requiring Plane items

### Backend reliability
1. Unchecked write errors in packwiz import/export and proxy route refresh.
   - `internal/packwiz/importer.go:178,428`
   - `internal/packwiz/packwiz.go:696,768,1228`
   - `cmd/carbon-panel/main.go:246`
   Impact: partial file corruption or stale proxy route can go unnoticed.
   Effort: Low/Medium.

2. Upload manager tests are environment-fragile on Windows.
   - `pkg/upload` temp file handle cleanup and `1<<40` allocation test.
   Impact: CI noise on Windows runners.
   Effort: Low.

### Frontend robustness
3. No `catch` blocks in Svelte/TS source; Connect errors rely on global handling.
   - Need audit per route for user-visible error states and retry actions.
   Impact: Medium.
   Effort: Medium.

4. Loading states exist only in root layout and dashboard.
   - Most routes lack skeleton/empty/error states.
   Impact: Medium.
   Effort: Medium.

### Accessibility (from QOL-REPORT)
5. Remaining QOL accessibility items not yet closed:
   - UI-1 keyboard focus management
   - UI-2 icon-only button labels (partially covered by tests)
   - UI-3 color-only badges
   - UI-4 toast ARIA
   - UI-5 form validation linkage
   - UI-6 reduced motion
   - UI-7 table scope (covered by test)
   - UI-8 empty-state CTAs

### Observability/CI
6. No integration tests for scheduler+Docker+snapshot; no contract tests for ConnectRPC.
   - `CI-1`, `CI-2` from QOL-REPORT.

## Recommended Plane items (to be created in MINE)

| Suggested title | Area | Priority |
|---|---|---|
| Surface and handle unchecked packwiz/proxy write errors | Backend | High |
| Make upload tests OS-independent and avoid huge allocations | Testing | Medium |
| Add per-route error/empty/loading states to web UI | Frontend | Medium |
| Audit Connect error handling and retry affordances | Frontend | Medium |
| Close remaining accessibility gaps (focus, badges, forms, motion) | Frontend/a11y | Medium |
| Add scheduler/Docker/snapshot integration tests | CI | High |
| Add ConnectRPC breaking-change checks in CI | CI | Medium |

## Tracking

GitHub issues created in `athNdev/carbon-panel` for the suggested Plane items:
- #211 Close remaining accessibility gaps (focus, badges, forms, motion)
- #212 Surface and handle unchecked packwiz/proxy write errors
- #213 Add ConnectRPC breaking-change checks in CI
- #214 Add scheduler/Docker/snapshot integration tests
- #215 Add per-route error/empty/loading states to web UI
- #216 Audit Connect error handling and retry affordances
- #217 Make upload tests OS-independent and avoid huge allocations

## Progress in this pass

- Fixed unchecked packwiz/proxy write errors (#212): override extraction now returns errors, export override directory creation and modlist write are checked, ClonePack propagates filesystem errors, deep-sleep proxy route refresh logs failures.
- Made upload tests OS-independent (#217): added `defer m.Stop()` to release temp-file handles on Windows and reduced the unlimited-size test from 1 TiB to 1 MiB to avoid disk-space failures. Upload package tests now pass on Windows.
