package db

import (
	"context"
	"testing"
)

// TestStore_CreateDefaultServerConfig_PauseWhenEmpty verifies the MINE-119
// sleep baseline: new servers default to pause-when-empty=60 (native tick
// freeze, ~0 CPU, instant wake) unless global settings override it.
func TestStore_CreateDefaultServerConfig_PauseWhenEmpty(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	cfg := store.CreateDefaultServerConfig("srv-sleep-base")
	if cfg.PauseWhenEmptySeconds == nil {
		t.Fatal("expected PauseWhenEmptySeconds default to be set, got nil")
	}
	if *cfg.PauseWhenEmptySeconds != 60 {
		t.Fatalf("expected PauseWhenEmptySeconds default 60, got %d", *cfg.PauseWhenEmptySeconds)
	}
}

// TestStore_CreateDefaultServerConfig_PauseWhenEmptyGlobalOverride verifies a
// global PAUSE_WHEN_EMPTY_SECONDS still wins over the 60s baseline default.
func TestStore_CreateDefaultServerConfig_PauseWhenEmptyGlobalOverride(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	global := store.CreateDefaultServerConfig(GlobalSettingsID)
	override := 120
	global.PauseWhenEmptySeconds = &override
	if err := store.UpdateGlobalSettings(ctx, global); err != nil {
		t.Fatalf("failed to save global settings: %v", err)
	}

	cfg := store.CreateDefaultServerConfig("srv-sleep-override")
	if cfg.PauseWhenEmptySeconds == nil {
		t.Fatal("expected PauseWhenEmptySeconds to be set, got nil")
	}
	if *cfg.PauseWhenEmptySeconds != 120 {
		t.Fatalf("expected global override 120 to win, got %d", *cfg.PauseWhenEmptySeconds)
	}
}
