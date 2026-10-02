package services

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func newBundleTestService() *SupportService {
	return NewSupportService(nil, nil, nil, logger.New())
}

// TestSupportBundlesConcurrentAccess hammers the bundles map from many
// goroutines. On the pre-fix code (unguarded map shared by
// GenerateSupportBundle / DownloadSupportBundle / cleanupBundle) this fails
// under -race with "concurrent map writes" (or a fatal runtime panic); with
// the RWMutex guard it passes.
func TestSupportBundlesConcurrentAccess(t *testing.T) {
	svc := newBundleTestService()
	dir := t.TempDir()

	const workers = 32
	const iters = 100
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				id := fmt.Sprintf("bundle-%d-%d", w, i)
				path := filepath.Join(dir, id+".tar.gz")
				if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
					t.Errorf("write temp bundle: %v", err)
					return
				}
				svc.storeBundle(&BundleInfo{
					ID:        id,
					Filename:  id + ".tar.gz",
					Path:      path,
					Size:      4,
					CreatedAt: time.Now(),
				})
				if _, ok := svc.loadBundle(id); !ok {
					t.Errorf("loadBundle(%s) missed immediately after store", id)
					return
				}
				// Concurrent lookups of ids that may or may not exist yet.
				_, _ = svc.loadBundle(fmt.Sprintf("bundle-%d-%d", (w+1)%workers, i))
				svc.cleanupBundle(id)
			}
		}(w)
	}
	wg.Wait()

	svc.bundlesMu.RLock()
	remaining := len(svc.bundles)
	svc.bundlesMu.RUnlock()
	if remaining != 0 {
		t.Errorf("expected all bundles cleaned up, %d remain", remaining)
	}
}

// TestDownloadSupportBundleRoundTrip exercises the locked Download path end
// to end: snapshot under RLock, file I/O unlocked, synchronous cleanup.
func TestDownloadSupportBundleRoundTrip(t *testing.T) {
	svc := newBundleTestService()
	path := filepath.Join(t.TempDir(), "b.tar.gz")
	content := []byte("bundle-bytes")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	svc.storeBundle(&BundleInfo{ID: "b1", Filename: "b.tar.gz", Path: path, Size: int64(len(content)), CreatedAt: time.Now()})

	resp, err := svc.DownloadSupportBundle(t.Context(), connect.NewRequest(&v1.DownloadSupportBundleRequest{BundleId: "b1"}))
	if err != nil {
		t.Fatalf("DownloadSupportBundle: %v", err)
	}
	if string(resp.Msg.Content) != string(content) {
		t.Errorf("downloaded %q, want %q", resp.Msg.Content, content)
	}
	if _, ok := svc.loadBundle("b1"); ok {
		t.Errorf("bundle b1 should be cleaned up after download")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("bundle file should be removed after download")
	}

	// Second download must 404.
	if _, err := svc.DownloadSupportBundle(t.Context(), connect.NewRequest(&v1.DownloadSupportBundleRequest{BundleId: "b1"})); err == nil {
		t.Errorf("second download of consumed bundle should fail")
	}
}

// TestExpiredBundlesEvictedWithoutSleepers stores an expired bundle and
// asserts the opportunistic eviction on the store path removes it — with no
// per-bundle sleeper goroutine created (the pre-fix code spawned one
// time.Sleep(1h) goroutine per bundle).
func TestExpiredBundlesEvictedWithoutSleepers(t *testing.T) {
	svc := newBundleTestService()
	dir := t.TempDir()

	stalePath := filepath.Join(dir, "stale.tar.gz")
	if err := os.WriteFile(stalePath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc.bundlesMu.Lock()
	svc.bundles["stale"] = &BundleInfo{ID: "stale", Filename: "stale.tar.gz", Path: stalePath, CreatedAt: time.Now().Add(-2 * time.Hour)}
	svc.bundlesMu.Unlock()

	before := runtime.NumGoroutine()
	for i := 0; i < 20; i++ {
		p := filepath.Join(dir, fmt.Sprintf("fresh-%d.tar.gz", i))
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		svc.storeBundle(&BundleInfo{ID: fmt.Sprintf("fresh-%d", i), Filename: "f.tar.gz", Path: p, CreatedAt: time.Now()})
	}
	// Give any stray sleeper a chance to be scheduled, then compare.
	time.Sleep(50 * time.Millisecond)
	if got := runtime.NumGoroutine(); got > before+2 {
		t.Errorf("goroutines grew from %d to %d: per-bundle sleepers must not be created", before, got)
	}

	if _, ok := svc.loadBundle("stale"); ok {
		t.Errorf("expired bundle was not evicted by the store path")
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Errorf("expired bundle file was not removed")
	}
	if _, ok := svc.loadBundle("fresh-19"); !ok {
		t.Errorf("fresh bundle should survive eviction")
	}
}
