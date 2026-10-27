// SPDX-License-Identifier: MIT AND MPL-2.0
// This file includes DiscoPanel material under the MIT License,
// Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
// and still applies to that inherited material.
// Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
// If a copy of the MPL was not distributed with this file, You can obtain
// one at https://mozilla.org/MPL/2.0/.

package indexers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/athNdev/carbon-panel/internal/config"
)

// ModpackIndexer defines the interface for modpack indexing services
type ModpackIndexer interface {
	// SearchModpacks searches for modpacks with the given criteria
	SearchModpacks(ctx context.Context, query string, gameVersion string, modLoader string, offset, limit int) (*SearchResult, error)

	// GetModpack retrieves detailed information about a specific modpack
	GetModpack(ctx context.Context, modpackID string) (*Modpack, error)

	// GetModpackFiles retrieves all available files for a modpack
	GetModpackFiles(ctx context.Context, modpackID string) ([]ModpackFile, error)

	// GetIndexerName returns the name of this indexer (e.g., "fuego", "modrinth")
	GetIndexerName() string
}

// SearchResult contains the results of a modpack search
type SearchResult struct {
	Modpacks   []Modpack
	TotalCount int
	PageSize   int
	Offset     int
}

// Modpack represents a modpack from any indexer
type Modpack struct {
	ID            string    `json:"id"`
	IndexerID     string    `json:"indexer_id"` // Original ID from the indexer
	Indexer       string    `json:"indexer"`    // Which indexer this came from
	Name          string    `json:"name"`
	Slug          string    `json:"slug"`
	Summary       string    `json:"summary"`
	Description   string    `json:"description"`
	LogoURL       string    `json:"logo_url"`
	WebsiteURL    string    `json:"website_url"`
	DownloadCount int64     `json:"download_count"`
	Categories    []string  `json:"categories"`
	GameVersions  []string  `json:"game_versions"`
	ModLoaders    []string  `json:"mod_loaders"`
	LatestFileID  string    `json:"latest_file_id"`
	DateCreated   time.Time `json:"date_created"`
	DateModified  time.Time `json:"date_modified"`
	DateReleased  time.Time `json:"date_released"`
}

// ModpackFile represents a downloadable file for a modpack
type ModpackFile struct {
	ID               string    `json:"id"`
	ModpackID        string    `json:"modpack_id"`
	DisplayName      string    `json:"display_name"`
	FileName         string    `json:"file_name"`
	FileDate         time.Time `json:"file_date"`
	FileLength       int64     `json:"file_length"`
	ReleaseType      string    `json:"release_type"` // "release", "beta", "alpha"
	DownloadURL      string    `json:"download_url"`
	GameVersions     []string  `json:"game_versions"`
	ModLoader        string    `json:"mod_loader"`
	ServerPackFileID *string   `json:"server_pack_file_id,omitempty"`
	SortIndex        int       `json:"sort_index"`
	VersionNumber    string    `json:"version_number"` // Human-readable version for Modrinth
}

// IndexerFactory creates a ModpackIndexer from an API key and config.
type IndexerFactory func(apiKey string, cfg *config.Config) ModpackIndexer

var (
	registryMu sync.RWMutex
	registry   = make(map[string]IndexerFactory)
)

// RegisterIndexer registers an IndexerFactory under the given name.
// Typically called from an indexer package's init() function.
func RegisterIndexer(name string, factory IndexerFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = factory
}

// NewIndexer creates a ModpackIndexer by name using the factory registry.
func NewIndexer(name string, apiKey string, cfg *config.Config) (ModpackIndexer, error) {
	registryMu.RLock()
	factory, ok := registry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown indexer: %s", name)
	}
	return factory(apiKey, cfg), nil
}

// DatapackIndexerFactory creates a DatapackIndexer from an API key and config.
type DatapackIndexerFactory func(apiKey string, cfg *config.Config) DatapackIndexer

var (
	datapackRegistryMu sync.RWMutex
	datapackRegistry   = make(map[string]DatapackIndexerFactory)
)

// RegisterDatapackIndexer registers a DatapackIndexerFactory under the given name.
// Typically called from an indexer package's init() function.
func RegisterDatapackIndexer(name string, factory DatapackIndexerFactory) {
	datapackRegistryMu.Lock()
	defer datapackRegistryMu.Unlock()
	datapackRegistry[name] = factory
}

// normalizeDatapackIndexerName maps provider aliases to their canonical
// registry name so callers can use one vocabulary across content types
// ("fuego" is the CurseForge adapter's established name on the modpack side).
func normalizeDatapackIndexerName(name string) string {
	if strings.EqualFold(name, "fuego") {
		return "curseforge"
	}
	return name
}

// NewDatapackIndexer creates a DatapackIndexer by name using the factory registry.
func NewDatapackIndexer(name string, apiKey string, cfg *config.Config) (DatapackIndexer, error) {
	name = normalizeDatapackIndexerName(name)
	datapackRegistryMu.RLock()
	factory, ok := datapackRegistry[name]
	datapackRegistryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown datapack indexer: %s", name)
	}
	return factory(apiKey, cfg), nil
}

// DatapackIndexerNames returns the registered datapack indexer names,
// sorted, so callers can search every provider for merged coverage.
func DatapackIndexerNames() []string {
	datapackRegistryMu.RLock()
	defer datapackRegistryMu.RUnlock()
	names := make([]string, 0, len(datapackRegistry))
	for name := range datapackRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// DatapackIndexersFor builds the datapack indexer set for file resolution: the
// named indexer, or every registered provider when name is empty so search
// results from any provider remain installable.
func DatapackIndexersFor(name, apiKey string, cfg *config.Config) ([]DatapackIndexer, error) {
	if strings.TrimSpace(name) != "" {
		idx, err := NewDatapackIndexer(name, apiKey, cfg)
		if err != nil {
			return nil, err
		}
		return []DatapackIndexer{idx}, nil
	}
	var idxs []DatapackIndexer
	for _, n := range DatapackIndexerNames() {
		if idx, err := NewDatapackIndexer(n, apiKey, cfg); err == nil {
			idxs = append(idxs, idx)
		}
	}
	if len(idxs) == 0 {
		return nil, fmt.Errorf("no datapack providers available")
	}
	return idxs, nil
}

// FirstDatapackLatestFile resolves a datapack file against the given indexers
// in order and returns the first success with the indexer that resolved it (for
// download trust scoping). An id from one provider must not be resolvable on
// another, so cross-provider misses are expected and joined into the final
// error.
func FirstDatapackLatestFile(ctx context.Context, datapackID, gameVersion string, idxs []DatapackIndexer) (*DatapackFile, DatapackIndexer, error) {
	var errs []error
	for _, idx := range idxs {
		f, err := idx.GetDatapackLatestFile(ctx, datapackID, gameVersion)
		if err == nil {
			return f, idx, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", idx.GetIndexerName(), err))
	}
	return nil, nil, fmt.Errorf("no datapack file found for %s: %w", datapackID, errors.Join(errs...))
}
