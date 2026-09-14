package packwiz

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPackwizE2E_MultiLoaderMatrix tests pack creation across Fabric, NeoForge, and Forge on MC 1.20.1 and 1.21 (MINE-34).
func TestPackwizE2E_MultiLoaderMatrix(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "packwiz_matrix_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	log := logger.New()
	mgr := NewManager(tempDir, log)

	testCases := []struct {
		name          string
		loader        string
		loaderVersion string
		mcVersion     string
	}{
		{"fabric-1.20.1", "fabric", "0.15.11", "1.20.1"},
		{"fabric-1.21", "fabric", "0.16.0", "1.21"},
		{"neoforge-1.20.1", "neoforge", "20.1.100", "1.20.1"},
		{"neoforge-1.21", "neoforge", "21.0.167", "1.21"},
		{"forge-1.20.1", "forge", "47.3.0", "1.20.1"},
		{"forge-1.21", "forge", "51.0.8", "1.21"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pack := &Pack{
				ID:            tc.name,
				Name:          fmt.Sprintf("Matrix Pack %s", tc.name),
				Author:        "DiscoAdmin",
				Version:       "1.0.0",
				MCVersion:     tc.mcVersion,
				ModLoader:     tc.loader,
				LoaderVersion: tc.loaderVersion,
			}

			created, err := mgr.CreatePack(pack)
			require.NoError(t, err)
			assert.Equal(t, tc.name, created.ID)

			packDir := filepath.Join(tempDir, tc.name)
			assert.FileExists(t, filepath.Join(packDir, "pack.toml"))
			assert.FileExists(t, filepath.Join(packDir, "index.toml"))

			// Read pack.toml content to verify loader and version mapping
			packBytes, err := os.ReadFile(filepath.Join(packDir, "pack.toml"))
			require.NoError(t, err)
			assert.Contains(t, string(packBytes), tc.mcVersion)
			assert.Contains(t, string(packBytes), tc.loader)
			assert.Contains(t, string(packBytes), tc.loaderVersion)

			// Verify pack retrieval
			retrieved, err := mgr.GetPack(tc.name)
			require.NoError(t, err)
			assert.Equal(t, tc.mcVersion, retrieved.MCVersion)
			assert.Equal(t, tc.loader, retrieved.ModLoader)
			assert.Equal(t, tc.loaderVersion, retrieved.LoaderVersion)
		})
	}
}

// TestPackwizE2E_ModDependenciesIngestion tests adding mods with dependencies via Modrinth and CurseForge (MINE-34).
func TestPackwizE2E_ModDependenciesIngestion(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "packwiz_mods_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	log := logger.New()
	mgr := NewManager(tempDir, log)

	pack := &Pack{
		ID:            "dependency-pack",
		Name:          "Dependency Verification Pack",
		Author:        "Carbon Panel E2E",
		Version:       "1.0.0",
		MCVersion:     "1.20.1",
		ModLoader:     "fabric",
		LoaderVersion: "0.15.11",
	}

	_, err = mgr.CreatePack(pack)
	require.NoError(t, err)

	// 1. Ingest Modrinth Mod with Hashes and Meta
	modrinthMod := ModItem{
		Slug:        "sodium",
		Name:        "Sodium",
		FileName:    "sodium-fabric-0.5.11+mc1.20.1.jar",
		Side:        "both",
		Platform:    "modrinth",
		ProjectID:   "AANobbMI",
		VersionID:   "v12345",
		DownloadURL: "https://cdn.modrinth.com/data/AANobbMI/versions/v12345/sodium-fabric-0.5.11+mc1.20.1.jar",
		Pinned:      true,
	}
	err = mgr.AddMod("dependency-pack", modrinthMod)
	require.NoError(t, err)

	// 2. Ingest CurseForge Mod with ProjectID and FileID
	curseforgeMod := ModItem{
		Slug:        "jei",
		Name:        "Just Enough Items",
		FileName:    "jei-1.20.1-fabric-15.3.0.4.jar",
		Side:        "both",
		Platform:    "curseforge",
		ProjectID:   "238222",
		VersionID:   "4635832",
		DownloadURL: "https://edge.forgecdn.net/files/4635/832/jei-1.20.1-fabric-15.3.0.4.jar",
		Pinned:      false,
	}
	err = mgr.AddMod("dependency-pack", curseforgeMod)
	require.NoError(t, err)

	// 3. Ingest Server-Only Mod
	serverMod := ModItem{
		Slug:        "spark",
		Name:        "spark",
		FileName:    "spark-1.10.53-fabric.jar",
		Side:        "server",
		Platform:    "modrinth",
		ProjectID:   "l6YH9Als",
		VersionID:   "spark-v1",
		DownloadURL: "https://cdn.modrinth.com/data/l6YH9Als/versions/spark-v1/spark-1.10.53-fabric.jar",
	}
	err = mgr.AddMod("dependency-pack", serverMod)
	require.NoError(t, err)

	// Verify all 3 mods registered
	p, err := mgr.GetPack("dependency-pack")
	require.NoError(t, err)
	assert.Len(t, p.Mods, 3)

	// Verify .pw.toml files exist
	assert.FileExists(t, filepath.Join(tempDir, "dependency-pack", "mods", "sodium.pw.toml"))
	assert.FileExists(t, filepath.Join(tempDir, "dependency-pack", "mods", "jei.pw.toml"))
	assert.FileExists(t, filepath.Join(tempDir, "dependency-pack", "mods", "spark.pw.toml"))

	// Verify CurseForge export captures CurseForge dependencies
	var cfBuf bytes.Buffer
	err = mgr.ExportCurseForge("dependency-pack", &cfBuf)
	require.NoError(t, err)
	cfZip, err := zip.NewReader(bytes.NewReader(cfBuf.Bytes()), int64(cfBuf.Len()))
	require.NoError(t, err)

	var manifestFileFound bool
	for _, f := range cfZip.File {
		if f.Name == "manifest.json" {
			manifestFileFound = true
			rc, err := f.Open()
			require.NoError(t, err)
			var manifest struct {
				Files []struct {
					ProjectID int  `json:"projectID"`
					FileID    int  `json:"fileID"`
					Required  bool `json:"required"`
				} `json:"files"`
			}
			err = json.NewDecoder(rc).Decode(&manifest)
			rc.Close()
			require.NoError(t, err)
			assert.NotEmpty(t, manifest.Files)
			assert.Equal(t, 238222, manifest.Files[0].ProjectID)
			assert.Equal(t, 4635832, manifest.Files[0].FileID)
		}
	}
	assert.True(t, manifestFileFound)

	// Verify Modrinth export captures Modrinth dependencies
	var mrpackBuf bytes.Buffer
	err = mgr.ExportMrpack("dependency-pack", &mrpackBuf)
	require.NoError(t, err)
	mrpackZip, err := zip.NewReader(bytes.NewReader(mrpackBuf.Bytes()), int64(mrpackBuf.Len()))
	require.NoError(t, err)

	var mrIndexFound bool
	for _, f := range mrpackZip.File {
		if f.Name == "modrinth.index.json" {
			mrIndexFound = true
			rc, err := f.Open()
			require.NoError(t, err)
			var index struct {
				Files []struct {
					Path      string            `json:"path"`
					Downloads []string          `json:"downloads"`
					Env       map[string]string `json:"env"`
				} `json:"files"`
			}
			err = json.NewDecoder(rc).Decode(&index)
			rc.Close()
			require.NoError(t, err)
			assert.True(t, len(index.Files) >= 2)
		}
	}
	assert.True(t, mrIndexFound)
}

// TestPackwizE2E_ServerBootstrapAndBake verifies local PACKWIZ_URL HTTP serving and BakeToServer deployment (MINE-34).
func TestPackwizE2E_ServerBootstrapAndBake(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "packwiz_bootstrap_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Start local mock download server for BakeToServer test
	mockDownloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/java-archive")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("mock jar content"))
	}))
	defer mockDownloadServer.Close()

	log := logger.New()
	mgr := NewManager(tempDir, log)

	pack := &Pack{
		ID:            "bootstrap-pack",
		Name:          "Bootstrap Test Pack",
		Author:        "DiscoAdmin",
		Version:       "1.0.0",
		MCVersion:     "1.20.1",
		ModLoader:     "fabric",
		LoaderVersion: "0.15.11",
	}
	_, err = mgr.CreatePack(pack)
	require.NoError(t, err)

	mod := ModItem{
		Slug:        "lithium",
		Name:        "Lithium",
		FileName:    "lithium-fabric-0.11.2.jar",
		Side:        "both",
		Platform:    "modrinth",
		ProjectID:   "gvQqBUqZ",
		VersionID:   "lithium-ver-1",
		DownloadURL: mockDownloadServer.URL + "/lithium-fabric-0.11.2.jar",
	}
	err = mgr.AddMod("bootstrap-pack", mod)
	require.NoError(t, err)

	// 1. Verify ServePackFile can serve pack.toml and index.toml (used by PACKWIZ_URL container init)
	packData, ctype, err := mgr.ServePackFile("bootstrap-pack", "pack.toml")
	require.NoError(t, err)
	assert.Contains(t, ctype, "toml")
	assert.Contains(t, string(packData), "Bootstrap Test Pack")
	assert.Contains(t, string(packData), "index.toml")

	indexData, ctypeIndex, err := mgr.ServePackFile("bootstrap-pack", "index.toml")
	require.NoError(t, err)
	assert.Contains(t, ctypeIndex, "toml")
	assert.Contains(t, string(indexData), "lithium.pw.toml")

	// 2. Verify BakeToServer mode installs mod files into server directory
	serverModsDir := filepath.Join(tempDir, "server_data", "mods")
	err = os.MkdirAll(serverModsDir, 0755)
	require.NoError(t, err)

	installed, err := mgr.BakeToServer("bootstrap-pack", serverModsDir)
	require.NoError(t, err)
	assert.Equal(t, 1, installed)

	// Verify downloaded jar was written to target mods directory
	targetJar := filepath.Join(serverModsDir, "lithium-fabric-0.11.2.jar")
	assert.FileExists(t, targetJar)
	jarBytes, err := os.ReadFile(targetJar)
	require.NoError(t, err)
	assert.Equal(t, "mock jar content", string(jarBytes))
}
