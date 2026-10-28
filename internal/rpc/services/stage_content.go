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
	"path"
	"strings"

	"connectrpc.com/connect"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/nodestorage"
	"github.com/google/uuid"
)

// stagedContentRoot is outside mods, datapacks, and the world. Apply (a later
// change) is what copies these bytes onto the live tree.
const stagedContentRoot = ".carbon-panel/staged"

var errStagedPath = errors.New("invalid staged path")

func safeStageTarget(rel string) (string, error) {
	rel = strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, ":") {
		return "", fmt.Errorf("%w: %q", errStagedPath, rel)
	}
	clean := path.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%w: %q", errStagedPath, rel)
	}
	if clean == stagedContentRoot || strings.HasPrefix(clean, stagedContentRoot+"/") {
		return "", fmt.Errorf("%w: %q", errStagedPath, rel)
	}
	return clean, nil
}

func stagedFileBase(name string) (string, error) {
	base := path.Base(strings.ReplaceAll(name, "\\", "/"))
	if base == "" || base == "." || base == ".." {
		return "", fmt.Errorf("%w: file name %q", errStagedPath, name)
	}
	return base, nil
}

func serverAccessor(ctx context.Context, store *storage.Store, server *storage.Server) (nodestorage.Accessor, error) {
	if _, err := dataRootFor(ctx, store, server, true); err != nil {
		return nil, err
	}
	info, err := storageNodeInfo(ctx, store, server.NodeID)
	if err != nil {
		return nil, err
	}
	return nodestorage.ForNode(info, server.DataPath)
}

// stageContentBytes records an install, replace, or delete on the open batch.
// Install and replace bytes are written through nodestorage under
// .carbon-panel/staged. A delete records the live path and writes nothing.
// The server's mods directory, datapacks directory, and world are not modified.
func stageContentBytes(ctx context.Context, store *storage.Store, server *storage.Server, kind, action, targetRel, fileName string, data []byte) (*storage.StagedContentChange, error) {
	target, err := safeStageTarget(targetRel)
	if err != nil {
		return nil, err
	}
	base, err := stagedFileBase(fileName)
	if err != nil {
		return nil, err
	}
	acc, err := serverAccessor(ctx, store, server)
	if err != nil {
		return nil, err
	}
	defer func() { _ = acc.Close() }()

	id := uuid.New().String()
	stagePath := ""
	if action != storage.StagedActionDelete {
		stagePath = stagedContentRoot + "/" + id + "/" + base
		if err := acc.WriteFile(stagePath, data); err != nil {
			return nil, err
		}
	}
	row, err := store.AddStagedContent(ctx, &storage.StagedContentChange{
		ID:         id,
		ServerID:   server.ID,
		Kind:       kind,
		Action:     action,
		TargetPath: target,
		StagePath:  stagePath,
		FileName:   base,
	})
	if err != nil {
		if stagePath != "" {
			_ = acc.RemoveAll(stagedContentRoot + "/" + id)
		}
		return nil, err
	}
	return row, nil
}

// discardStagedContent deletes staged bytes, then marks the row discarded.
// The live tree is left as it is. A delete item has no bytes to remove.
func discardStagedContent(ctx context.Context, store *storage.Store, server *storage.Server, id string) error {
	row, err := store.GetStagedContent(ctx, id)
	if err != nil {
		return err
	}
	if row.ServerID != server.ID {
		return fmt.Errorf("staged content %s is not on this server", id)
	}
	if row.StagePath != "" {
		acc, err := serverAccessor(ctx, store, server)
		if err != nil {
			return err
		}
		defer func() { _ = acc.Close() }()
		if err := acc.RemoveAll(path.Dir(row.StagePath)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	_, err = store.DiscardStagedContent(ctx, id)
	return err
}

func stageContentError(err error) error {
	switch {
	case nodestorage.IsUnavailable(err):
		return connect.NewError(connect.CodeUnavailable, err)
	case nodestorage.IsEscape(err), errors.Is(err, errStagedPath):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}
