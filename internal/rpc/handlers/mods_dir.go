// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package handlers

import (
	"context"
	"fmt"
	"os"
	"strings"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/minecraft"
	"github.com/athNdev/carbon-panel/internal/nodestorage"
)

// modsDirectory is the mods folder on the node's mapped storage. A remote
// node with no reachable storage returns ErrUnavailable. An empty string
// means this loader has no mods directory.
func modsDirectory(ctx context.Context, store *storage.Store, server *storage.Server, write bool) (string, error) {
	if server == nil {
		return "", fmt.Errorf("server is required")
	}
	root := server.DataPath
	if !nodestorage.IsLegacyLocalNodeID(server.NodeID) {
		if store == nil {
			return "", fmt.Errorf("store is required to resolve node storage")
		}
		node, err := store.GetNode(ctx, server.NodeID)
		if err != nil {
			return "", fmt.Errorf("resolve storage node %q: %w", server.NodeID, err)
		}
		if write {
			root, err = nodestorage.EnsureDataRoot(node.StorageInfo(), server.DataPath)
		} else {
			root, err = nodestorage.MapDataPath(node.StorageInfo(), server.DataPath)
		}
		if err != nil {
			return "", err
		}
	}
	return minecraft.GetModsPath(root, server.ModLoader), nil
}

// installedModJars lists jar names already present for a server. An
// unreachable remote node yields an empty set so a controller copy of the
// data path is not reported as installed.
func installedModJars(ctx context.Context, store *storage.Store, server *storage.Server) map[string]bool {
	installed := map[string]bool{}
	modsDir, err := modsDirectory(ctx, store, server, false)
	if err != nil || modsDir == "" {
		return installed
	}
	files, err := os.ReadDir(modsDir)
	if err != nil {
		return installed
	}
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".jar") {
			installed[strings.ToLower(f.Name())] = true
		}
	}
	return installed
}
