package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/events"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if c.StatsInterval != 5*time.Second || c.RCONInterval != 10*time.Second ||
		c.DiskInterval != 60*time.Second || c.SLPInterval != 15*time.Second ||
		c.SLPTimeout != 5*time.Second || !c.SLPEnabled {
		t.Fatalf("config=%+v", c)
	}
}

func TestNewCollectorWiring(t *testing.T) {
	// All-nil construction must not panic; defaults apply.
	c := NewCollectorWithPool(nil, nil, nil, nil, nil, nil, nil)
	if c == nil || c.metrics == nil || c.lifecycle == nil {
		t.Fatal("collector maps must be initialized")
	}
	if got := c.collectorConfig; got != DefaultConfig() {
		t.Fatalf("config=%+v", got)
	}
	if c.resolveDocker("any") != nil {
		t.Fatal("no docker source: must resolve nil")
	}

	custom := CollectorConfig{StatsInterval: time.Second}
	c2 := NewCollectorWithPool(nil, nil, nil, nil, nil, nil, nil, custom)
	if c2.collectorConfig.StatsInterval != time.Second {
		t.Fatalf("custom config not honored: %+v", c2.collectorConfig)
	}

	c3 := NewCollector(nil, nil, nil, nil, nil, nil)
	if c3 == nil {
		t.Fatal("NewCollector must delegate")
	}
}

func TestUpdateGetRemoveMetrics(t *testing.T) {
	c := NewCollectorWithPool(nil, nil, nil, nil, nil, nil, nil)

	if got := c.GetMetrics("missing"); got != nil {
		t.Fatalf("missing=%v", got)
	}

	c.updateMetrics("s1", func(m *ServerMetrics) { m.PlayersOnline = 3 })
	if got := c.GetMetrics("s1"); got == nil || got.PlayersOnline != 3 || got.ServerID != "s1" {
		t.Fatalf("metrics=%+v", got)
	}
	c.updateMetrics("s1", func(m *ServerMetrics) { m.PlayersOnline = 5 })
	if got := c.GetMetrics("s1"); got.PlayersOnline != 5 {
		t.Fatalf("update not applied: %+v", got)
	}

	// GetAll returns a copy: mutating it must not affect the registry.
	all := c.GetAllMetrics()
	if len(all) != 1 {
		t.Fatalf("all=%v", all)
	}
	delete(all, "s1")
	if got := c.GetMetrics("s1"); got == nil {
		t.Fatal("registry mutated through copy")
	}

	c.RemoveMetrics("s1")
	if got := c.GetMetrics("s1"); got != nil {
		t.Fatalf("removed=%v", got)
	}
	c.RemoveMetrics("missing") // no-op, must not panic
}

func TestPruneStaleEntries(t *testing.T) {
	c := NewCollectorWithPool(nil, nil, nil, nil, nil, nil, nil)

	c.updateMetrics("fresh", func(m *ServerMetrics) { m.LastUpdated = time.Now() })
	c.updateMetrics("stale", func(m *ServerMetrics) { m.LastUpdated = time.Now().Add(-time.Hour) })
	c.mu.Lock()
	c.metrics["nilentry"] = nil
	c.mu.Unlock()

	if n := c.PruneStale(time.Minute); n != 2 {
		t.Fatalf("evicted=%d want 2", n)
	}
	if got := c.GetMetrics("fresh"); got == nil {
		t.Fatal("fresh entry must survive")
	}
	if got := c.GetMetrics("stale"); got != nil {
		t.Fatal("stale entry must go")
	}
	if n := c.PruneStale(time.Minute); n != 0 {
		t.Fatalf("second prune=%d want 0", n)
	}
}

func TestLifecycleRoundTrip(t *testing.T) {
	c := NewCollectorWithPool(nil, nil, nil, nil, nil, nil, nil)
	if _, ok := c.getLifecycle("s1"); ok {
		t.Fatal("must be absent")
	}
	c.setLifecycle("s1", lifecycleState{healthy: true, players: map[string]bool{"a": true}})
	st, ok := c.getLifecycle("s1")
	if !ok || !st.healthy || !st.players["a"] {
		t.Fatalf("st=%+v ok=%v", st, ok)
	}
	c.clearLifecycle("s1")
	if _, ok := c.getLifecycle("s1"); ok {
		t.Fatal("must be cleared")
	}
}

func TestCurrentRoster(t *testing.T) {
	c := NewCollectorWithPool(nil, nil, nil, nil, nil, nil, nil)
	if got := c.currentRoster("missing"); got != nil {
		t.Fatalf("missing=%v", got)
	}
	c.updateMetrics("s1", func(m *ServerMetrics) { m.PlayersOnline = 3; m.PlayerSample = []string{"a"} })
	if got := c.currentRoster("s1"); got != nil {
		t.Fatalf("short sample=%v", got)
	}
	c.updateMetrics("s1", func(m *ServerMetrics) { m.PlayersOnline = 2; m.PlayerSample = []string{"a", "b", ""} })
	got := c.currentRoster("s1")
	if len(got) != 2 || !got["a"] || !got["b"] {
		t.Fatalf("roster=%v", got)
	}
}

func TestEmit(t *testing.T) {
	c := NewCollectorWithPool(nil, nil, nil, nil, nil, nil, nil)
	// Nil bus: no-op, must not panic.
	c.emit(context.Background(), v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_HEALTHY, "s1", nil)

	bus := events.NewBus(logger.New())
	c2 := NewCollectorWithPool(nil, nil, nil, nil, nil, bus, logger.New())
	var got []events.Event
	bus.Subscribe(func(_ context.Context, e events.Event) { got = append(got, e) })
	c2.emit(context.Background(), v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_PLAYER_JOIN, "s9", map[string]any{"player": "z"})
	if len(got) != 1 || got[0].ServerID != "s9" {
		t.Fatalf("events=%+v", got)
	}
}

func TestStartStopEmptyStore(t *testing.T) {
	store, err := db.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:           "file:metrics-start-stop?mode=memory&cache=shared",
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := CollectorConfig{
		StatsInterval: 10 * time.Millisecond,
		RCONInterval:  10 * time.Millisecond,
		DiskInterval:  10 * time.Millisecond,
		SLPInterval:   10 * time.Millisecond,
		SLPEnabled:    true,
	}
	minCfg := &config.Config{}
	minCfg.Storage.DataDir = t.TempDir()
	c := NewCollectorWithPool(store, nil, nil, nil, minCfg, nil, logger.New(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	c.Stop()
	c.Stop() // idempotent
	if n := c.PruneStale(time.Hour); n != 0 {
		t.Fatalf("prune=%d", n)
	}
}
