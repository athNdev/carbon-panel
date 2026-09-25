package metrics

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

func TestDefaultConfig_SaneIntervals(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.StatsInterval <= 0 || cfg.RCONInterval <= 0 || cfg.DiskInterval <= 0 {
		t.Errorf("non-positive interval in %+v", cfg)
	}
	if cfg.SLPInterval <= 0 || cfg.SLPTimeout <= 0 {
		t.Errorf("non-positive SLP timing in %+v", cfg)
	}
	if !cfg.SLPEnabled {
		t.Error("expected SLP enabled by default")
	}
}

func TestNewCollectorWithPool_Wiring(t *testing.T) {
	custom := CollectorConfig{
		StatsInterval: time.Second,
		RCONInterval:  2 * time.Second,
		DiskInterval:  3 * time.Second,
		SLPInterval:   4 * time.Second,
		SLPTimeout:    time.Second,
		SLPEnabled:    false,
	}

	c := NewCollectorWithPool(nil, nil, nil, nil, nil, nil, nil, custom)
	if c.metrics == nil || c.lifecycle == nil {
		t.Fatal("expected initialized maps")
	}
	if c.collectorConfig.StatsInterval != time.Second || c.collectorConfig.SLPEnabled {
		t.Errorf("custom config not applied: %+v", c.collectorConfig)
	}
	if c.pool != nil || c.docker != nil {
		t.Error("expected nil pool and docker client")
	}

	def := NewCollectorWithPool(nil, nil, nil, nil, nil, nil, nil)
	if def.collectorConfig != DefaultConfig() {
		t.Errorf("default config not applied: %+v", def.collectorConfig)
	}
}

func TestRegistry_UpdateGetRemove(t *testing.T) {
	c := NewCollectorWithPool(nil, nil, nil, nil, nil, nil, nil)

	if got := c.GetMetrics("srv-1"); got != nil {
		t.Fatalf("unknown server metrics = %+v, want nil", got)
	}

	c.updateMetrics("srv-1", func(m *ServerMetrics) {
		m.CPUPercent = 12.5
		m.PlayersOnline = 3
		m.LastUpdated = time.Now()
	})
	got := c.GetMetrics("srv-1")
	if got == nil || got.CPUPercent != 12.5 || got.PlayersOnline != 3 {
		t.Fatalf("metrics after update = %+v", got)
	}
	if got.ServerID != "srv-1" {
		t.Errorf("ServerID = %q, want srv-1", got.ServerID)
	}

	c.updateMetrics("srv-1", func(m *ServerMetrics) { m.CPUPercent = 99 })
	if got := c.GetMetrics("srv-1"); got.CPUPercent != 99 || got.PlayersOnline != 3 {
		t.Errorf("second update clobbered fields: %+v", got)
	}

	all := c.GetAllMetrics()
	if len(all) != 1 || all["srv-1"] == nil {
		t.Fatalf("GetAllMetrics = %v", all)
	}
	// Copy semantics: mutating the returned map must not affect the registry.
	delete(all, "srv-1")
	if got := c.GetMetrics("srv-1"); got == nil {
		t.Fatal("registry affected by caller mutating GetAllMetrics result")
	}

	c.RemoveMetrics("srv-1")
	if got := c.GetMetrics("srv-1"); got != nil {
		t.Errorf("metrics after remove = %+v, want nil", got)
	}
	// Removing an unknown server must not panic.
	c.RemoveMetrics("nope")
}

func TestResolveDocker_NilPoolFallsBackToDefault(t *testing.T) {
	c := NewCollectorWithPool(nil, nil, nil, nil, nil, nil, nil)
	if got := c.resolveDocker("node-1"); got != nil {
		t.Errorf("resolveDocker with no pool/client = %v, want nil", got)
	}
}

func TestCollectDiskUsage_MeasuresServerDir(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(filepath.Join(dataDir, "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("world-data-payload")
	if err := os.WriteFile(filepath.Join(dataDir, "world", "level.dat"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	store, err := db.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:           fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName),
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	if err := store.DB().Create(&db.Server{
		ID:       "srv-disk",
		Name:     "disk-test",
		DataPath: dataDir,
	}).Error; err != nil {
		t.Fatalf("failed to insert server: %v", err)
	}

	cfg := &config.Config{}
	cfg.Storage.DataDir = dir
	c := NewCollectorWithPool(store, nil, nil, nil, cfg, nil, logger.New())
	c.collectDiskUsage()

	got := c.GetMetrics("srv-disk")
	if got == nil {
		t.Fatal("no metrics recorded for server")
	}
	if got.DiskUsage < int64(len(payload)) {
		t.Errorf("DiskUsage = %d, want at least %d", got.DiskUsage, len(payload))
	}
	if got.DiskTotal <= 0 {
		t.Errorf("DiskTotal = %d, want positive", got.DiskTotal)
	}
}

func TestClearLifecycle_ForgetsBaseline(t *testing.T) {
	c := NewCollectorWithPool(nil, nil, nil, nil, nil, nil, nil)
	c.lifecycle["srv-1"] = lifecycleState{healthy: true, players: map[string]bool{"alice": true}}
	c.clearLifecycle("srv-1")
	if _, ok := c.lifecycle["srv-1"]; ok {
		t.Error("lifecycle baseline not cleared")
	}
	// Clearing an unknown server must not panic.
	c.clearLifecycle("nope")
}

func TestLifecycle_GetSetRoundTrip(t *testing.T) {
	c := NewCollectorWithPool(nil, nil, nil, nil, nil, nil, nil)

	if _, ok := c.getLifecycle("srv-1"); ok {
		t.Fatal("unexpected lifecycle baseline")
	}
	c.setLifecycle("srv-1", lifecycleState{healthy: true, players: map[string]bool{"alice": true}})
	st, ok := c.getLifecycle("srv-1")
	if !ok || !st.healthy || !st.players["alice"] {
		t.Errorf("lifecycle round-trip = %+v, %v", st, ok)
	}
	c.clearLifecycle("srv-1")
	if _, ok := c.getLifecycle("srv-1"); ok {
		t.Error("lifecycle baseline not cleared")
	}
}

func TestCurrentRoster_FromCachedSample(t *testing.T) {
	c := NewCollectorWithPool(nil, nil, nil, nil, nil, nil, nil)

	if roster := c.currentRoster("unknown"); roster != nil {
		t.Errorf("unknown server roster = %v, want nil", roster)
	}
	c.updateMetrics("srv-1", func(m *ServerMetrics) {
		m.PlayersOnline = 2
		m.PlayerSample = []string{"alice", "bob"}
	})
	roster := c.currentRoster("srv-1")
	if len(roster) != 2 || !roster["alice"] || !roster["bob"] {
		t.Errorf("roster = %v", roster)
	}

	// Stale sample (fewer names than the online count) is untrustworthy.
	c.updateMetrics("srv-2", func(m *ServerMetrics) {
		m.PlayersOnline = 3
		m.PlayerSample = []string{"alice"}
	})
	if roster := c.currentRoster("srv-2"); roster != nil {
		t.Errorf("stale roster = %v, want nil", roster)
	}
}

func TestDetectLifecycleEvents_NilBusIsNoOp(t *testing.T) {
	c := NewCollectorWithPool(nil, nil, nil, nil, nil, nil, nil)
	c.detectLifecycleEvents() // must return immediately without store/docker
	c.emit(t.Context(), 0, "srv-1", nil)
}

func TestStartStop_IdleCollector(t *testing.T) {
	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	store, err := db.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:           fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName),
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	cfg := &config.Config{}
	cfg.Storage.DataDir = t.TempDir()
	c := NewCollectorWithPool(store, nil, nil, nil, cfg, nil, logger.New())

	if err := c.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	// Second Start is a no-op.
	if err := c.Start(); err != nil {
		t.Fatalf("second Start failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	c.Stop()
	// Second Stop is a no-op.
	c.Stop()

	if n := len(c.GetAllMetrics()); n != 0 {
		t.Errorf("idle collector recorded %d metric entries", n)
	}
}
