package packwiz

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackwiz_ImportMrpackAndExport(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "mrpack_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	log := logger.New()
	mgr := NewManager(tempDir, log)

	// Create sample .mrpack zip in memory
	var mrpackBuf bytes.Buffer
	zw := zip.NewWriter(&mrpackBuf)

	indexData := map[string]any{
		"formatVersion": 1,
		"game":          "minecraft",
		"versionId":     "2.0.0",
		"name":          "Speedy Pack",
		"summary":       "A fast fabric modpack",
		"dependencies": map[string]string{
			"minecraft":     "1.20.1",
			"fabric-loader": "0.15.11",
		},
		"files": []map[string]any{
			{
				"path": "mods/sodium.jar",
				"hashes": map[string]string{
					"sha256": "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
				},
				"env": map[string]string{
					"client": "required",
					"server": "unsupported",
				},
				"downloads": []string{"https://cdn.modrinth.com/sodium.jar"},
				"fileSize":  123456,
			},
		},
	}
	idxBytes, err := json.Marshal(indexData)
	require.NoError(t, err)

	zf, err := zw.Create("modrinth.index.json")
	require.NoError(t, err)
	_, _ = zf.Write(idxBytes)

	// Add an override file: overrides/config/options.txt
	cfgFile, err := zw.Create("overrides/config/options.txt")
	require.NoError(t, err)
	_, _ = cfgFile.Write([]byte("gamma:1.0\ngraphics:fast"))

	require.NoError(t, zw.Close())

	// Test Import
	pack, err := mgr.ImportMrpack(bytes.NewReader(mrpackBuf.Bytes()), int64(mrpackBuf.Len()), ImportOptions{
		Name: "My Imported Speedy Pack",
	})
	require.NoError(t, err)
	assert.Equal(t, "My Imported Speedy Pack", pack.Name)
	assert.Equal(t, "1.20.1", pack.MCVersion)
	assert.Equal(t, "fabric", pack.ModLoader)
	assert.Len(t, pack.Mods, 1)
	assert.Equal(t, "client", pack.Mods[0].Side)

	// Verify override extracted to packDir/config/options.txt
	optionsPath := filepath.Join(tempDir, pack.ID, "config", "options.txt")
	assert.FileExists(t, optionsPath)
	data, _ := os.ReadFile(optionsPath)
	assert.Contains(t, string(data), "gamma:1.0")

	// Test Export Native Packwiz ZIP
	var packwizZipBuf bytes.Buffer
	err = mgr.ExportPackwizZip(pack.ID, &packwizZipBuf)
	require.NoError(t, err)
	assert.True(t, packwizZipBuf.Len() > 0)

	zr, err := zip.NewReader(bytes.NewReader(packwizZipBuf.Bytes()), int64(packwizZipBuf.Len()))
	require.NoError(t, err)
	var foundPackToml, foundIndexToml, foundOptions bool
	for _, f := range zr.File {
		if f.Name == "pack.toml" {
			foundPackToml = true
		} else if f.Name == "index.toml" {
			foundIndexToml = true
		} else if f.Name == "config/options.txt" {
			foundOptions = true
		}
	}
	assert.True(t, foundPackToml)
	assert.True(t, foundIndexToml)
	assert.True(t, foundOptions)
}

func TestPackwiz_ImportCurseForge(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cf_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	log := logger.New()
	mgr := NewManager(tempDir, log)

	var cfBuf bytes.Buffer
	zw := zip.NewWriter(&cfBuf)

	manifestData := map[string]any{
		"minecraft": map[string]any{
			"version": "1.20.1",
			"modLoaders": []map[string]any{
				{"id": "forge-47.2.0", "primary": true},
			},
		},
		"manifestType":    "minecraftModpack",
		"manifestVersion": 1,
		"name":            "Forge Heroes",
		"version":         "1.0.0",
		"author":          "CurseAuthor",
		"files": []map[string]any{
			{"projectID": 12345, "fileID": 67890, "required": true},
			{"projectID": 54321, "fileID": 98765, "required": false},
		},
		"overrides": "overrides",
	}
	mBytes, err := json.Marshal(manifestData)
	require.NoError(t, err)

	zf, err := zw.Create("manifest.json")
	require.NoError(t, err)
	_, _ = zf.Write(mBytes)

	require.NoError(t, zw.Close())

	pack, err := mgr.ImportCurseForge(bytes.NewReader(cfBuf.Bytes()), int64(cfBuf.Len()), ImportOptions{})
	require.NoError(t, err)
	assert.Equal(t, "Forge Heroes", pack.Name)
	assert.Equal(t, "1.20.1", pack.MCVersion)
	assert.Equal(t, "forge", pack.ModLoader)
	assert.Equal(t, "47.2.0", pack.LoaderVersion)
	assert.Len(t, pack.Mods, 2)
	assert.Equal(t, "both", pack.Mods[0].Side)
	assert.Equal(t, "client", pack.Mods[1].Side)
}

func TestPackwiz_DirectURLModAndRefresh(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "url_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/java-archive")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("mock jar file content"))
	}))
	defer mockServer.Close()

	log := logger.New()
	mgr := NewManager(tempDir, log)

	pack := &Pack{
		ID:        "url-pack",
		Name:      "URL Pack",
		MCVersion: "1.20.1",
		ModLoader: "fabric",
	}
	_, err = mgr.CreatePack(pack)
	require.NoError(t, err)

	// Add mod from URL
	mod, err := mgr.AddModFromURL("url-pack", DirectURLModRequest{
		Name:        "Custom Utility",
		FileName:    "custom-util-1.0.jar",
		DownloadURL: mockServer.URL + "/custom-util-1.0.jar",
		Side:        "both",
		Pinned:      true,
	})
	require.NoError(t, err)
	assert.Equal(t, "custom-util-1.0.jar", mod.FileName)
	assert.Equal(t, "url", mod.Platform)
	assert.True(t, mod.Pinned)

	// Verify RefreshPack recalculates without errors
	err = mgr.RefreshPack("url-pack")
	require.NoError(t, err)

	idxData, _, err := mgr.ServePackFile("url-pack", "index.toml")
	require.NoError(t, err)
	assert.Contains(t, string(idxData), "custom-utility.pw.toml")
}

func TestPackwiz_FilesManagementAndClone(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "files_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	log := logger.New()
	mgr := NewManager(tempDir, log)

	pack := &Pack{
		ID:        "files-pack",
		Name:      "Files Pack",
		MCVersion: "1.20.1",
		ModLoader: "fabric",
	}
	_, err = mgr.CreatePack(pack)
	require.NoError(t, err)

	// Save override file
	err = mgr.SavePackFile("files-pack", "config/mysettings.json", strings.NewReader(`{"enabled":true}`))
	require.NoError(t, err)

	// List files
	files, err := mgr.ListPackFiles("files-pack")
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "config/mysettings.json", files[0].Path)
	assert.Equal(t, "config", files[0].Category)

	// Clone Pack
	cloned, err := mgr.ClonePack("files-pack", "Cloned Pack")
	require.NoError(t, err)
	assert.Equal(t, "Cloned Pack", cloned.Name)
	assert.NotEqual(t, "files-pack", cloned.ID)

	// Verify cloned has the config file
	clonedFiles, err := mgr.ListPackFiles(cloned.ID)
	require.NoError(t, err)
	require.Len(t, clonedFiles, 1)
	assert.Equal(t, "config/mysettings.json", clonedFiles[0].Path)

	// Delete file from original
	err = mgr.DeletePackFile("files-pack", "config/mysettings.json")
	require.NoError(t, err)

	origFiles, err := mgr.ListPackFiles("files-pack")
	require.NoError(t, err)
	assert.Empty(t, origFiles)
}
