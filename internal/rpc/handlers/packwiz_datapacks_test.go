// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package handlers

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/indexers/modrinth"
	"github.com/athNdev/carbon-panel/internal/packwiz"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/athNdev/carbon-panel/pkg/upload"
	"github.com/stretchr/testify/require"
)

func testZipBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// newDatapackHarness starts a fake Modrinth API whose project version points at
// the given artifact server, plus a pack with the studio handler wired up.
func newDatapackHarness(t *testing.T, artifactBody []byte, filename string) (*PackwizHandler, string) {
	t.Helper()
	t.Setenv("DISCO_ALLOW_LOOPBACK_TEST", "1")

	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(artifactBody)
	}))
	t.Cleanup(artifact.Close)

	mux := http.NewServeMux()
	mux.HandleFunc("/project/abc/version", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `[{"id":"v1","version_number":"1","version_type":"release","game_versions":["1.21.1"],"date_published":"2024-01-01T00:00:00Z","files":[{"hashes":{"sha1":"x"},"url":%q,"filename":%q,"primary":true,"size":%d}]}]`,
			artifact.URL+"/"+filename, filename, len(artifactBody))
	})
	api := httptest.NewServer(mux)
	t.Cleanup(api.Close)
	old := modrinth.BaseURL
	modrinth.BaseURL = api.URL
	t.Cleanup(func() { modrinth.BaseURL = old })

	mgr := packwiz.NewManager(t.TempDir(), logger.New())
	_, err := mgr.CreatePack(&packwiz.Pack{ID: "p1", Name: "P", MCVersion: "1.21.1", ModLoader: "fabric"})
	require.NoError(t, err)

	um := upload.NewManager(t.TempDir(), time.Minute, 10<<20, logger.New())
	t.Cleanup(um.Stop)
	return NewPackwizHandler(mgr, nil, um, logger.New(), nil, nil, &config.Config{}), "p1"
}

func TestHandleAddDatapackInstallsArchive(t *testing.T) {
	body := testZipBytes(t, map[string]string{
		"pack.mcmeta":      `{"pack":{"pack_format":34}}`,
		"data/minecraft/x": "",
	})
	h, packID := newDatapackHarness(t, body, "farming.zip")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/packwiz/packs/p1/datapacks",
		strings.NewReader(`{"datapack_id":"abc","game_version":"1.21.1","indexer":"modrinth"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp struct {
		Success  bool   `json:"success"`
		FileName string `json:"file_name"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	require.Equal(t, "farming.zip", resp.FileName)

	raw, _, err := h.manager.ServePackFile(packID, "datapacks/farming.zip")
	require.NoError(t, err)
	require.Equal(t, body, raw)
}

func TestHandleAddDatapackRepairsWrappedArchive(t *testing.T) {
	body := testZipBytes(t, map[string]string{
		"my-pack/pack.mcmeta":      `{"pack":{"pack_format":34}}`,
		"my-pack/data/minecraft/x": "",
	})
	h, packID := newDatapackHarness(t, body, "wrapped.zip")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/packwiz/packs/p1/datapacks",
		strings.NewReader(`{"datapack_id":"abc","game_version":"1.21.1","indexer":"modrinth"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	raw, _, err := h.manager.ServePackFile(packID, "datapacks/wrapped.zip")
	require.NoError(t, err)
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	require.True(t, names["pack.mcmeta"], "pack.mcmeta not at root: %v", names)
}

func TestHandleAddDatapackRejectsNonDatapack(t *testing.T) {
	body := testZipBytes(t, map[string]string{"readme.txt": "not a pack"})
	h, packID := newDatapackHarness(t, body, "junk.zip")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/packwiz/packs/p1/datapacks",
		strings.NewReader(`{"datapack_id":"abc","game_version":"1.21.1","indexer":"modrinth"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	_, _, err := h.manager.ServePackFile(packID, "datapacks/junk.zip")
	require.Error(t, err, "invalid archive must not be installed")
}

func TestHandleAddDatapackValidatesInput(t *testing.T) {
	h, _ := newDatapackHarness(t, testZipBytes(t, map[string]string{"pack.mcmeta": `{}`}), "x.zip")

	for _, body := range []string{`{}`, `not json`, `{"datapack_id":"   "}`} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/packwiz/packs/p1/datapacks", strings.NewReader(body))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code, "body %q", body)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/packwiz/packs/p1/datapacks",
		strings.NewReader(`{"datapack_id":"abc","file_name":"../evil.zip"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code, "traversal name must be rejected")
}

func uploadSessionBytes(t *testing.T, h *PackwizHandler, filename string, data []byte) string {
	t.Helper()
	sess, err := h.uploadManager.InitSession(filename, int64(len(data)), int32(len(data)))
	require.NoError(t, err)
	_, done, err := h.uploadManager.WriteStream(sess.ID, bytes.NewReader(data), 0)
	require.NoError(t, err)
	require.True(t, done)
	return sess.ID
}

func TestHandleUploadDatapackInstallsArchive(t *testing.T) {
	h, packID := newDatapackHarness(t, testZipBytes(t, map[string]string{"pack.mcmeta": `{}`}), "unused.zip")
	body := testZipBytes(t, map[string]string{
		"pack.mcmeta":      `{"pack":{"pack_format":48}}`,
		"data/minecraft/x": "",
	})
	session := uploadSessionBytes(t, h, "up-pack.zip", body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/packwiz/packs/p1/datapacks/upload",
		strings.NewReader(`{"upload_session_id":"`+session+`"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	raw, _, err := h.manager.ServePackFile(packID, "datapacks/up-pack.zip")
	require.NoError(t, err)
	require.Equal(t, body, raw)
}

func TestHandleUploadDatapackRejectsNonDatapack(t *testing.T) {
	h, packID := newDatapackHarness(t, testZipBytes(t, map[string]string{"pack.mcmeta": `{}`}), "unused.zip")
	body := testZipBytes(t, map[string]string{"readme.txt": "not a pack"})
	session := uploadSessionBytes(t, h, "junk.zip", body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/packwiz/packs/p1/datapacks/upload",
		strings.NewReader(`{"upload_session_id":"`+session+`"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	_, _, err := h.manager.ServePackFile(packID, "datapacks/junk.zip")
	require.Error(t, err, "invalid archive must not be installed")

	_, _, getErr := h.uploadManager.GetTempPath(session)
	require.Error(t, getErr, "failed upload must clean up its session")
}
