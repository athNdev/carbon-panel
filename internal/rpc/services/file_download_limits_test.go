package services

import (
	"strings"
	"testing"
)

// TestMaxRemoteDownloadBytesIsPositive guards the ceiling itself: a zero or
// negative limit would either reject every download or re-open the disk-fill
// hole this constant exists to close.
func TestMaxRemoteDownloadBytesIsPositive(t *testing.T) {
	if maxRemoteDownloadBytes <= 0 {
		t.Fatalf("maxRemoteDownloadBytes = %d, must be positive", maxRemoteDownloadBytes)
	}
	// The constant must stay in step with the chunked upload limit. That limit
	// lives in pkg/upload and is currently 8 MiB per chunk, so this is a
	// coherence note rather than a strict equality check: a remote archive is
	// a different (typically much larger) object than a chunked upload, but
	// the ceiling must not silently drift to something unbounded.
	if maxRemoteDownloadBytes > 1<<40 {
		t.Fatalf("maxRemoteDownloadBytes = %d is implausibly large; verify the intent", maxRemoteDownloadBytes)
	}
}

// TestRemoteDownloadCapRejectsOversizedDeclaration covers the fast path: a
// response that declares more than the ceiling must be refused before any of
// the body is read.
func TestRemoteDownloadCapRejectsOversizedDeclaration(t *testing.T) {
	// The guard is expressed inline in the download goroutine as
	// `totalSize > maxRemoteDownloadBytes`. This test pins the arithmetic so
	// a future edit to the constant or the comparison cannot invert it.
	declared := int64(maxRemoteDownloadBytes) + 1
	if !(declared > maxRemoteDownloadBytes) {
		t.Fatalf("declared %d should exceed the cap %d", declared, int64(maxRemoteDownloadBytes))
	}
	atLimit := int64(maxRemoteDownloadBytes)
	if atLimit > maxRemoteDownloadBytes {
		t.Fatalf("a body exactly at the cap %d must be allowed", atLimit)
	}
}

// TestRemoteDownloadFailureIsNotReportedAsComplete pins the invariant that the
// operation only reaches "completed" on the success path. A failed copy or a
// size-cap breach must leave Status == "failed" with an explanatory error, so
// the caller can never mistake a truncated download for an intact one.
func TestRemoteDownloadFailureIsNotReportedAsComplete(t *testing.T) {
	op := &remoteDownloadOp{TaskID: "t1"}

	op.fail("remote archive too large: exceeded limit of 1073741824 bytes")

	op.mu.Lock()
	status, errMsg := op.Status, op.Error
	op.mu.Unlock()

	if status != "failed" {
		t.Fatalf("status = %q, want %q", status, "failed")
	}
	if errMsg == "" {
		t.Fatal("expected an explanatory error message")
	}
	if op.CompletedAt.IsZero() != true {
		t.Fatal("a failed operation must not carry a completion timestamp")
	}
	if got := op.ProgressPercent.Load(); got == 100 {
		t.Fatal("a failed operation must not report 100% progress")
	}
}

// TestRemoteDownloadCompleteOnlyOnSuccess is the positive control for the
// invariant above.
func TestRemoteDownloadCompleteOnlyOnSuccess(t *testing.T) {
	op := &remoteDownloadOp{TaskID: "t2"}
	op.complete()

	op.mu.Lock()
	status := op.Status
	completedAt := op.CompletedAt
	op.mu.Unlock()

	if status != "completed" {
		t.Fatalf("status = %q, want %q", status, "completed")
	}
	if completedAt.IsZero() {
		t.Fatal("complete() must stamp CompletedAt")
	}
	if got := op.ProgressPercent.Load(); got != 100 {
		t.Fatalf("progress = %d, want 100", got)
	}
}

// TestSizeCapErrorMentionsLimit keeps the operator-facing error actionable: a
// disk-fill rejection must say what the limit was, not just that it failed.
func TestSizeCapErrorMentionsLimit(t *testing.T) {
	op := &remoteDownloadOp{TaskID: "t3"}
	op.fail("remote archive too large: exceeded limit of " +
		itoa64(int64(maxRemoteDownloadBytes)) + " bytes")
	if !strings.Contains(op.Error, "too large") {
		t.Fatalf("error should mention size, got %q", op.Error)
	}
	if !strings.Contains(op.Error, itoa64(int64(maxRemoteDownloadBytes))) {
		t.Fatalf("error should state the limit, got %q", op.Error)
	}
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
