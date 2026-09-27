package fuego

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
)

func testCfg() *config.Config {
	return &config.Config{}
}

func TestNewClientVariants(t *testing.T) {
	if c := NewClient("", testCfg()); c.baseURL != KeylessBaseURL {
		t.Fatalf("keyless base=%q", c.baseURL)
	}
	if c := NewClient("secret", testCfg()); c.baseURL != OfficialBaseURL {
		t.Fatalf("keyed base=%q", c.baseURL)
	}
	if c := NewClient("", testCfg(), WithBaseURL("http://mirror/")); c.baseURL != "http://mirror/" {
		t.Fatalf("option base=%q", c.baseURL)
	}
}

func TestSearchModpacks(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"data":[{"id":123,"name":"ATM9","slug":"atm9","summary":"pack","downloadCount":100,"mainFileId":456}],"pagination":{"index":0,"pageSize":10,"resultCount":1,"totalCount":1}}`))
	}))
	defer srv.Close()

	c := NewClient("", testCfg(), WithBaseURL(srv.URL))
	resp, err := c.SearchModpacks(context.Background(), "atm", "1.20.1", ModLoaderNeoForge, 0, 10)
	if err != nil {
		t.Fatalf("SearchModpacks: %v", err)
	}
	if resp.Pagination.TotalCount != 1 || len(resp.Data) != 1 || resp.Data[0].Name != "ATM9" {
		t.Fatalf("resp=%+v", resp)
	}
	for _, want := range []string{"gameId=432", "classId=4471", "searchFilter=atm", "gameVersion=1.20.1", "modLoaderType=6"} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("query %q missing %q", gotQuery, want)
		}
	}
}

func TestGetModpack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mods/123" {
			t.Errorf("path=%q", r.URL.Path)
		}
		w.Write([]byte(`{"data":{"id":123,"name":"ATM9","slug":"atm9"}}`))
	}))
	defer srv.Close()

	m, err := NewClient("", testCfg(), WithBaseURL(srv.URL)).GetModpack(context.Background(), 123)
	if err != nil || m.Name != "ATM9" {
		t.Fatalf("modpack=%+v err=%v", m, err)
	}
}

func TestGetModpackFilesCDNFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// One file with a URL, one without (gets edge-CDN synthesis).
		w.Write([]byte(`{"data":[{"id":2000,"fileName":"server.zip","downloadUrl":"http://cdn/x.zip"},{"id":3001500,"fileName":"client.jar","downloadUrl":""}]}`))
	}))
	defer srv.Close()

	files, err := NewClient("", testCfg(), WithBaseURL(srv.URL)).GetModpackFiles(context.Background(), 123)
	if err != nil || len(files) != 2 {
		t.Fatalf("files=%+v err=%v", files, err)
	}
	if files[0].DownloadURL != "http://cdn/x.zip" {
		t.Fatalf("file0=%+v", files[0])
	}
	// 3001500/1000=3001, %1000=500.
	if want := "https://edge.forgecdn.net/files/3001/500/client.jar"; files[1].DownloadURL != want {
		t.Fatalf("file1 url=%q want %q", files[1].DownloadURL, want)
	}
}

func TestIndexerName(t *testing.T) {
	if got := NewIndexer("", testCfg()).GetIndexerName(); got != "fuego" {
		t.Fatalf("name=%q", got)
	}
}

func intPtr(i int) *int { return &i }

func TestConvertModpack(t *testing.T) {
	f := NewIndexer("", testCfg())
	m := f.convertModpack(Modpack{
		ID: 123, Name: "ATM9", Slug: "atm9", Summary: "pack",
		DownloadCount: 100, MainFileID: 456,
		Links:       Links{WebsiteURL: "http://x"},
		Logo:        Logo{ThumbnailURL: "http://x/logo.png"},
		Categories:  []Category{{Name: "Adventure"}},
		LatestFiles: []File{{GameVersions: []string{"1.20.1"}}},
		LatestFilesIndexes: []FileIndex{
			{ModLoader: intPtr(1)},
			{ModLoader: intPtr(4)},
			{ModLoader: intPtr(4)}, // dup deduped
			{ModLoader: nil},       // skipped
		},
	})
	if m.ID != "fuego-123" || m.IndexerID != "123" || m.Indexer != "fuego" {
		t.Fatalf("ids=%+v", m)
	}
	if m.LatestFileID != "456" || m.LogoURL != "http://x/logo.png" {
		t.Fatalf("pack=%+v", m)
	}
	if len(m.GameVersions) != 1 || m.GameVersions[0] != "1.20.1" {
		t.Fatalf("versions=%v", m.GameVersions)
	}
	if len(m.ModLoaders) != 2 || m.ModLoaders[0] != "forge" || m.ModLoaders[1] != "fabric" {
		t.Fatalf("loaders=%v", m.ModLoaders)
	}
}

func TestConvertFile(t *testing.T) {
	f := NewIndexer("", testCfg())
	for in, want := range map[int]string{1: "release", 2: "beta", 3: "alpha", 9: "release"} {
		got := f.convertFile(File{ID: 7, ReleaseType: in, ServerPackFileID: intPtr(8)}, "123")
		if got.ReleaseType != want || got.ID != "7" || got.ModpackID != "123" {
			t.Fatalf("type %d -> %+v", in, got)
		}
		if got.ServerPackFileID == nil || *got.ServerPackFileID != "8" {
			t.Fatalf("serverpack=%v", got.ServerPackFileID)
		}
	}
	// Nil server pack stays non-nil pointer to "" (existing semantics).
	if got := f.convertFile(File{}, "123"); got.ServerPackFileID == nil {
		t.Fatal("ServerPackFileID should be non-nil")
	}
}

func TestAdapterInvalidIDs(t *testing.T) {
	f := NewIndexer("", testCfg())
	if _, err := f.GetModpack(context.Background(), "not-a-number"); err == nil {
		t.Fatal("want error for non-numeric modpack ID")
	}
	if _, err := f.GetModpackFiles(context.Background(), "not-a-number"); err == nil {
		t.Fatal("want error for non-numeric modpack ID")
	}
}

func TestAdapterSearchLoaderMapping(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"data":[{"id":1,"name":"P","slug":"p"}],"pagination":{"totalCount":1}}`))
	}))
	defer srv.Close()

	f := NewIndexer("", testCfg())
	f.client.baseURL = srv.URL
	sr, err := f.SearchModpacks(context.Background(), "", "", "fabric", 20, 10)
	if err != nil || len(sr.Modpacks) != 1 || sr.Modpacks[0].ID != "fuego-1" {
		t.Fatalf("search=%+v err=%v", sr, err)
	}
	// offset 20 / limit 10 -> page index 2; fabric -> type 4.
	for _, want := range []string{"index=2", "pageSize=10", "modLoaderType=4"} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("query %q missing %q", gotQuery, want)
		}
	}
	if sr.Offset != 20 || sr.PageSize != 10 || sr.TotalCount != 1 {
		t.Fatalf("result=%+v", sr)
	}
}

func TestAdapterGetFilesSortIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":1,"fileName":"a.jar","downloadUrl":"http://x/a"},{"id":2,"fileName":"b.jar","downloadUrl":"http://x/b"}]}`))
	}))
	defer srv.Close()

	f := NewIndexer("", testCfg())
	f.client.baseURL = srv.URL
	files, err := f.GetModpackFiles(context.Background(), "99")
	if err != nil || len(files) != 2 {
		t.Fatalf("files=%+v err=%v", files, err)
	}
	if files[0].SortIndex != 0 || files[1].SortIndex != 1 || files[0].ModpackID != "99" {
		t.Fatalf("files=%+v", files)
	}
}
