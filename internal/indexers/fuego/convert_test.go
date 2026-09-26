package fuego

import (
	"testing"
	"time"
)

func intPtr(i int) *int { return &i }

func TestConvertModpack(t *testing.T) {
	fx := &FuegoIndexer{}
	got := fx.convertModpack(Modpack{
		ID:            123,
		Name:          "All The Mods",
		Slug:          "all-the-mods-9",
		Summary:       "big pack",
		DownloadCount: 42,
		MainFileID:    7,
		Categories:    []Category{{Name: "Adventure"}, {Name: "Tech"}},
		Logo:          Logo{ThumbnailURL: "https://cdn/logo.png"},
		Links:         Links{WebsiteURL: "https://site"},
		LatestFiles: []File{
			{GameVersions: []string{"1.20.1", "1.20.1"}},
		},
		LatestFilesIndexes: []FileIndex{
			{ModLoader: intPtr(1)},
			{ModLoader: intPtr(4)},
			{ModLoader: intPtr(99)},
		},
		DateCreated: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	})

	if got.ID != "fuego-123" || got.Indexer != "fuego" || got.IndexerID != "123" {
		t.Errorf("ids = %+v", got)
	}
	if got.Name != "All The Mods" || got.LatestFileID != "7" {
		t.Errorf("fields = %+v", got)
	}
	if len(got.Categories) != 2 || got.LogoURL != "https://cdn/logo.png" {
		t.Errorf("cats/logo = %+v", got)
	}
	if len(got.GameVersions) != 1 || got.GameVersions[0] != "1.20.1" {
		t.Errorf("versions not deduped = %v", got.GameVersions)
	}
	for _, want := range []string{"forge", "fabric"} {
		found := false
		for _, l := range got.ModLoaders {
			if l == want {
				found = true
			}
		}
		if !found {
			t.Errorf("loaders %v missing %q", got.ModLoaders, want)
		}
	}
}

func TestConvertFile_ReleaseTypes(t *testing.T) {
	fx := &FuegoIndexer{}
	for _, tc := range []struct {
		release int
		want    string
	}{
		{1, "release"},
		{2, "beta"},
		{3, "alpha"},
		{9, "release"},
	} {
		got := fx.convertFile(File{
			ID: 5, DisplayName: "f", FileName: "f.jar",
			ReleaseType: tc.release, GameVersions: []string{"1.20.1"},
		}, "pack-1")
		if got.ReleaseType != tc.want {
			t.Errorf("release %d = %q, want %q", tc.release, got.ReleaseType, tc.want)
		}
	}
}

func TestGetIndexerName(t *testing.T) {
	fx := &FuegoIndexer{}
	if fx.GetIndexerName() == "" {
		t.Error("empty indexer name")
	}
}
