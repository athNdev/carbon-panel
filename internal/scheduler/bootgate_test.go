package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/google/uuid"
)

func TestWaitForBootTimeout(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()
	appCfg := &config.Config{}
	s := NewScheduler(store, nil, nil, appCfg, nil, logger.New(), Config{CheckInterval: time.Hour})
	server := &storage.Server{ID: uuid.New().String(), Name: "boot", Port: 1, MCVersion: "1.21"}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	err := s.waitForBoot(ctx, server, 0)
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	// Default 3min gate must respect context cancellation, not hang.
	if time.Since(start) > 30*time.Second {
		t.Fatalf("gate ignored context cancellation")
	}
}

func TestVerifyBootSkippedWithoutSnapshot(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()
	appCfg := &config.Config{}
	s := NewScheduler(store, nil, nil, appCfg, nil, logger.New(), Config{CheckInterval: time.Hour})
	server := &storage.Server{ID: uuid.New().String(), Name: "boot"}
	task := &storage.ScheduledTask{ID: uuid.New().String(), Name: "t"}
	cfg := ModpackUpdateTaskConfig{BootGateTimeoutSecs: 60}
	if err := s.verifyBootAfterUpdate(context.Background(), server, task, cfg, nil); err != nil {
		t.Fatalf("expected skip without snapshot, got %v", err)
	}
	cfgDisabled := ModpackUpdateTaskConfig{BootGateTimeoutSecs: 0}
	snap := &storage.ServerSnapshot{ID: "snap-1"}
	if err := s.verifyBootAfterUpdate(context.Background(), server, task, cfgDisabled, snap); err != nil {
		t.Fatalf("expected skip when disabled, got %v", err)
	}
}
