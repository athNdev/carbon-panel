// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package fuego

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFuegoSearchDatapacks(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mods/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		require.Equal(t, "432", q.Get("gameId"))
		require.Equal(t, "6945", q.Get("classId"), "must search the Data Packs class")
		require.Equal(t, "1.21.1", q.Get("gameVersion"))
		require.Equal(t, "hearth", q.Get("searchFilter"))
		_, _ = w.Write([]byte(`{"data":[{"id":1563727,"name":"Wrong Yellow","slug":"wrong-yellow","summary":"s","links":{},"downloadCount":42,"authors":[{"name":"Starmute"}],"logo":{"thumbnailUrl":"http://x/i.png"},"latestFilesIndexes":[{"gameVersion":"1.21.1"},{"gameVersion":"1.21.4"},{"gameVersion":"1.21.1"}]}],"pagination":{"index":0,"pageSize":20,"resultCount":1,"totalCount":7}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := NewClient("", testCfg(), WithBaseURL(srv.URL))
	di := &FuegoDatapackIndexer{client: c}

	res, err := di.SearchDatapacks(context.Background(), "hearth", "1.21.1", 0, 20)
	require.NoError(t, err)
	require.Equal(t, 7, res.TotalCount)
	require.Len(t, res.Datapacks, 1)

	d := res.Datapacks[0]
	require.Equal(t, "1563727", d.ID)
	require.Equal(t, "curseforge", d.Indexer)
	require.Equal(t, "Wrong Yellow", d.Name)
	require.Equal(t, "Starmute", d.Author)
	require.Equal(t, int64(42), d.DownloadCount)
	require.Equal(t, "http://x/i.png", d.IconURL)
	require.Equal(t, "https://www.curseforge.com/minecraft/data-packs/wrong-yellow", d.WebsiteURL)
	require.Equal(t, []string{"1.21.1", "1.21.4"}, d.GameVersions, "versions deduplicated")
	require.Equal(t, "curseforge", di.GetIndexerName())
	require.NotEmpty(t, di.Origin())
}

func TestFuegoGetDatapackLatestFile(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mods/1563727/files", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
		  {"id":9222935,"modId":1563727,"isAvailable":true,"fileName":"side-notes.txt","fileLength":5,"downloadUrl":"http://x/notes.txt","gameVersions":["1.21.1"]},
		  {"id":9222934,"modId":1563727,"isAvailable":false,"fileName":"gone.zip","fileLength":5,"downloadUrl":"http://x/gone.zip","gameVersions":["1.21.1"]},
		  {"id":9222933,"modId":1563727,"isAvailable":true,"fileName":"pack-mod.jar","fileLength":7,"downloadUrl":"http://x/mod.jar","gameVersions":["1.21.1"]},
		  {"id":9222932,"modId":1563727,"isAvailable":true,"fileName":"pack-old.zip","fileLength":9,"downloadUrl":"http://x/old.zip","gameVersions":["1.20.4"]},
		  {"id":9222931,"modId":1563727,"isAvailable":true,"fileName":"pack.zip","fileLength":11,"downloadUrl":"http://x/pack.zip","gameVersions":["1.21.1","1.21.4"],"hashes":[{"value":"abc","algo":1},{"value":"def","algo":3}]}
		]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	di := &FuegoDatapackIndexer{client: NewClient("", testCfg(), WithBaseURL(srv.URL))}

	// Newest-first selection must skip non-ZIP and unavailable files.
	f, err := di.GetDatapackLatestFile(context.Background(), "1563727", "1.21.1")
	require.NoError(t, err)
	require.Equal(t, "pack.zip", f.Filename)
	require.Equal(t, "http://x/pack.zip", f.DownloadURL)
	require.Equal(t, int64(11), f.Size)
	require.Equal(t, "abc", f.SHA1)
	require.Equal(t, "def", f.SHA512)

	// Game-version filter picks the matching release.
	f, err = di.GetDatapackLatestFile(context.Background(), "1563727", "1.20.4")
	require.NoError(t, err)
	require.Equal(t, "pack-old.zip", f.Filename)

	// No ZIP for the requested version is an error, never a jar fallback.
	_, err = di.GetDatapackLatestFile(context.Background(), "1563727", "1.19.2")
	require.Error(t, err)

	// Invalid ids fail cleanly.
	_, err = di.GetDatapackLatestFile(context.Background(), "not-a-number", "")
	require.Error(t, err)
}
