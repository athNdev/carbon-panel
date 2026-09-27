package modrinth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
)

func testServer(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	old := BaseURL
	BaseURL = srv.URL
	t.Cleanup(func() { BaseURL = old })
}

func testCfg() *config.Config {
	return &config.Config{}
}

func TestSearchModpacks(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query(); q.Get("facets") == "" || q.Get("limit") == "" {
			t.Errorf("missing query params: %v", q)
		}
		w.Write([]byte(`{"hits":[{"slug":"pl-pack","title":"PL Pack","description":"d","categories":["fabric"],"project_id":"abc123","downloads":42,"icon_url":"http://x/i.png","versions":["1.20.1"],"latest_version":"v1","date_created":"2024-01-01T00:00:00Z","date_modified":"2024-02-01T00:00:00Z"}],"offset":0,"limit":10,"total_hits":1}`))
	})
	testServer(t, mux)

	resp, err := NewClient(testCfg()).SearchModpacks(context.Background(), "pl", "1.20.1", "fabric", 0, 10)
	if err != nil {
		t.Fatalf("SearchModpacks: %v", err)
	}
	if resp.TotalHits != 1 || len(resp.Hits) != 1 || resp.Hits[0].Title != "PL Pack" {
		t.Fatalf("resp=%+v", resp)
	}
}

func TestSearchModpacksError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("slow down"))
	})
	testServer(t, mux)

	if _, err := NewClient(testCfg()).SearchModpacks(context.Background(), "x", "", "", 0, 10); err == nil {
		t.Fatal("want error on 429")
	}
}

func TestGetModpackAndVersions(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/project/abc123", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"abc123","slug":"pl-pack","title":"PL Pack","description":"d","body":"full","project_type":"modpack","game_versions":["1.20.1"],"loaders":["fabric"],"categories":["adventure"],"versions":["v1"],"downloads":42,"published":"2024-01-01T00:00:00Z","updated":"2024-02-01T00:00:00Z"}`))
	})
	mux.HandleFunc("/project/abc123/version", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":"v1","name":"Pack 1.0","version_number":"1.0","version_type":"release","game_versions":["1.20.1"],"loaders":["fabric"],"date_published":"2024-02-01T00:00:00Z","files":[{"hashes":{"sha512":"a","sha1":"b"},"url":"http://x/pack.jar","filename":"pack.jar","primary":true,"size":99}]}]`))
	})
	testServer(t, mux)

	c := NewClient(testCfg())
	p, err := c.GetModpack(context.Background(), "abc123")
	if err != nil || p.Title != "PL Pack" {
		t.Fatalf("GetModpack=%+v err=%v", p, err)
	}
	vs, err := c.GetModpackVersions(context.Background(), "abc123")
	if err != nil || len(vs) != 1 || vs[0].Files[0].Filename != "pack.jar" {
		t.Fatalf("versions=%+v err=%v", vs, err)
	}
}

func TestAdapterEndToEnd(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"hits":[{"slug":"pl-pack","title":"PL Pack","description":"d","categories":["fabric"],"display_categories":["adventure"],"project_id":"abc123","downloads":7,"icon_url":"","versions":["1.20.1"],"latest_version":"v1","date_created":"2024-01-01T00:00:00Z","date_modified":"2024-02-01T00:00:00Z"}],"offset":0,"limit":10,"total_hits":1}`))
	})
	mux.HandleFunc("/project/abc123", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"abc123","slug":"pl-pack","title":"PL Pack","description":"d","body":"full","game_versions":["1.20.1"],"loaders":["Fabric"],"categories":["adventure"],"additional_categories":["extra"],"versions":["v1"],"downloads":7,"published":"2024-01-01T00:00:00Z","updated":"2024-02-01T00:00:00Z"}`))
	})
	mux.HandleFunc("/project/abc123/version", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":"v1","name":"Pack 1.0","version_number":"1.0","version_type":"beta","game_versions":["1.20.1"],"loaders":["Fabric"],"date_published":"2024-02-01T00:00:00Z","files":[{"hashes":{},"url":"http://x/pack.jar","filename":"pack.jar","primary":false,"size":10}]}]`))
	})
	testServer(t, mux)

	idx := NewIndexer(testCfg())
	if idx.GetIndexerName() != "modrinth" {
		t.Fatalf("name=%q", idx.GetIndexerName())
	}
	sr, err := idx.SearchModpacks(context.Background(), "pl", "", "", 0, 10)
	if err != nil || len(sr.Modpacks) != 1 {
		t.Fatalf("search=%+v err=%v", sr, err)
	}
	m := sr.Modpacks[0]
	if m.ID != "modrinth-abc123" || m.Indexer != "modrinth" || m.LatestFileID != "v1" {
		t.Fatalf("modpack=%+v", m)
	}
	if len(m.Categories) != 1 || m.Categories[0] != "adventure" {
		t.Fatalf("display categories=%v", m.Categories)
	}

	full, err := idx.GetModpack(context.Background(), "abc123")
	if err != nil || full.Description != "full" || full.LatestFileID != "v1" {
		t.Fatalf("get=%+v err=%v", full, err)
	}
	if len(full.ModLoaders) != 1 || full.ModLoaders[0] != "fabric" {
		t.Fatalf("loaders=%v", full.ModLoaders)
	}

	files, err := idx.GetModpackFiles(context.Background(), "abc123")
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%+v err=%v", files, err)
	}
	f := files[0]
	if f.FileName != "pack.jar" || f.ReleaseType != "beta" || f.ModLoader != "fabric" || f.SortIndex != 0 {
		t.Fatalf("file=%+v", f)
	}
}

func TestConvertVersionReleaseTypes(t *testing.T) {
	idx := NewIndexer(testCfg())
	for in, want := range map[string]string{"release": "release", "BETA": "beta", "Alpha": "alpha", "weird": "release"} {
		v := Version{ID: "v", VersionType: in, Files: []File{{Filename: "f.jar", Primary: true}}}
		if got := idx.convertVersionToFile(v, v.Files[0], "m").ReleaseType; got != want {
			t.Fatalf("type %q -> %q want %q", in, got, want)
		}
	}
	// Versions without files are skipped: covered in adapter via empty Files.
}
