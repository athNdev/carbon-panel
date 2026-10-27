// SPDX-License-Identifier: MIT AND MPL-2.0
// This file includes DiscoPanel material under the MIT License,
// Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
// and still applies to that inherited material.
// Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
// If a copy of the MPL was not distributed with this file, You can obtain
// one at https://mozilla.org/MPL/2.0/.

package modrinth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/indexers"
)

func init() {
	indexers.RegisterDatapackIndexer("modrinth", func(_ string, cfg *config.Config) indexers.DatapackIndexer {
		return NewDatapackIndexer(cfg)
	})
}

// Implements DatapackIndexer
var _ indexers.DatapackIndexer = (*ModrinthDatapackIndexer)(nil)

// ModrinthDatapackIndexer adapts the Modrinth client to the DatapackIndexer
// interface. Datapacks are a distinct Modrinth project type (project_type:datapack)
// that runs on Vanilla servers without a mod loader.
type ModrinthDatapackIndexer struct {
	client *Client
}

// NewDatapackIndexer creates a Modrinth datapack indexer. The public Modrinth
// API needs no key for search or download.
func NewDatapackIndexer(cfg *config.Config) *ModrinthDatapackIndexer {
	return &ModrinthDatapackIndexer{client: NewClient(cfg)}
}

// GetIndexerName returns the indexer name.
func (m *ModrinthDatapackIndexer) GetIndexerName() string {
	return "modrinth"
}

// Origin returns the configured Modrinth API origin (OriginReporter).
func (m *ModrinthDatapackIndexer) Origin() string {
	return BaseURL
}

// SearchDatapacks searches Modrinth for datapacks. Datapacks are loader-agnostic,
// so only the game version narrows the results.
func (m *ModrinthDatapackIndexer) SearchDatapacks(ctx context.Context, query string, gameVersion string, offset, limit int) (*indexers.DatapackSearchResult, error) {
	facets := [][]string{{"project_type:datapack"}}
	if gameVersion != "" {
		facets = append(facets, []string{fmt.Sprintf("versions:%s", gameVersion)})
	}

	facetsJSON, err := json.Marshal(facets)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal facets: %w", err)
	}

	params := url.Values{}
	if query != "" {
		params.Set("query", query)
	}
	params.Set("facets", string(facetsJSON))
	params.Set("index", "downloads")
	params.Set("offset", strconv.Itoa(offset))
	params.Set("limit", strconv.Itoa(limit))

	var resp SearchResponse
	if err := m.client.http.DoJSON(ctx, fmt.Sprintf("%s/search?%s", BaseURL, params.Encode()), &resp); err != nil {
		return nil, err
	}

	datapacks := make([]indexers.Datapack, len(resp.Hits))
	for i, hit := range resp.Hits {
		datapacks[i] = indexers.Datapack{
			ID:            hit.ProjectID,
			IndexerID:     hit.ProjectID,
			Indexer:       "modrinth",
			Name:          hit.Title,
			Slug:          hit.Slug,
			Summary:       hit.Description,
			IconURL:       hit.IconURL,
			WebsiteURL:    fmt.Sprintf("https://modrinth.com/datapack/%s", hit.Slug),
			Author:        hit.Author,
			DownloadCount: hit.Downloads,
			GameVersions:  hit.Versions,
			DateModified:  hit.DateModified,
		}
	}

	return &indexers.DatapackSearchResult{
		Datapacks:  datapacks,
		TotalCount: resp.TotalHits,
		PageSize:   resp.Limit,
		Offset:     resp.Offset,
	}, nil
}

// GetDatapackLatestFile returns the primary file for a datapack's newest version
// compatible with the given game version (any version when gameVersion is empty).
func (m *ModrinthDatapackIndexer) GetDatapackLatestFile(ctx context.Context, datapackID string, gameVersion string) (*indexers.DatapackFile, error) {
	versions, err := m.client.GetModpackVersions(ctx, datapackID)
	if err != nil {
		return nil, err
	}

	for _, v := range versions {
		if gameVersion != "" && !contains(v.GameVersions, gameVersion) {
			continue
		}
		file := datapackZipFile(v.Files)
		if file == nil {
			continue
		}
		return &indexers.DatapackFile{
			DatapackID:   datapackID,
			Filename:     file.Filename,
			DownloadURL:  file.URL,
			Size:         file.Size,
			SHA1:         file.Hashes.SHA1,
			SHA512:       file.Hashes.SHA512,
			GameVersions: v.GameVersions,
		}, nil
	}

	return nil, fmt.Errorf("no datapack file found for %s (game version %q)", datapackID, gameVersion)
}

// datapackZipFile returns the ZIP artifact from a version's files, or nil when
// the version ships no ZIP. Datapacks are ZIP archives, but some projects
// dual-publish a mod (.jar) and a datapack (.zip) as sibling version entries,
// so never fall back to a non-ZIP file: a mod jar is not installable as a
// datapack even when it is the version's primary artifact.
func datapackZipFile(files []File) *File {
	var firstZip *File
	for i := range files {
		if !strings.EqualFold(filepath.Ext(files[i].Filename), ".zip") {
			continue
		}
		if files[i].Primary {
			return &files[i]
		}
		if firstZip == nil {
			firstZip = &files[i]
		}
	}
	return firstZip
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}
