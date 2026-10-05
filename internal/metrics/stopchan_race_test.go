package metrics

import (
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// newRaceTestCollector builds a collector wired to an in-memory store with
// short intervals, so every loop is actively selecting while the collector
// runs.
func newRaceTestCollector(t *testing.T, dbName string) *Collector {
	t.Helper()
	store, err := db.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:           "file:" + dbName + "?mode=memory&cache=shared",
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
		StatsInterval: time.Millisecond,
		RCONInterval:  time.Millisecond,
		DiskInterval:  time.Millisecond,
		SLPInterval:   time.Millisecond,
		SLPEnabled:    true,
	}
	minCfg := &config.Config{}
	minCfg.Storage.DataDir = t.TempDir()
	return NewCollectorWithPool(store, nil, nil, nil, minCfg, nil, logger.New(), cfg)
}

// TestSequentialStartStopCyclesDoNotRace guards the stopChan data-race fix.
//
// Start assigned c.stopChan under c.mu, but the five collector loops read
// c.stopChan directly inside their select WITHOUT holding the lock. That is a
// data race by the Go memory model even where it is not currently reachable:
// Start is guarded by `if c.running`, and Stop's wg.Wait() happens-before the
// next Start, so today's control flow keeps the read and the write apart. The
// fix removes the unsynchronized field access entirely, so the loops cannot
// become racy if that guard is ever relaxed or a Stop is short-circuited.
//
// Honesty note: with the guard in place this test does NOT reproduce a live
// race — reverting the loops to read c.stopChan still passes under -race. It
// is a regression guard, not a reproducer. The behavioural half is asserted by
// TestCollectorLoopsObserveTheirStopChannel below.
//
// This exercises the supported lifecycle (Start, run, Stop, restart) across
// several cycles so the field is reassigned repeatedly while loops come and go.
// Run under -race.
//
// NOTE: deliberately NOT testing concurrent Start+Stop. Start is guarded by
// `if c.running`, and Stop's wg.Wait() cannot overlap a later Start's wg.Add —
// so reentrant Start/Stop is unsupported by design and testing it deadlocks on
// that separate concern, which this fix does not address.
func TestSequentialStartStopCyclesDoNotRace(t *testing.T) {
	c := newRaceTestCollector(t, "metrics-stopchan-race")

	for cycle := 0; cycle < 5; cycle++ {
		if err := c.Start(); err != nil {
			t.Fatalf("cycle %d Start: %v", cycle, err)
		}
		// Let every loop reach its select and tick at least once.
		time.Sleep(10 * time.Millisecond)
		c.Stop()
		// Stop waits on the WaitGroup, so the previous cycle's loops have
		// definitely exited before the next Start reassigns stopChan.
	}

	// The collector must be left cleanly stoppable and fully drained.
	c.Stop()

	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("collector loops did not exit; a loop missed its stop signal")
	}
}

// TestCollectorLoopsObserveTheirStopChannel pins the behavioural half of the
// fix: each loop must exit when the channel it was HANDED is closed, with no
// dependence on reading the struct field. This is what guarantees a loop
// cannot miss its stop signal.
func TestCollectorLoopsObserveTheirStopChannel(t *testing.T) {
	c := newRaceTestCollector(t, "metrics-loop-stop")

	// Snapshot the channel exactly the way Start does, then hand it to the
	// loops. Closing it must terminate every loop.
	c.mu.Lock()
	stop := make(chan struct{})
	c.stopChan = stop
	c.mu.Unlock()

	type loopFn func(<-chan struct{})
	loops := []struct {
		name string
		fn   loopFn
	}{
		{"docker", c.collectDockerStatsLoop},
		{"rcon", c.collectRCONDataLoop},
		{"disk", c.collectDiskUsageLoop},
		{"lifecycle", c.collectLifecycleEventsLoop},
		{"slp", c.collectSLPDataLoop},
	}

	exited := make(chan string, len(loops))
	for _, l := range loops {
		c.wg.Add(1)
		go func(name string, fn loopFn) {
			defer func() { exited <- name }()
			fn(stop)
		}(l.name, l.fn)
	}

	// Let them all enter their select.
	time.Sleep(20 * time.Millisecond)
	close(stop)

	for range loops {
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			t.Fatal("a collector loop ignored its stop channel")
		}
	}
}
