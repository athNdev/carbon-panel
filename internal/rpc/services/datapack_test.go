// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/indexers"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"github.com/athNdev/carbon-panel/pkg/upload"
	"github.com/stretchr/testify/require"
)

func TestDisplayNameFromFilename(t *testing.T) {
	if got := displayNameFromFilename("my-pack.zip"); got != "my-pack" {
		t.Errorf("displayNameFromFilename = %q, want %q", got, "my-pack")
	}
	if got := displayNameFromFilename("noext"); got != "noext" {
		t.Errorf("displayNameFromFilename = %q, want %q", got, "noext")
	}
}

type fakeDatapackIndexer struct {
	name      string
	datapacks []indexers.Datapack
	total     int
	err       error
}

func (f *fakeDatapackIndexer) SearchDatapacks(_ context.Context, _, _ string, _, _ int) (*indexers.DatapackSearchResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &indexers.DatapackSearchResult{Datapacks: f.datapacks, TotalCount: f.total}, nil
}

func (f *fakeDatapackIndexer) GetDatapackLatestFile(context.Context, string, string) (*indexers.DatapackFile, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeDatapackIndexer) GetIndexerName() string { return f.name }

// TestSearchDatapacksMergesProviders covers multi-provider search: results are
// merged by popularity and one provider's outage degrades coverage instead of
// failing the whole search.
func TestSearchDatapacksMergesProviders(t *testing.T) {
	a := &fakeDatapackIndexer{name: "a", total: 10, datapacks: []indexers.Datapack{
		{ID: "a1", Name: "A1", DownloadCount: 5},
		{ID: "a2", Name: "A2", DownloadCount: 50},
	}}
	b := &fakeDatapackIndexer{name: "b", total: 20, datapacks: []indexers.Datapack{
		{ID: "b1", Name: "B1", DownloadCount: 100},
	}}
	broken := &fakeDatapackIndexer{name: "broken", err: errors.New("boom")}

	s := &DatapackService{
		config:        &config.Config{},
		log:           logger.New(),
		searchIndexes: []indexers.DatapackIndexer{a, b, broken},
	}

	res, err := s.SearchDatapacks(context.Background(), connect.NewRequest(&v1.SearchDatapacksRequest{Limit: 10}))
	require.NoError(t, err)
	require.Equal(t, int32(30), res.Msg.TotalCount)

	var ids []string
	for _, d := range res.Msg.Datapacks {
		ids = append(ids, d.Id)
	}
	require.Equal(t, []string{"b1", "a2", "a1"}, ids, "merged by popularity across providers")
}

// TestSearchDatapacksAllProvidersFail: a total outage is still an error.
func TestSearchDatapacksAllProvidersFail(t *testing.T) {
	s := &DatapackService{
		config: &config.Config{},
		log:    logger.New(),
		searchIndexes: []indexers.DatapackIndexer{
			&fakeDatapackIndexer{name: "x", err: errors.New("down")},
			&fakeDatapackIndexer{name: "y", err: errors.New("also down")},
		},
	}
	_, err := s.SearchDatapacks(context.Background(), connect.NewRequest(&v1.SearchDatapacksRequest{}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}

func zipArchiveBytes(t *testing.T, entries map[string]string) []byte {
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

func newUploadTestService(t *testing.T) (*DatapackService, *upload.Manager, *storage.Server) {
	t.Helper()
	store, err := storage.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "panel.sqlite")},
	})
	require.NoError(t, err)
	require.NoError(t, store.Migrate())
	t.Cleanup(func() { _ = store.Close() })

	server := &storage.Server{ID: "srv-up", Name: "up", DataPath: t.TempDir()}
	require.NoError(t, store.CreateServer(context.Background(), server))

	um := upload.NewManager(t.TempDir(), time.Minute, 10<<20, logger.New())
	t.Cleanup(um.Stop)
	return NewDatapackService(store, &config.Config{}, um, logger.New()), um, server
}

func uploadSession(t *testing.T, um *upload.Manager, filename string, data []byte) string {
	t.Helper()
	sess, err := um.InitSession(filename, int64(len(data)), int32(len(data)))
	require.NoError(t, err)
	_, done, err := um.WriteStream(sess.ID, bytes.NewReader(data), 0)
	require.NoError(t, err)
	require.True(t, done)
	return sess.ID
}

func TestUploadDatapack(t *testing.T) {
	t.Run("installs a valid upload", func(t *testing.T) {
		s, um, server := newUploadTestService(t)
		data := zipArchiveBytes(t, map[string]string{
			"pack.mcmeta":      `{"pack":{"pack_format":34}}`,
			"data/minecraft/x": "",
		})
		session := uploadSession(t, um, "my-pack.zip", data)

		res, err := s.UploadDatapack(context.Background(), connect.NewRequest(&v1.UploadDatapackRequest{
			ServerId:        server.ID,
			UploadSessionId: session,
		}))
		require.NoError(t, err)
		require.Equal(t, "my-pack.zip", res.Msg.Datapack.FileName)
		require.Equal(t, "my-pack", res.Msg.Datapack.DisplayName)

		installed := filepath.Join(server.DataPath, "world", "datapacks", "my-pack.zip")
		require.FileExists(t, installed)
	})

	t.Run("repairs wrapper archives", func(t *testing.T) {
		s, um, server := newUploadTestService(t)
		data := zipArchiveBytes(t, map[string]string{
			"my-pack/pack.mcmeta":      `{"pack":{"pack_format":34}}`,
			"my-pack/data/minecraft/x": "",
		})
		session := uploadSession(t, um, "wrapped.zip", data)

		_, err := s.UploadDatapack(context.Background(), connect.NewRequest(&v1.UploadDatapackRequest{
			ServerId:        server.ID,
			UploadSessionId: session,
		}))
		require.NoError(t, err)
		require.FileExists(t, filepath.Join(server.DataPath, "world", "datapacks", "wrapped.zip"))
	})

	t.Run("rejects non-datapack uploads and cleans the session", func(t *testing.T) {
		s, um, server := newUploadTestService(t)
		data := zipArchiveBytes(t, map[string]string{"readme.txt": "not a pack"})
		session := uploadSession(t, um, "junk.zip", data)

		_, err := s.UploadDatapack(context.Background(), connect.NewRequest(&v1.UploadDatapackRequest{
			ServerId:        server.ID,
			UploadSessionId: session,
		}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

		_, _, getErr := um.GetTempPath(session)
		require.Error(t, getErr, "failed upload must clean up its session")

		entries, readErr := filepath.Glob(filepath.Join(server.DataPath, "world", "datapacks", "*"))
		require.NoError(t, readErr)
		require.Empty(t, entries, "nothing may land in the world")
	})

	t.Run("rejects unsafe names and missing sessions", func(t *testing.T) {
		s, um, server := newUploadTestService(t)
		data := zipArchiveBytes(t, map[string]string{"pack.mcmeta": `{}`})
		session := uploadSession(t, um, "pack.zip", data)

		_, err := s.UploadDatapack(context.Background(), connect.NewRequest(&v1.UploadDatapackRequest{
			ServerId:        server.ID,
			UploadSessionId: session,
			FileName:        "../evil.zip",
		}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

		_, err = s.UploadDatapack(context.Background(), connect.NewRequest(&v1.UploadDatapackRequest{
			ServerId: server.ID,
		}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})
}
