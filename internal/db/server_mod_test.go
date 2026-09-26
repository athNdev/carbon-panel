package db

import (
	"context"
	"testing"
)

func TestServer_ListUpdateGetByPort(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	mk := func(id, name string, port int) {
		if err := store.DB().Create(&Server{ID: id, Name: name, DataPath: t.TempDir(), Port: port}).Error; err != nil {
			t.Fatalf("insert %s failed: %v", id, err)
		}
	}
	mk("srv-1", "one", 25565)
	mk("srv-2", "two", 25566)

	listed, err := store.ListServers(ctx)
	if err != nil || len(listed) != 2 {
		t.Fatalf("list = %d, %v", len(listed), err)
	}

	got, err := store.GetServerByPort(ctx, 25566)
	if err != nil || got.ID != "srv-2" {
		t.Fatalf("by port = %+v, %v", got, err)
	}
	got, err = store.GetServerByPort(ctx, 29999)
	if err != nil || got != nil {
		t.Errorf("free port = %+v, %v; want nil, nil", got, err)
	}

	srv, err := store.GetServer(ctx, "srv-1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	srv.Name = "renamed"
	if err := store.UpdateServer(ctx, srv); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	got, err = store.GetServer(ctx, "srv-1")
	if err != nil || got.Name != "renamed" {
		t.Fatalf("after update = %+v, %v", got, err)
	}
}

func TestServerConfig_MemoryAndEphemeral(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	if err := store.DB().Create(&Server{ID: "srv-1", Name: "s", DataPath: t.TempDir()}).Error; err != nil {
		t.Fatalf("insert failed: %v", err)
	}
	if _, err := store.GetServerConfig(ctx, "srv-1"); err != nil {
		if err := store.DB().Create(&ServerConfig{ID: "srv-1", ServerID: "srv-1"}).Error; err != nil {
			t.Fatalf("insert config failed: %v", err)
		}
	}

	if err := store.UpdateServerConfigMemory(ctx, "srv-1", 2048); err != nil {
		t.Fatalf("memory update failed: %v", err)
	}
	cfg, err := store.GetServerConfig(ctx, "srv-1")
	if err != nil || cfg.MaxMemory == nil || *cfg.MaxMemory == "" {
		t.Fatalf("config = %+v, %v", cfg, err)
	}
	if err := store.UpdateServerConfigMemory(ctx, "missing", 2048); err == nil {
		t.Error("memory update on missing server succeeded")
	}

	force := true
	cfg.CFForceReinstallModloader = &force
	if err := store.UpdateServerConfig(ctx, cfg); err != nil {
		t.Fatalf("config update failed: %v", err)
	}
	if err := store.ClearEphemeralConfigFields(ctx, "srv-1"); err != nil {
		t.Fatalf("clear ephemeral failed: %v", err)
	}
	cfg, err = store.GetServerConfig(ctx, "srv-1")
	if err != nil || cfg.CFForceReinstallModloader != nil {
		t.Fatalf("ephemeral not cleared: %+v, %v", cfg, err)
	}
	if err := store.ClearEphemeralConfigFields(ctx, "missing"); err == nil {
		t.Error("clear on missing server succeeded")
	}
}

func TestMod_CRUD(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	mod := &Mod{ID: "mod-1", ServerID: "srv-1", Name: "Sodium", FileName: "sodium.jar", Version: "1.0", Enabled: true}
	if err := store.AddMod(ctx, mod); err != nil {
		t.Fatalf("add failed: %v", err)
	}
	got, err := store.GetMod(ctx, "mod-1")
	if err != nil || got.FileName != "sodium.jar" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := store.GetMod(ctx, "missing"); err == nil {
		t.Error("missing mod fetched")
	}

	mods, err := store.ListServerMods(ctx, "srv-1")
	if err != nil || len(mods) != 1 {
		t.Fatalf("list = %v, %v", mods, err)
	}
	mods, err = store.ListServerMods(ctx, "srv-other")
	if err != nil || len(mods) != 0 {
		t.Fatalf("other server list = %v, %v", mods, err)
	}

	mod.Enabled = false
	if err := store.UpdateMod(ctx, mod); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	got, err = store.GetMod(ctx, "mod-1")
	if err != nil || got.Enabled {
		t.Fatalf("after update = %+v, %v", got, err)
	}

	if err := store.DeleteMod(ctx, "mod-1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := store.GetMod(ctx, "mod-1"); err == nil {
		t.Error("deleted mod fetched")
	}
}
