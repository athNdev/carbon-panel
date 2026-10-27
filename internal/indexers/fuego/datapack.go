// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package fuego

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/indexers"
)

// DataPackClassID is the CurseForge Minecraft class for Data Packs (verified
// against live API project metadata: datapack projects report classId 6945).
const DataPackClassID = 6945

// dataPackWebsiteURL is the CurseForge site section used when the API's links
// block is empty.
const dataPackWebsiteURL = "https://www.curseforge.com/minecraft/data-packs/"

func init() {
	indexers.RegisterDatapackIndexer("curseforge", func(token string, cfg *config.Config) indexers.DatapackIndexer {
		return NewDatapackIndexer(token, cfg)
	})
}

// FuegoDatapackIndexer adapts the CurseForge client to the DatapackIndexer
// interface. Data packs are CurseForge class 6945 and run on Vanilla servers.
type FuegoDatapackIndexer struct {
	client *Client
}

// NewDatapackIndexer creates a CurseForge datapack indexer. An API key selects
// the official endpoint; without one the keyless mirror is used.
func NewDatapackIndexer(apiKey string, cfg *config.Config) *FuegoDatapackIndexer {
	return &FuegoDatapackIndexer{client: NewClient(apiKey, cfg)}
}

var _ indexers.DatapackIndexer = (*FuegoDatapackIndexer)(nil)
var _ indexers.OriginReporter = (*FuegoDatapackIndexer)(nil)

// GetIndexerName returns the indexer name.
func (f *FuegoDatapackIndexer) GetIndexerName() string {
	return "curseforge"
}

// Origin returns the configured CurseForge API origin (OriginReporter).
func (f *FuegoDatapackIndexer) Origin() string {
	return f.client.baseURL
}

// SearchDatapacks searches CurseForge's Data Packs class. Data packs are
// loader-agnostic, so only the game version narrows the results.
func (f *FuegoDatapackIndexer) SearchDatapacks(ctx context.Context, query string, gameVersion string, offset, limit int) (*indexers.DatapackSearchResult, error) {
	params := url.Values{}
	params.Set("gameId", strconv.Itoa(MinecraftGameID))
	params.Set("classId", strconv.Itoa(DataPackClassID))
	params.Set("index", strconv.Itoa(offset))
	params.Set("pageSize", strconv.Itoa(limit))
	params.Set("sortField", "6") // Sort by total downloads
	params.Set("sortOrder", "desc")
	if query != "" {
		params.Set("searchFilter", query)
	}
	if gameVersion != "" {
		params.Set("gameVersion", gameVersion)
	}

	var result SearchModsResponse
	urlStr := fmt.Sprintf("%s/mods/search?%s", f.client.baseURL, params.Encode())
	if err := f.client.http.DoJSON(ctx, urlStr, &result); err != nil {
		return nil, err
	}

	datapacks := make([]indexers.Datapack, len(result.Data))
	for i, m := range result.Data {
		author := ""
		if len(m.Authors) > 0 {
			author = m.Authors[0].Name
		}
		website := m.Links.WebsiteURL
		if website == "" {
			website = dataPackWebsiteURL + m.Slug
		}
		versions := map[string]bool{}
		var gameVersions []string
		for _, idx := range m.LatestFilesIndexes {
			if idx.GameVersion != "" && !versions[idx.GameVersion] {
				versions[idx.GameVersion] = true
				gameVersions = append(gameVersions, idx.GameVersion)
			}
		}
		datapacks[i] = indexers.Datapack{
			ID:            strconv.Itoa(m.ID),
			IndexerID:     strconv.Itoa(m.ID),
			Indexer:       "curseforge",
			Name:          m.Name,
			Slug:          m.Slug,
			Summary:       m.Summary,
			IconURL:       m.Logo.ThumbnailURL,
			WebsiteURL:    website,
			Author:        author,
			DownloadCount: int64(m.DownloadCount),
			GameVersions:  gameVersions,
			DateModified:  m.DateModified.Format(time.RFC3339),
		}
	}

	return &indexers.DatapackSearchResult{
		Datapacks:  datapacks,
		TotalCount: result.Pagination.TotalCount,
		PageSize:   result.Pagination.PageSize,
		Offset:     result.Pagination.Index,
	}, nil
}

// GetDatapackLatestFile returns the newest ZIP file for a datapack compatible
// with the given game version (any version when gameVersion is empty). CurseForge
// returns files newest-first; ZIP-only selection matters because some data-pack
// projects also ship non-ZIP side files.
func (f *FuegoDatapackIndexer) GetDatapackLatestFile(ctx context.Context, datapackID string, gameVersion string) (*indexers.DatapackFile, error) {
	modID, err := strconv.Atoi(datapackID)
	if err != nil {
		return nil, fmt.Errorf("invalid curseforge datapack id %q", datapackID)
	}

	files, err := f.client.GetModpackFiles(ctx, modID)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		if !file.IsAvailable || !strings.EqualFold(filepath.Ext(file.FileName), ".zip") {
			continue
		}
		if gameVersion != "" && !fileHasGameVersion(file, gameVersion) {
			continue
		}
		return &indexers.DatapackFile{
			DatapackID:   datapackID,
			Filename:     file.FileName,
			DownloadURL:  file.DownloadURL,
			Size:         file.FileLength,
			SHA1:         hashValue(file, 1), // CurseForge hash algo 1 = SHA-1
			SHA512:       hashValue(file, 3), // CurseForge hash algo 3 = SHA-512
			GameVersions: file.GameVersions,
		}, nil
	}

	return nil, fmt.Errorf("no datapack file found for %s (game version %q)", datapackID, gameVersion)
}

func fileHasGameVersion(file File, gameVersion string) bool {
	for _, v := range file.GameVersions {
		if strings.EqualFold(v, gameVersion) {
			return true
		}
	}
	return false
}

func hashValue(file File, algo int) string {
	for _, h := range file.Hashes {
		if h.Algo == algo {
			return h.Value
		}
	}
	return ""
}
