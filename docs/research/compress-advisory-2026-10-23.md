# klauspost/compress GHSA-259r-337f-4rfw review (2026-10-23)

## Change

`github.com/klauspost/compress` was upgraded from v1.18.6 to v1.18.7, the first patched release identified by the upstream advisory. The module is indirect in Carbon Panel's `go.mod`; it is selected transitively through `github.com/mholt/archives` (v0.1.5). No unrelated module versions were changed.

## Advisory facts

The upstream [GHSA-259r-337f-4rfw advisory](https://github.com/klauspost/compress/security/advisories/GHSA-259r-337f-4rfw) identifies package `github.com/klauspost/compress/s2`, affected versions `<= v1.18.6`, and patched version `v1.18.7`. It describes signed integer overflow in `s2.NewDict` for a dictionary containing an oversized uvarint repeat value, followed by an out-of-bounds access when `Dict.Encode` is called. The advisory rates it High (CVSS 3.1 7.5; availability impact). Advisory publication date: 2026-06-30.

## Carbon Panel dependency and call-path evidence

- `go mod why -m github.com/klauspost/compress` reports `pkg/files -> github.com/mholt/archives -> github.com/klauspost/compress/gzip` as the module-selection explanation.
- `pkg/files/files.go` calls `archives.Identify` and extracts through the selected archive extractor. In archives v0.1.5, `sz.go` registers S2/Snappy archive support and uses `s2.NewReader` and `s2.NewWriter`.
- `go list -deps ./cmd/carbon-panel` includes `github.com/klauspost/compress/s2`, because the archives package registers its format implementations.
- A Windows build of `./cmd/carbon-panel` succeeded. `go tool nm` on that executable produced no `s2.NewDict` or `Dict.Encode` symbol matches. Repository Go sources also contain no calls to `s2.NewDict` or `Dict.Encode`.

This establishes that the vulnerable package is included, while the reviewed Carbon Panel source and built executable show no direct path to the specific dictionary-construction/encoding sequence described by the advisory. It is not proof for every platform-specific or operator-specific binary variant, nor a generic guarantee about unreviewed future call paths. The patched release is therefore applied regardless of the current call-path finding.

## Verification

- `go test ./pkg/files` — passed.
- `go test github.com/mholt/archives` — passed (tests the resolved v0.1.5 archive module).
- `go build -o <temporary-path> ./cmd/carbon-panel` — passed; temporary executable removed after symbol inspection.
- `go list -m all` before the change selected compress v1.18.6; after the change, the selected version is v1.18.7.
- `govulncheck` was not available on `PATH`; no govulncheck result is claimed.

These checks cover the Carbon Panel file/archive integration and the archive library tests. They do not constitute an exhaustive application test run or a formal security audit.
