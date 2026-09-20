package blueprint

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
)

func testStore(t *testing.T) *storage.Store {
	t.Helper()
	cfg := &config.Config{
		Database: config.DatabaseConfig{Path: ":memory:", AutoMigrate: true, MaxConnections: 1},
	}
	store, err := storage.NewSQLiteStore(cfg)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestSeedCountAndEnv(t *testing.T) {
	seeds := Seed()
	if len(seeds) != 6 {
		t.Fatalf("expected 6 seeds, got %d", len(seeds))
	}
	for _, s := range seeds {
		var env map[string]string
		if err := json.Unmarshal([]byte(s.DefaultEnv), &env); err != nil {
			t.Fatalf("seed %s has invalid DefaultEnv: %v", s.ID, err)
		}
		if env["EULA"] != "TRUE" {
			t.Fatalf("seed %s missing EULA=TRUE", s.ID)
		}
	}
}

func TestInitIdempotent(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	if err := InitBuiltinBlueprints(store); err != nil {
		t.Fatal(err)
	}
	if err := InitBuiltinBlueprints(store); err != nil {
		t.Fatalf("second init: %v", err)
	}
	list, err := store.ListBlueprints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 6 {
		t.Fatalf("expected 6 blueprints, got %d", len(list))
	}
	bp, err := store.GetBlueprint(ctx, "builtin-paper")
	if err != nil {
		t.Fatal(err)
	}
	if string(bp.ModLoader) != "paper" {
		t.Fatalf("wrong loader: %s", bp.ModLoader)
	}
}
