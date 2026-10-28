// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package staging

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/nodestorage"
	"github.com/google/uuid"
)

const stagedContentRoot = ".carbon-panel/staged"

// StageModpackTree copies a fetched pack into the open batch. The live pack
// and the container are left as they are. A later window copies the tree.
func StageModpackTree(ctx context.Context, store *storage.Store, server *storage.Server, srcDir, summary string) (*storage.StagedContentChange, error) {
	if store == nil || server == nil || server.ID == "" || server.DataPath == "" || srcDir == "" {
		return nil, fmt.Errorf("modpack stage requires a store, a server, and a source tree")
	}
	acc, err := openAccessor(ctx, store, server)
	if err != nil {
		return nil, err
	}
	defer func() { _ = acc.Close() }()

	id := uuid.NewString()
	stageRoot := stagedContentRoot + "/" + id + "/tree"
	files := 0
	walkErr := filepath.Walk(srcDir, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcDir, current)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("modpack stage refuses symlink %s", rel)
		}
		parts := strings.Split(rel, "/")
		if parts[0] == ".git" {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		live, err := modpackLivePath(rel)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		if err := acc.WriteFile(stageRoot+"/"+live, data); err != nil {
			return err
		}
		files++
		return nil
	})
	if walkErr != nil || files == 0 {
		_ = acc.RemoveAll(stagedContentRoot + "/" + id)
		if walkErr != nil {
			return nil, walkErr
		}
		return nil, fmt.Errorf("modpack tree is empty")
	}
	if summary == "" {
		summary = "modpack"
	}
	row, err := store.AddStagedContent(ctx, &storage.StagedContentChange{
		ID:         id,
		ServerID:   server.ID,
		Kind:       storage.StagedKindModpack,
		Action:     storage.StagedActionReplace,
		TargetPath: "modpack",
		StagePath:  stageRoot,
		FileName:   summary,
	})
	if err != nil {
		_ = acc.RemoveAll(stagedContentRoot + "/" + id)
		return nil, err
	}
	return row, nil
}

func modpackLivePath(rel string) (string, error) {
	rel = path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("modpack path %q is not inside the pack", rel)
	}
	top := strings.SplitN(rel, "/", 2)[0]
	switch top {
	case ".git", ".carbon-panel", ".carbon-panel_modpack_git", ".carbon-panel_modpack_staged", ".carbon-panel_snapshots":
		return "", fmt.Errorf("modpack path %q is reserved", rel)
	}
	return rel, nil
}

type modpackFile struct {
	stage string
	live  string
}

func modpackFiles(acc nodestorage.Accessor, stageRoot string) ([]modpackFile, error) {
	stageRoot = path.Clean(stageRoot)
	var files []modpackFile
	var walk func(string) error
	walk = func(dir string) error {
		entries, err := acc.ReadDir(dir)
		if err != nil {
			return err
		}
		names := make([]string, 0, len(entries))
		byName := make(map[string]os.DirEntry, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
			byName[entry.Name()] = entry
		}
		sort.Strings(names)
		for _, name := range names {
			if name == "" || name == "." || name == ".." {
				continue
			}
			entry := byName[name]
			child := dir + "/" + name
			if entry.IsDir() {
				if err := walk(child); err != nil {
					return err
				}
				continue
			}
			rel := strings.TrimPrefix(child, stageRoot+"/")
			live, err := modpackLivePath(rel)
			if err != nil {
				return err
			}
			files = append(files, modpackFile{stage: child, live: live})
		}
		return nil
	}
	if err := walk(stageRoot); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("staged modpack is empty")
	}
	return files, nil
}

func snapshotModpack(acc nodestorage.Accessor, row storage.StagedContentChange) ([]fileSnap, error) {
	files, err := modpackFiles(acc, row.StagePath)
	if err != nil {
		return nil, err
	}
	snaps := make([]fileSnap, 0, len(files))
	for _, file := range files {
		info, statErr := acc.Stat(file.live)
		if statErr != nil && !os.IsNotExist(statErr) {
			return snaps, statErr
		}
		if statErr == nil && info.IsDir() {
			return snaps, fmt.Errorf("staged modpack path %s is a directory", file.live)
		}
		item := fileSnap{target: file.live}
		if statErr == nil {
			item.existed = true
			item.data, err = acc.ReadFile(file.live)
			if err != nil {
				return snaps, err
			}
		}
		snaps = append(snaps, item)
	}
	return snaps, nil
}

func writeModpack(acc nodestorage.Accessor, row storage.StagedContentChange) error {
	files, err := modpackFiles(acc, row.StagePath)
	if err != nil {
		return err
	}
	for _, file := range files {
		data, err := acc.ReadFile(file.stage)
		if err != nil {
			return err
		}
		if err := acc.WriteFile(file.live, data); err != nil {
			return err
		}
	}
	return nil
}
