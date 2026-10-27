// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/indexers"
	"github.com/athNdev/carbon-panel/internal/minecraft"
	"github.com/athNdev/carbon-panel/pkg/files"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"github.com/athNdev/carbon-panel/pkg/upload"
)

// DatapackService manages datapack discovery and installation. Datapacks are
// world-data bundles that run on Vanilla servers (no mod loader) and live under
// <world>/datapacks.
type DatapackService struct {
	store         *storage.Store
	config        *config.Config
	log           *logger.Logger
	uploadManager *upload.Manager
	// indexer, when set, overrides the registry lookup. Tests use this to
	// inject a fake index; production leaves it nil.
	indexer indexers.DatapackIndexer
	// searchIndexes, when set, overrides the provider set for merged
	// multi-provider search. Tests use it to inject fakes without touching
	// the registry; production leaves it nil.
	searchIndexes []indexers.DatapackIndexer
}

// NewDatapackService creates a datapack service.
func NewDatapackService(store *storage.Store, cfg *config.Config, uploadManager *upload.Manager, log *logger.Logger) *DatapackService {
	return &DatapackService{store: store, config: cfg, uploadManager: uploadManager, log: log}
}

// getDatapackIndexer resolves a datapack indexer by name (default: modrinth).
func (s *DatapackService) getDatapackIndexer(name string) (indexers.DatapackIndexer, error) {
	if s.indexer != nil {
		return s.indexer, nil
	}
	return indexers.NewDatapackIndexer(name, s.curseForgeAPIKey(), s.config)
}

// curseForgeAPIKey returns the configured CurseForge API key if there is one;
// without it the CurseForge adapter uses its keyless mirror.
func (s *DatapackService) curseForgeAPIKey() string {
	if globalSettings, _, err := s.store.GetGlobalSettings(context.Background()); err == nil && globalSettings != nil && globalSettings.CFAPIKey != nil {
		return *globalSettings.CFAPIKey
	}
	return ""
}

// toProtoDatapack maps an indexer datapack to its wire representation.
func toProtoDatapack(d indexers.Datapack) *v1.Datapack {
	return &v1.Datapack{
		Id:            d.ID,
		Indexer:       d.Indexer,
		Name:          d.Name,
		Slug:          d.Slug,
		Summary:       d.Summary,
		IconUrl:       d.IconURL,
		WebsiteUrl:    d.WebsiteURL,
		Author:        d.Author,
		DownloadCount: d.DownloadCount,
		GameVersions:  d.GameVersions,
	}
}

// SearchDatapacks queries datapack indexes for installable datapacks. Without
// an explicit indexer it searches every registered provider concurrently and
// merges by popularity, so one provider's outage degrades coverage instead of
// failing the search.
func (s *DatapackService) SearchDatapacks(ctx context.Context, req *connect.Request[v1.SearchDatapacksRequest]) (*connect.Response[v1.SearchDatapacksResponse], error) {
	msg := req.Msg

	limit := int(msg.Limit)
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	offset := int(msg.Offset)
	if offset < 0 {
		offset = 0
	}

	if msg.Indexer != "" {
		idx, err := s.getDatapackIndexer(msg.Indexer)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		result, err := idx.SearchDatapacks(ctx, msg.Query, msg.GameVersion, offset, limit)
		if err != nil {
			s.log.Error("Datapack search failed: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("datapack search failed: %w", err))
		}
		datapacks := make([]*v1.Datapack, len(result.Datapacks))
		for i, d := range result.Datapacks {
			datapacks[i] = toProtoDatapack(d)
		}
		return connect.NewResponse(&v1.SearchDatapacksResponse{
			Datapacks:  datapacks,
			TotalCount: int32(result.TotalCount),
			PageSize:   int32(result.PageSize),
			Offset:     int32(result.Offset),
		}), nil
	}

	var idxs []indexers.DatapackIndexer
	if len(s.searchIndexes) > 0 {
		idxs = s.searchIndexes
	} else {
		for _, name := range indexers.DatapackIndexerNames() {
			idx, err := s.getDatapackIndexer(name)
			if err != nil {
				s.log.Error("Datapack indexer %q unavailable: %v", name, err)
				continue
			}
			idxs = append(idxs, idx)
		}
	}
	if len(idxs) == 0 {
		return nil, connect.NewError(connect.CodeInternal, errors.New("no datapack providers available"))
	}

	type indexResult struct {
		datapacks []indexers.Datapack
		total     int
		err       error
	}
	results := make([]indexResult, len(idxs))
	var wg sync.WaitGroup
	for i, idx := range idxs {
		wg.Add(1)
		go func(i int, idx indexers.DatapackIndexer) {
			defer wg.Done()
			res, err := idx.SearchDatapacks(ctx, msg.Query, msg.GameVersion, offset, limit)
			if err != nil {
				results[i].err = err
				return
			}
			results[i].datapacks = res.Datapacks
			results[i].total = res.TotalCount
		}(i, idx)
	}
	wg.Wait()

	var merged []indexers.Datapack
	total := 0
	okCount := 0
	for i, res := range results {
		if res.err != nil {
			s.log.Error("Datapack search on %q failed: %v", idxs[i].GetIndexerName(), res.err)
			continue
		}
		okCount++
		merged = append(merged, res.datapacks...)
		total += res.total
	}
	if okCount == 0 {
		return nil, connect.NewError(connect.CodeInternal, errors.New("datapack search failed on all providers"))
	}

	// Interleave providers by popularity so one large index cannot crowd out
	// the others' top hits.
	sort.SliceStable(merged, func(a, b int) bool {
		return merged[a].DownloadCount > merged[b].DownloadCount
	})
	if len(merged) > limit {
		merged = merged[:limit]
	}
	datapacks := make([]*v1.Datapack, len(merged))
	for i, d := range merged {
		datapacks[i] = toProtoDatapack(d)
	}

	return connect.NewResponse(&v1.SearchDatapacksResponse{
		Datapacks:  datapacks,
		TotalCount: int32(total),
		PageSize:   int32(limit),
		Offset:     int32(offset),
	}), nil
}

// InstallDatapack downloads a datapack from an index into the server's world.
func (s *DatapackService) InstallDatapack(ctx context.Context, req *connect.Request[v1.InstallDatapackRequest]) (*connect.Response[v1.InstallDatapackResponse], error) {
	msg := req.Msg

	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Resolve the file from the provider that produced the id; when the caller
	// does not name one (older clients), try every provider so results from any
	// search remain installable.
	var file *indexers.DatapackFile
	var resolved indexers.DatapackIndexer
	if s.indexer != nil {
		resolved = s.indexer
		file, err = s.indexer.GetDatapackLatestFile(ctx, msg.DatapackId, msg.GameVersion)
	} else {
		var idxs []indexers.DatapackIndexer
		idxs, err = indexers.DatapackIndexersFor(msg.Indexer, s.curseForgeAPIKey(), s.config)
		if err == nil {
			file, resolved, err = indexers.FirstDatapackLatestFile(ctx, msg.DatapackId, msg.GameVersion, idxs)
		}
	}
	if err != nil {
		s.log.Error("Failed to resolve datapack file for %s: %v", msg.DatapackId, err)
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("could not find a datapack file: %w", err))
	}

	datapacksDir := minecraft.GetDatapacksPath(server.DataPath, "")
	if err := os.MkdirAll(datapacksDir, 0755); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create datapacks directory: %w", err))
	}

	filename := strings.TrimSpace(msg.FileName)
	if filename == "" {
		filename = filepath.Base(file.Filename)
	}
	if err := minecraft.ValidateDatapackFilename(filename); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	destPath := filepath.Join(datapacksDir, filename)

	if err := minecraft.DownloadDatapackArchiveFrom(ctx, indexers.DatapackIndexerOrigin(resolved), file.DownloadURL, destPath); err != nil {
		s.log.Error("Failed to download datapack %s: %v", file.DownloadURL, err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to download datapack: %w", err))
	}

	// Reject anything that is not a real datapack before it reaches the world.
	// Repair the common wrapper-folder packaging mistake on the way.
	if err := minecraft.ValidateDatapackArchive(destPath); err != nil {
		normalized, nerr := minecraft.NormalizeDatapackArchive(destPath)
		if nerr != nil || !normalized {
			_ = os.Remove(destPath)
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("downloaded file is not a valid datapack: %w", err))
		}
		s.log.Info("Normalized wrapped datapack archive %s for server %s", filename, server.ID)
	}

	info, statErr := os.Stat(destPath)
	if statErr != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("installed datapack missing: %w", statErr))
	}

	s.log.Info("Installed datapack %s for server %s", filename, server.ID)
	return connect.NewResponse(&v1.InstallDatapackResponse{
		Datapack: withPackFormat(&v1.InstalledDatapack{
			FileName:    filename,
			DisplayName: displayNameFromFilename(filename),
			Size:        info.Size(),
		}, destPath, server.MCVersion),
	}), nil
}

// UploadDatapack imports an uploaded datapack archive into the server's world.
// The archive is validated as a real Minecraft datapack before it lands, with
// the same wrapper-folder repair as indexed installs.
func (s *DatapackService) UploadDatapack(ctx context.Context, req *connect.Request[v1.UploadDatapackRequest]) (*connect.Response[v1.UploadDatapackResponse], error) {
	msg := req.Msg

	if msg.UploadSessionId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("upload_session_id is required"))
	}

	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	tempPath, originalFilename, err := s.uploadManager.GetTempPath(msg.UploadSessionId)
	if err != nil {
		s.log.Error("Failed to get upload session: %v", err)
		return nil, connect.NewError(connect.CodeNotFound, errors.New("upload session not found or not completed"))
	}
	// Failures after this point own the temp file and the session.
	fail := func(err error) (*connect.Response[v1.UploadDatapackResponse], error) {
		_ = os.Remove(tempPath)
		_ = s.uploadManager.CleanupSession(msg.UploadSessionId)
		return nil, err
	}

	filename := strings.TrimSpace(msg.FileName)
	if filename == "" {
		filename = filepath.Base(originalFilename)
	}
	if err := minecraft.ValidateDatapackFilename(filename); err != nil {
		return fail(connect.NewError(connect.CodeInvalidArgument, err))
	}

	// Reject anything that is not a real datapack before it reaches the world,
	// repairing the common wrapper-folder packaging mistake on the way.
	if err := minecraft.ValidateDatapackArchive(tempPath); err != nil {
		normalized, nerr := minecraft.NormalizeDatapackArchive(tempPath)
		if nerr != nil || !normalized {
			return fail(connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("uploaded file is not a valid datapack: %w", err)))
		}
		s.log.Info("Normalized wrapped datapack archive %s for server %s", filename, server.ID)
	}

	datapacksDir := minecraft.GetDatapacksPath(server.DataPath, "")
	if err := os.MkdirAll(datapacksDir, 0755); err != nil {
		return fail(connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create datapacks directory: %w", err)))
	}

	destPath := filepath.Join(datapacksDir, filename)
	if err := os.Rename(tempPath, destPath); err != nil {
		if copyErr := files.CopyFile(tempPath, destPath); copyErr != nil {
			return fail(connect.NewError(connect.CodeInternal, errors.New("failed to save datapack")))
		}
		_ = os.Remove(tempPath)
	}
	_ = s.uploadManager.CleanupSession(msg.UploadSessionId)

	info, err := os.Stat(destPath)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("uploaded datapack missing: %w", err))
	}

	s.log.Info("Uploaded datapack %s for server %s", filename, server.ID)
	return connect.NewResponse(&v1.UploadDatapackResponse{
		Datapack: withPackFormat(&v1.InstalledDatapack{
			FileName:    filename,
			DisplayName: displayNameFromFilename(filename),
			Size:        info.Size(),
		}, destPath, server.MCVersion),
		Message: "Datapack uploaded successfully",
	}), nil
}

// ListDatapacks lists the datapacks installed in a server's world.
func (s *DatapackService) ListDatapacks(ctx context.Context, req *connect.Request[v1.ListDatapacksRequest]) (*connect.Response[v1.ListDatapacksResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	dir := minecraft.GetDatapacksPath(server.DataPath, "")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return connect.NewResponse(&v1.ListDatapacksResponse{}), nil
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list datapacks: %w", err))
	}

	var out []*v1.InstalledDatapack
	for _, e := range entries {
		if e.IsDir() || !minecraft.IsValidDatapackFile(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, withPackFormat(&v1.InstalledDatapack{
			FileName:    e.Name(),
			DisplayName: displayNameFromFilename(e.Name()),
			Size:        info.Size(),
		}, filepath.Join(dir, e.Name()), server.MCVersion))
	}

	return connect.NewResponse(&v1.ListDatapacksResponse{Datapacks: out}), nil
}

// DeleteDatapack removes an installed datapack from a server's world.
func (s *DatapackService) DeleteDatapack(ctx context.Context, req *connect.Request[v1.DeleteDatapackRequest]) (*connect.Response[v1.DeleteDatapackResponse], error) {
	msg := req.Msg
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	if err := minecraft.ValidateDatapackFilename(msg.FileName); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	dir := minecraft.GetDatapacksPath(server.DataPath, "")
	target := filepath.Join(dir, msg.FileName)
	// Re-resolve inside the datapacks dir so a crafted name cannot escape it.
	if filepath.Dir(target) != filepath.Clean(dir) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid datapack file name"))
	}

	if err := os.Remove(target); err != nil {
		if os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("datapack not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete datapack: %w", err))
	}

	s.log.Info("Deleted datapack %s for server %s", msg.FileName, server.ID)
	return connect.NewResponse(&v1.DeleteDatapackResponse{}), nil
}

// withPackFormat annotates an installed datapack with its declared pack
// format and any compatibility warning against the server version.
func withPackFormat(dp *v1.InstalledDatapack, path, mcVersion string) *v1.InstalledDatapack {
	if format, err := minecraft.ReadPackFormat(path); err == nil {
		dp.PackFormat = format.String()
		dp.FormatWarning = minecraft.PackFormatWarning(format, mcVersion)
	}
	return dp
}

// displayNameFromFilename strips the .zip extension for display.
func displayNameFromFilename(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}
