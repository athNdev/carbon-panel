// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/athNdev/carbon-panel/internal/indexers"
	"github.com/athNdev/carbon-panel/internal/minecraft"
	"github.com/athNdev/carbon-panel/pkg/utils"
)

// handleAddDatapack resolves a datapack from a search index (e.g. Modrinth),
// downloads it, and installs it into the pack's datapacks override directory.
// The manager validates the archive (and repairs wrapper-folder packaging)
// before it lands, so a broken download can never enter the pack.
func (h *PackwizHandler) handleAddDatapack(w http.ResponseWriter, r *http.Request, packID string) {
	var req struct {
		DatapackID  string `json:"datapack_id"`
		GameVersion string `json:"game_version"`
		FileName    string `json:"file_name"`
		Indexer     string `json:"indexer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.DatapackID) == "" {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// Fail fast on a bad override name before doing any network work.
	if name := strings.TrimSpace(req.FileName); name != "" {
		if err := minecraft.ValidateDatapackFilename(name); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Resolve from the provider that produced the id (empty: try every one).
	apiKey := ""
	if h.store != nil {
		if globalSettings, _, err := h.store.GetGlobalSettings(r.Context()); err == nil && globalSettings != nil && globalSettings.CFAPIKey != nil {
			apiKey = *globalSettings.CFAPIKey
		}
	}
	idxs, err := indexers.DatapackIndexersFor(req.Indexer, apiKey, h.config)
	if err != nil {
		http.Error(w, fmt.Sprintf("datapack index unavailable: %v", err), http.StatusBadGateway)
		return
	}
	file, resolved, err := indexers.FirstDatapackLatestFile(r.Context(), req.DatapackID, req.GameVersion, idxs)
	if err != nil {
		h.log.Error("Failed to resolve studio datapack %s: %v", req.DatapackID, err)
		http.Error(w, fmt.Sprintf("could not resolve datapack file: %v", err), http.StatusBadGateway)
		return
	}

	name := strings.TrimSpace(req.FileName)
	if name == "" {
		name = filepath.Base(file.Filename)
	}

	client := utils.NewSafeHTTPClient(5*time.Minute, minecraft.DownloadAllowsPrivate(indexers.DatapackIndexerOrigin(resolved)))
	dlReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, file.DownloadURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid download URL: %v", err), http.StatusBadGateway)
		return
	}
	dlReq.Header.Set("User-Agent", "CarbonPanel/2.x (Datapack Installer)")

	resp, err := client.Do(dlReq)
	if err != nil {
		h.log.Error("Failed to download studio datapack %s: %v", file.DownloadURL, err)
		http.Error(w, fmt.Sprintf("download failed: %v", err), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		http.Error(w, fmt.Sprintf("download failed: remote returned status %d", resp.StatusCode), http.StatusBadGateway)
		return
	}

	if err := h.manager.AddDatapackFile(packID, name, resp.Body); err != nil {
		h.log.Error("Failed to install studio datapack %s into %s: %v", name, packID, err)
		// Most failures here come from the downloaded archive itself (invalid
		// name or not a real datapack), so surface them as bad requests.
		http.Error(w, fmt.Sprintf("failed to install datapack: %v", err), http.StatusBadRequest)
		return
	}

	h.log.Info("Installed studio datapack %s into pack %s", name, packID)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "file_name": name})
}
