// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package packwiz

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/stretchr/testify/require"
)

func newDatapackTestManager(t *testing.T) *Manager {
	t.Helper()
	mgr := NewManager(t.TempDir(), logger.New())
	_, err := mgr.CreatePack(&Pack{ID: "dp-pack", Name: "DP Pack", MCVersion: "1.21.1", ModLoader: "fabric"})
	require.NoError(t, err)
	return mgr
}

func zipArchive(t *testing.T, entries map[string]string) *bytes.Reader {
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
	return bytes.NewReader(buf.Bytes())
}

func TestAddDatapackFile(t *testing.T) {
	t.Run("installs a valid datapack", func(t *testing.T) {
		mgr := newDatapackTestManager(t)
		r := zipArchive(t, map[string]string{
			"pack.mcmeta":      `{"pack":{"pack_format":34}}`,
			"data/minecraft/x": "",
		})
		require.NoError(t, mgr.AddDatapackFile("dp-pack", "farming.zip", r))

		installed := filepath.Join(mgr.packDir("dp-pack"), "datapacks", "farming.zip")
		require.FileExists(t, installed)

		files, err := mgr.ListPackFiles("dp-pack")
		require.NoError(t, err)
		var found bool
		for _, f := range files {
			if f.Path == "datapacks/farming.zip" {
				found = true
				require.Equal(t, "datapack", f.Category)
			}
		}
		require.True(t, found, "installed datapack missing from pack files: %+v", files)
	})

	t.Run("repairs wrapper-folder archives", func(t *testing.T) {
		mgr := newDatapackTestManager(t)
		r := zipArchive(t, map[string]string{
			"my-pack/pack.mcmeta":      `{"pack":{"pack_format":34}}`,
			"my-pack/data/minecraft/x": "",
		})
		require.NoError(t, mgr.AddDatapackFile("dp-pack", "wrapped.zip", r))

		installed := filepath.Join(mgr.packDir("dp-pack"), "datapacks", "wrapped.zip")
		zr, err := zip.OpenReader(installed)
		require.NoError(t, err)
		defer func() { _ = zr.Close() }()
		names := map[string]bool{}
		for _, f := range zr.File {
			names[f.Name] = true
		}
		require.True(t, names["pack.mcmeta"], "pack.mcmeta not at root after repair: %v", names)
		require.False(t, names["my-pack/pack.mcmeta"])
	})

	t.Run("rejects non-datapack archives and leaves nothing behind", func(t *testing.T) {
		mgr := newDatapackTestManager(t)
		r := zipArchive(t, map[string]string{"readme.txt": "not a pack"})
		require.Error(t, mgr.AddDatapackFile("dp-pack", "junk.zip", r))

		dpDir := filepath.Join(mgr.packDir("dp-pack"), "datapacks")
		entries, err := os.ReadDir(dpDir)
		if err == nil {
			for _, e := range entries {
				require.Fail(t, "unexpected leftover file", e.Name())
			}
		}
	})

	t.Run("rejects unsafe names", func(t *testing.T) {
		mgr := newDatapackTestManager(t)
		r := zipArchive(t, map[string]string{"pack.mcmeta": `{}`})
		for _, name := range []string{"../evil.zip", "a/b.zip", "notzip.txt", ""} {
			require.Error(t, mgr.AddDatapackFile("dp-pack", name, r), "name %q", name)
		}
	})
}

func TestExportMrpackBundlesOverrides(t *testing.T) {
	mgr := newDatapackTestManager(t)

	require.NoError(t, mgr.SavePackFile("dp-pack", "config/settings.json", strings.NewReader(`{"a":1}`)))
	r := zipArchive(t, map[string]string{"pack.mcmeta": `{}`})
	require.NoError(t, mgr.AddDatapackFile("dp-pack", "farming.zip", r))

	var buf bytes.Buffer
	require.NoError(t, mgr.ExportMrpack("dp-pack", &buf))

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	require.True(t, names["modrinth.index.json"])
	require.True(t, names["overrides/config/settings.json"], "config override missing from mrpack: %v", names)
	require.True(t, names["overrides/datapacks/farming.zip"], "datapack override missing from mrpack: %v", names)
	require.False(t, names["overrides/pack.toml"], "pack.toml must not be bundled as an override")
	require.False(t, names["overrides/index.toml"], "index.toml must not be bundled as an override")
}
