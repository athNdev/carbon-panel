package db

import (
	"context"
	"testing"
)

func TestIndexedModpack_CRUD(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	mp := &IndexedModpack{
		ID: "modrinth-atm9", IndexerID: "atm9", Indexer: "modrinth",
		Name: "All The Mods 9", Slug: "all-the-mods-9",
		WebsiteURL: "https://modrinth.com/modpack/all-the-mods-9",
	}
	if err := store.UpsertIndexedModpack(ctx, mp); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	got, err := store.GetIndexedModpack(ctx, "modrinth-atm9")
	if err != nil || got.Name != "All The Mods 9" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := store.GetIndexedModpack(ctx, "missing"); err == nil {
		t.Error("missing modpack fetched")
	}

	got, err = store.GetModpackBySlug(ctx, "all-the-mods-9")
	if err != nil || got.ID != "modrinth-atm9" {
		t.Fatalf("by slug = %+v, %v", got, err)
	}
	got, err = store.GetModpackBySlug(ctx, "missing")
	if err != nil || got != nil {
		t.Errorf("missing slug = %+v, %v; want nil, nil", got, err)
	}

	got, err = store.GetModpackByWebsiteURL(ctx, "https://modrinth.com/modpack/all-the-mods-9")
	if err != nil || got.ID != "modrinth-atm9" {
		t.Fatalf("by URL = %+v, %v", got, err)
	}

	// Upsert updates in place.
	mp.Name = "All The Mods 9 Revised"
	if err := store.UpsertIndexedModpack(ctx, mp); err != nil {
		t.Fatalf("re-upsert failed: %v", err)
	}
	got, err = store.GetIndexedModpack(ctx, "modrinth-atm9")
	if err != nil || got.Name != "All The Mods 9 Revised" {
		t.Fatalf("after re-upsert = %+v, %v", got, err)
	}

	// Files round-trip.
	file := &IndexedModpackFile{ID: "f1", ModpackID: "modrinth-atm9", DisplayName: "ATM9 1.0", FileName: "atm9.jar"}
	if err := store.UpsertIndexedModpackFile(ctx, file); err != nil {
		t.Fatalf("upsert file failed: %v", err)
	}
	files, err := store.GetIndexedModpackFiles(ctx, "modrinth-atm9")
	if err != nil || len(files) != 1 || files[0].FileName != "atm.jar" && files[0].FileName != "atm9.jar" {
		t.Fatalf("files = %+v, %v", files, err)
	}
	files, err = store.GetIndexedModpackFiles(ctx, "missing")
	if err != nil || len(files) != 0 {
		t.Fatalf("missing pack files = %v, %v", files, err)
	}

	// In-use check: no servers reference it.
	servers, err := store.CheckModpackInUse(ctx, "modrinth-atm9")
	if err != nil || len(servers) != 0 {
		t.Fatalf("in-use = %v, %v", servers, err)
	}

	if err := store.DeleteIndexedModpack(ctx, "modrinth-atm9"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := store.GetIndexedModpack(ctx, "modrinth-atm9"); err == nil {
		t.Error("deleted modpack fetched")
	}
}

func TestSearchIndexedModpacks(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	seed := []IndexedModpack{
		{ID: "m1", Indexer: "modrinth", Name: "Adventure Pack", GameVersions: `["1.20.1"]`, ModLoaders: `["forge"]`},
		{ID: "m2", Indexer: "modrinth", Name: "Tech Pack", GameVersions: `["1.20.1"]`, ModLoaders: `["fabric"]`},
		{ID: "f1", Indexer: "fuego", Name: "Adventure Plus", GameVersions: `["1.19.2"]`, ModLoaders: `["forge"]`},
	}
	for i := range seed {
		if err := store.UpsertIndexedModpack(ctx, &seed[i]); err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}

	results, total, err := store.SearchIndexedModpacks(ctx, "adventure", "", "", "", 0, 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if total != 2 || len(results) != 2 {
		t.Errorf("search adventure = %d/%d, want 2", len(results), total)
	}

	results, total, err = store.SearchIndexedModpacks(ctx, "", "1.20.1", "fabric", "", 0, 10)
	if err != nil {
		t.Fatalf("filtered search failed: %v", err)
	}
	if total != 1 || results[0].ID != "m2" {
		t.Errorf("filtered = %+v (%d)", results, total)
	}

	results, total, err = store.SearchIndexedModpacks(ctx, "", "", "", "fuego", 0, 10)
	if err != nil {
		t.Fatalf("indexer search failed: %v", err)
	}
	if total != 1 || len(results) != 1 || results[0].ID != "f1" {
		t.Errorf("fuego search = %+v (%d), want f1 (1)", results, total)
	}

	listed, total, err := store.ListIndexedModpacks(ctx, 0, 10)
	if err != nil || total != 3 || len(listed) != 3 {
		t.Errorf("list = %d/%d, %v", len(listed), total, err)
	}
	paged, _, err := store.ListIndexedModpacks(ctx, 1, 1)
	if err != nil || len(paged) != 1 {
		t.Errorf("paged list = %v, %v", paged, err)
	}
}
