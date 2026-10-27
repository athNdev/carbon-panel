// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"fmt"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/nodestorage"
)

// storageNodeInfo resolves the node-aware storage view of a server's owning
// node (ADR 0005). Legacy single-node aliases name the controller's own
// daemon; any other ID must resolve to a node row so the storage location is
// never guessed.
func storageNodeInfo(ctx context.Context, store *storage.Store, nodeID string) (nodestorage.NodeInfo, error) {
	if nodestorage.IsLegacyLocalNodeID(nodeID) {
		return nodestorage.LocalNode(), nil
	}
	node, err := store.GetNode(ctx, nodeID)
	if err != nil {
		return nodestorage.NodeInfo{}, fmt.Errorf("resolve storage node %q: %w", nodeID, err)
	}
	return node.StorageInfo(), nil
}
