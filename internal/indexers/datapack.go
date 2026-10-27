// SPDX-License-Identifier: MIT AND MPL-2.0
// This file includes DiscoPanel material under the MIT License,
// Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
// and still applies to that inherited material.
// Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
// If a copy of the MPL was not distributed with this file, You can obtain
// one at https://mozilla.org/MPL/2.0/.

package indexers

import "context"

// DatapackIndexer defines the interface for datapack discovery services. A
// datapack is a self-contained world-data bundle (recipes, loot tables,
// worldgen) that runs on Vanilla servers without a mod loader, so it is a
// separate content type from mods and modpacks.
type DatapackIndexer interface {
	// SearchDatapacks searches for datapacks matching the criteria.
	SearchDatapacks(ctx context.Context, query string, gameVersion string, offset, limit int) (*DatapackSearchResult, error)

	// GetDatapackLatestFile returns the primary downloadable file for a
	// datapack, optionally filtered to a game version.
	GetDatapackLatestFile(ctx context.Context, datapackID string, gameVersion string) (*DatapackFile, error)

	// GetIndexerName returns the name of this indexer (e.g. "modrinth").
	GetIndexerName() string
}

// OriginReporter is an optional DatapackIndexer capability: it reports the
// configured API origin. Downloads inherit this origin's network trust (see
// minecraft.DownloadAllowsPrivate), so a public index can never point the
// panel at internal hosts while self-hosted indexes keep serving LAN files.
// Indexers that do not report an origin get strict public-only downloads.
type OriginReporter interface {
	Origin() string
}

// DatapackIndexerOrigin returns the indexer's configured API origin, or ""
// when the indexer does not report one.
func DatapackIndexerOrigin(idx DatapackIndexer) string {
	if r, ok := idx.(OriginReporter); ok {
		return r.Origin()
	}
	return ""
}

// DatapackSearchResult holds a page of datapack search results.
type DatapackSearchResult struct {
	Datapacks  []Datapack
	TotalCount int
	PageSize   int
	Offset     int
}

// Datapack is a datapack as reported by an indexer.
type Datapack struct {
	ID            string   `json:"id"`
	IndexerID     string   `json:"indexer_id"`
	Indexer       string   `json:"indexer"`
	Name          string   `json:"name"`
	Slug          string   `json:"slug"`
	Summary       string   `json:"summary"`
	IconURL       string   `json:"icon_url"`
	WebsiteURL    string   `json:"website_url"`
	Author        string   `json:"author"`
	DownloadCount int64    `json:"download_count"`
	GameVersions  []string `json:"game_versions"`
	DateModified  string   `json:"date_modified"`
}

// DatapackFile is a downloadable datapack artifact.
type DatapackFile struct {
	DatapackID   string   `json:"datapack_id"`
	Filename     string   `json:"filename"`
	DownloadURL  string   `json:"download_url"`
	Size         int64    `json:"size"`
	SHA1         string   `json:"sha1"`
	SHA512       string   `json:"sha512"`
	GameVersions []string `json:"game_versions"`
}
