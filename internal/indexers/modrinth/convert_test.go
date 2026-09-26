package modrinth

import (
	"testing"
)

func TestConvertSearchProject(t *testing.T) {
	m := &ModrinthIndexer{}
	got := m.convertSearchProject(Project{
		ProjectID:     "abc123",
		Slug:          "all-the-mods-9",
		Title:         "All The Mods 9",
		Description:   "big pack",
		Categories:    []string{"forge", "adventure"},
		Versions:      []string{"1.20.1"},
		Downloads:     99,
		IconURL:       "https://cdn/icon.png",
		DateCreated:   "2024-01-02T00:00:00Z",
		LatestVersion: "v9",
	})

	if got.ID != "modrinth-abc123" || got.Indexer != "modrinth" || got.LatestFileID != "v9" {
		t.Errorf("ids = %+v", got)
	}
	if got.WebsiteURL != "https://modrinth.com/modpack/all-the-mods-9" {
		t.Errorf("url = %q", got.WebsiteURL)
	}
	found := false
	for _, l := range got.ModLoaders {
		if l == "forge" {
			found = true
		}
	}
	if !found {
		t.Errorf("loaders %v missing forge", got.ModLoaders)
	}
}

func TestConvertProject_LoaderSplit(t *testing.T) {
	m := &ModrinthIndexer{}
	got := m.convertProject(ProjectDetails{
		ID:          "abc123",
		Slug:        "all-the-mods-9",
		Title:       "All The Mods 9",
		Description: "d",
		Body:        "full body",
		Loaders:     []string{"Forge", "Fabric"},
		Categories:  []string{"forge", "adventure"},
		Versions:    []string{"v2", "v1"},
		Published:   "2024-01-02T00:00:00Z",
	})

	if got.Description != "full body" {
		t.Errorf("description = %q", got.Description)
	}
	if len(got.ModLoaders) != 2 || got.ModLoaders[0] != "forge" || got.ModLoaders[1] != "fabric" {
		t.Errorf("loaders = %v", got.ModLoaders)
	}
	// Loader categories are filtered out of the display list.
	for _, c := range got.Categories {
		if c == "forge" {
			t.Errorf("loader leaked into categories: %v", got.Categories)
		}
	}
	if got.LatestFileID != "v2" {
		t.Errorf("latest = %q, want v2", got.LatestFileID)
	}
}

func TestConvertVersionToFile(t *testing.T) {
	m := &ModrinthIndexer{}
	got := m.convertVersionToFile(Version{ID: "ver1"}, File{
		Filename: "mod.jar",
		URL:      "https://cdn/mod.jar",
		Primary:  true,
		Size:     10,
	}, "pack-1")
	if got.FileName != "mod.jar" || got.DownloadURL != "https://cdn/mod.jar" {
		t.Errorf("file = %+v", got)
	}
	if got.ModpackID != "pack-1" {
		t.Errorf("modpack id = %q", got.ModpackID)
	}
}

func TestModrinthGetIndexerName(t *testing.T) {
	m := &ModrinthIndexer{}
	if m.GetIndexerName() == "" {
		t.Error("empty indexer name")
	}
}
