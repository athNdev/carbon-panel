package services

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestModpackService_IndexErrorAndSha256(t *testing.T) {
	store := setupTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	log := logger.New()

	cfg := &config.Config{
		Storage: config.StorageConfig{
			DataDir: t.TempDir(),
		},
	}
	svc := NewModpackService(store, cfg, nil, log)
	ctx := context.Background()

	// 1. Create a modpack in store with an IndexError
	modpackID := "test-pack-" + uuid.New().String()
	testErr := "rate limit exceeded: wait 60s"
	dbModpack := &storage.IndexedModpack{
		ID:             modpackID,
		IndexerID:      "indexer-123",
		Indexer:        "manual",
		Name:           "Test Pack With Error",
		Slug:           "test-pack-error",
		Summary:        "Testing index error surface",
		DateCreated:    time.Now(),
		DateModified:   time.Now(),
		DateReleased:   time.Now(),
		MCVersion:      "1.20.1",
		JavaVersion:    "17",
		RecommendedRAM: 4096,
		IndexError:     testErr,
	}
	require.NoError(t, store.UpsertIndexedModpack(ctx, dbModpack))

	// Verify GetModpack returns IndexError
	getResp, err := svc.GetModpack(ctx, connect.NewRequest(&v1.GetModpackRequest{Id: modpackID}))
	require.NoError(t, err)
	assert.Equal(t, testErr, getResp.Msg.Modpack.IndexError)

	// Verify SearchModpacks returns IndexError
	searchResp, err := svc.SearchModpacks(ctx, connect.NewRequest(&v1.SearchModpacksRequest{Query: "Test Pack"}))
	require.NoError(t, err)
	require.NotEmpty(t, searchResp.Msg.Modpacks)
	found := false
	for _, p := range searchResp.Msg.Modpacks {
		if p.Id == modpackID {
			assert.Equal(t, testErr, p.IndexError)
			found = true
			break
		}
	}
	assert.True(t, found, "expected to find modpack in search results")

	// 2. Add an IndexedModpackFile with Sha256
	expectedSHA := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	dbFile := &storage.IndexedModpackFile{
		ID:          "file-1",
		ModpackID:   modpackID,
		DisplayName: "v1.0.0",
		FileName:    "pack-1.0.0.zip",
		FileDate:    time.Now(),
		FileLength:  1024,
		ReleaseType: "release",
		Sha256:      expectedSHA,
	}
	require.NoError(t, store.UpsertIndexedModpackFile(ctx, dbFile))

	// Verify GetModpackFiles exposes Sha256
	filesResp, err := svc.GetModpackFiles(ctx, connect.NewRequest(&v1.GetModpackFilesRequest{Id: modpackID}))
	require.NoError(t, err)
	require.Len(t, filesResp.Msg.Files, 1)
	assert.Equal(t, expectedSHA, filesResp.Msg.Files[0].Sha256)
}

func TestModpackService_ImportRemoteModpack_ComputesSha256(t *testing.T) {
	t.Setenv("DISCO_ALLOW_LOOPBACK_TEST", "1")
	store := setupTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	log := logger.New()

	dataDir := t.TempDir()
	cfg := &config.Config{
		Storage: config.StorageConfig{
			DataDir: dataDir,
		},
	}
	svc := NewModpackService(store, cfg, nil, log)
	ctx := context.Background()

	// Generate a valid minimal ZIP archive
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	f, err := zw.Create("manifest.json")
	require.NoError(t, err)
	manifestData := `{"name": "Remote Pack", "version": "1.0.0", "author": "tester", "minecraft": {"version": "1.20.1", "modLoaders": [{"id": "fabric-0.14", "primary": true}]}}`
	_, err = f.Write([]byte(manifestData))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	zipBytes := zipBuf.Bytes()
	expectedHash := sha256.Sum256(zipBytes)
	expectedHex := hex.EncodeToString(expectedHash[:])

	// Host it on httptest server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(zipBytes)
	}))
	defer ts.Close()

	// Import via ImportRemoteModpack (with AllowPrivateNetwork=true since httptest binds loopback)
	req := &v1.ImportRemoteModpackRequest{
		Url:                  ts.URL + "/pack.zip",
		Name:                 "Remote Pack",
		AllowPrivateNetwork:  true,
	}
	resp, err := svc.ImportRemoteModpack(ctx, connect.NewRequest(req))
	require.NoError(t, err)
	assert.Equal(t, expectedHex, resp.Msg.Sha256)
	assert.Equal(t, "Remote Pack", resp.Msg.Modpack.Name)

	// Verify file record in database also stored sha256
	files, err := store.GetIndexedModpackFiles(ctx, resp.Msg.Modpack.Id)
	require.NoError(t, err)
	require.NotEmpty(t, files)
	assert.Equal(t, expectedHex, files[0].Sha256)
}
func TestModpackService_ImportRemoteModpack_ExceedsMaxSize(t *testing.T) {
	t.Setenv("DISCO_ALLOW_LOOPBACK_TEST", "1")
	store := setupTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	log := logger.New()

	dataDir := t.TempDir()
	cfg := &config.Config{
		Storage: config.StorageConfig{
			DataDir: dataDir,
		},
	}
	svc := NewModpackService(store, cfg, nil, log)
	ctx := context.Background()

	// Set small size limit
	origLimit := maxRemoteModpackSize
	maxRemoteModpackSize = 50
	defer func() { maxRemoteModpackSize = origLimit }()

	// Host a 100-byte response
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(bytes.Repeat([]byte("A"), 100))
	}))
	defer ts.Close()

	req := &v1.ImportRemoteModpackRequest{
		Url:                 ts.URL + "/pack.zip",
		Name:                "Huge Pack",
		AllowPrivateNetwork: true,
	}
	_, err := svc.ImportRemoteModpack(ctx, connect.NewRequest(req))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum allowed size")
}
