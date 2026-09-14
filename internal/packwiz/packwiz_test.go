package packwiz

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/stretchr/testify/assert"
)

func TestPackwizManager_CreateUpdateExport(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "packwiz_test_*")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	log := logger.New()
	mgr := NewManager(tempDir, log)

	// Create Pack
	pack := &Pack{
		ID:            "test-pack",
		Name:          "Test Pack",
		Author:        "DiscoAdmin",
		Version:       "1.0.0",
		MCVersion:     "1.20.1",
		ModLoader:     "fabric",
		LoaderVersion: "0.15.11",
	}

	created, err := mgr.CreatePack(pack)
	assert.NoError(t, err)
	assert.Equal(t, "test-pack", created.ID)

	// Verify pack.toml and index.toml exist
	assert.FileExists(t, filepath.Join(tempDir, "test-pack", "pack.toml"))
	assert.FileExists(t, filepath.Join(tempDir, "test-pack", "index.toml"))

	// Add mod
	mod := ModItem{
		Slug:        "sodium",
		Name:        "Sodium",
		FileName:    "sodium-fabric-0.5.11+mc1.20.1.jar",
		Side:        "both",
		Platform:    "modrinth",
		ProjectID:   "AANobbMI",
		VersionID:   "12345",
		DownloadURL: "https://cdn.modrinth.com/data/AANobbMI/versions/12345/sodium-fabric-0.5.11+mc1.20.1.jar",
		Pinned:      false,
	}
	err = mgr.AddMod("test-pack", mod)
	assert.NoError(t, err)

	assert.FileExists(t, filepath.Join(tempDir, "test-pack", "mods", "sodium.pw.toml"))

	// Get pack and verify
	retrieved, err := mgr.GetPack("test-pack")
	assert.NoError(t, err)
	assert.Len(t, retrieved.Mods, 1)
	assert.Equal(t, "Sodium", retrieved.Mods[0].Name)
	assert.Equal(t, "both", retrieved.Mods[0].Side)

	// Update mod side
	err = mgr.UpdateMod("test-pack", "sodium", "client", true)
	assert.NoError(t, err)

	updatedPack, err := mgr.GetPack("test-pack")
	assert.NoError(t, err)
	assert.Equal(t, "client", updatedPack.Mods[0].Side)
	assert.True(t, updatedPack.Mods[0].Pinned)

	// Export .mrpack
	var mrpackBuf bytes.Buffer
	err = mgr.ExportMrpack("test-pack", &mrpackBuf)
	assert.NoError(t, err)
	assert.True(t, mrpackBuf.Len() > 0)

	// Validate zip contents
	zr, err := zip.NewReader(bytes.NewReader(mrpackBuf.Bytes()), int64(mrpackBuf.Len()))
	assert.NoError(t, err)
	var foundIndex bool
	for _, f := range zr.File {
		if f.Name == "modrinth.index.json" {
			foundIndex = true
		}
	}
	assert.True(t, foundIndex)

	// Export CurseForge .zip
	var cfBuf bytes.Buffer
	err = mgr.ExportCurseForge("test-pack", &cfBuf)
	assert.NoError(t, err)
	assert.True(t, cfBuf.Len() > 0)

	// Test serving pack.toml
	data, ctype, err := mgr.ServePackFile("test-pack", "pack.toml")
	assert.NoError(t, err)
	assert.Contains(t, ctype, "toml")
	assert.Contains(t, string(data), "Test Pack")
}
