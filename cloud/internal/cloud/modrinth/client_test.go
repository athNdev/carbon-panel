package modrinth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModrinthClient_Search(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/search", r.URL.Path)
		assert.Equal(t, "viaversion", r.URL.Query().Get("query"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"hits": [{
				"project_id": "P1OZGk5p",
				"project_type": "plugin",
				"slug": "viaversion",
				"title": "ViaVersion",
				"description": "Allow newer clients to connect",
				"categories": ["paper", "velocity"],
				"versions": ["1.21.4"],
				"downloads": 500000,
				"follows": 1200,
				"icon_url": "https://example.com/icon.png",
				"latest_version": "v123",
				"author": "kennytv"
			}],
			"total_hits": 1,
			"offset": 0,
			"limit": 20
		}`))
	}))
	defer srv.Close()

	client := NewClient(srv.Client())
	client.SetBaseURL(srv.URL)

	res, err := client.Search(context.Background(), &v1.SearchAddonsRequest{
		Query:        "viaversion",
		AddonType:    v1.AddonType_ADDON_TYPE_PLUGIN,
		Loader:       "paper",
		GameVersion:  "1.21.4",
		Limit:        20,
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), res.TotalHits)
	require.Len(t, res.Hits, 1)
	assert.Equal(t, "ViaVersion", res.Hits[0].Title)
	assert.Equal(t, "P1OZGk5p", res.Hits[0].ProjectId)
	assert.Equal(t, v1.AddonType_ADDON_TYPE_PLUGIN, res.Hits[0].AddonType)
}

func TestModrinthClient_GetProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/project/viaversion", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "P1OZGk5p",
			"slug": "viaversion",
			"project_type": "plugin",
			"title": "ViaVersion",
			"description": "Allow newer clients",
			"downloads": 1000
		}`))
	}))
	defer srv.Close()

	client := NewClient(srv.Client())
	client.SetBaseURL(srv.URL)

	proj, err := client.GetProject(context.Background(), "viaversion")
	require.NoError(t, err)
	assert.Equal(t, "P1OZGk5p", proj.ProjectId)
	assert.Equal(t, "ViaVersion", proj.Title)
	assert.Equal(t, v1.AddonType_ADDON_TYPE_PLUGIN, proj.AddonType)
}

func TestModrinthClient_GetVersionsAndResolve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/project/viaversion/version" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{
				"id": "ver_123",
				"version_number": "5.0.0",
				"name": "ViaVersion 5.0.0",
				"game_versions": ["1.21.4"],
				"loaders": ["paper"],
				"files": [{
					"url": "https://example.com/ViaVersion-5.0.0.jar",
					"filename": "ViaVersion-5.0.0.jar",
					"primary": true,
					"size": 1024
				}]
			}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewClient(srv.Client())
	client.SetBaseURL(srv.URL)

	versions, err := client.GetVersions(context.Background(), "viaversion", "paper", "1.21.4")
	require.NoError(t, err)
	require.Len(t, versions, 1)
	assert.Equal(t, "5.0.0", versions[0].VersionNumber)

	url, filename, err := client.ResolveDownloadURL(context.Background(), "viaversion", "", "paper", "1.21.4")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/ViaVersion-5.0.0.jar", url)
	assert.Equal(t, "ViaVersion-5.0.0.jar", filename)
}
