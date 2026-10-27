// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package modrinth

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestDatapackZipFile(t *testing.T) {
	cases := []struct {
		name  string
		files []File
		want  string
	}{
		{"primary zip wins", []File{{Filename: "a.zip", Primary: true}, {Filename: "b.zip"}}, "a.zip"},
		{"jar primary falls back to zip", []File{{Filename: "mod.jar", Primary: true}, {Filename: "pack.zip"}}, "pack.zip"},
		{"uppercase zip accepted", []File{{Filename: "PACK.ZIP", Primary: true}}, "PACK.ZIP"},
		{"no zip yields nil", []File{{Filename: "mod.jar", Primary: true}}, ""},
		{"empty yields nil", nil, ""},
	}
	for _, tc := range cases {
		got := datapackZipFile(tc.files)
		if tc.want == "" {
			if got != nil {
				t.Errorf("%s: got %v, want nil", tc.name, got)
			}
			continue
		}
		if got == nil || got.Filename != tc.want {
			t.Errorf("%s: got %+v, want %s", tc.name, got, tc.want)
		}
	}
}

// TestGetDatapackLatestFileSkipsNonZipVersions mirrors the real Modrinth shape
// of dual-published projects (regression for QWfaJXEc): each release has a mod
// .jar version entry listed before its sibling _data_pack.zip entry. Only the
// ZIP entry is installable as a datapack.
func TestGetDatapackLatestFileSkipsNonZipVersions(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/project/abc123/version", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
		  {"id":"v3","version_number":"3","version_type":"release","game_versions":["1.21.11"],"date_published":"2024-03-01T00:00:00Z","files":[{"hashes":{"sha1":"j"},"url":"http://x/mod.jar","filename":"thing-1.21.11-fabric.jar","primary":true,"size":1}]},
		  {"id":"v2","version_number":"2","version_type":"release","game_versions":["1.21.11"],"date_published":"2024-02-01T00:00:00Z","files":[{"hashes":{"sha1":"z"},"url":"http://x/dp.zip","filename":"thing_1.21.11_data_pack.zip","primary":true,"size":2}]},
		  {"id":"v1","version_number":"1","version_type":"release","game_versions":["1.21.1"],"date_published":"2024-01-01T00:00:00Z","files":[{"hashes":{"sha1":"m"},"url":"http://x/old.jar","filename":"thing-1.21.1-fabric.jar","primary":true,"size":3},{"hashes":{"sha1":"o"},"url":"http://x/old-dp.zip","filename":"thing_1.21.1_data_pack.zip","primary":false,"size":4}]}
		]`))
	})
	testServer(t, mux)

	idx := NewDatapackIndexer(testCfg())

	// Newest matching version that ships a ZIP wins, even when a newer
	// jar-only version entry exists for the same game version.
	f, err := idx.GetDatapackLatestFile(context.Background(), "abc123", "1.21.11")
	if err != nil {
		t.Fatalf("GetDatapackLatestFile: %v", err)
	}
	if f.Filename != "thing_1.21.11_data_pack.zip" || f.DatapackID != "abc123" {
		t.Fatalf("file=%+v", f)
	}

	// Within one version entry, a non-primary ZIP is still selected when the
	// primary file is a mod jar.
	f, err = idx.GetDatapackLatestFile(context.Background(), "abc123", "1.21.1")
	if err != nil {
		t.Fatalf("GetDatapackLatestFile: %v", err)
	}
	if f.Filename != "thing_1.21.1_data_pack.zip" {
		t.Fatalf("file=%+v", f)
	}

	// No ZIP for the requested game version is an error, not a jar fallback.
	if _, err := idx.GetDatapackLatestFile(context.Background(), "abc123", "1.20.4"); err == nil {
		t.Fatal("want error when no zip matches game version")
	}
}

func TestSearchDatapacksUsesDatapackFacet(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		facets := r.URL.Query().Get("facets")
		if want := `project_type:datapack`; !strings.Contains(facets, want) {
			t.Errorf("facets=%s, want datapack project type", facets)
		}
		_, _ = w.Write([]byte(`{"hits":[{"slug":"dp","title":"DP","description":"d","project_id":"p1","downloads":5,"icon_url":"","versions":["1.21.1"],"latest_version":"v1","date_created":"2024-01-01T00:00:00Z","date_modified":"2024-02-01T00:00:00Z","author":"au"}],"offset":0,"limit":10,"total_hits":1}`))
	})
	testServer(t, mux)

	idx := NewDatapackIndexer(testCfg())
	sr, err := idx.SearchDatapacks(context.Background(), "farming", "1.21.1", 0, 10)
	if err != nil {
		t.Fatalf("SearchDatapacks: %v", err)
	}
	if len(sr.Datapacks) != 1 || sr.Datapacks[0].Name != "DP" || sr.Datapacks[0].Indexer != "modrinth" {
		t.Fatalf("result=%+v", sr)
	}
}
