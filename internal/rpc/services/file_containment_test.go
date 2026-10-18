// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// TestFileServicePathContainment is the security regression test for the
// file-browser sandbox. The server data directory is the tenancy boundary, so a
// sibling directory whose name merely shares its prefix must not be reachable.
func TestFileServicePathContainment(t *testing.T) {
	base := t.TempDir()
	serverDir := filepath.Join(base, "servers", "abc")
	siblingDir := filepath.Join(base, "servers", "abc-evil")
	for _, d := range []string{serverDir, siblingDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(siblingDir, "secret.txt"), []byte("TOP SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(serverDir, "ok.txt"), []byte("fine"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := setupTestStore(t)
	defer func() { _ = store.Close() }()
	if err := store.CreateServer(context.Background(), &db.Server{
		ID: "abc", Name: "abc", ModLoader: db.ModLoaderVanilla, MCVersion: "1.21",
		Status: db.StatusStopped, DataPath: serverDir, Port: 25565,
	}); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	svc := NewFileService(store, nil, nil, nil, logger.New())
	t.Cleanup(svc.Stop)
	ctx := context.Background()

	rejected := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: expected rejection, got success", name)
		}
		if code := connect.CodeOf(err); code != connect.CodeInvalidArgument && code != connect.CodeNotFound {
			t.Errorf("%s: expected invalid_argument/not_found, got %v (%v)", name, code, err)
		}
	}

	t.Run("ListFiles sibling prefix rejected", func(t *testing.T) {
		_, err := svc.ListFiles(ctx, connect.NewRequest(&v1.ListFilesRequest{ServerId: "abc", Path: "../abc-evil"}))
		rejected("ListFiles", err)
	})

	t.Run("GetFile sibling prefix rejected", func(t *testing.T) {
		_, err := svc.GetFile(ctx, connect.NewRequest(&v1.GetFileRequest{ServerId: "abc", Path: "../abc-evil/secret.txt"}))
		rejected("GetFile", err)
	})

	t.Run("CreateFolder sibling prefix rejected", func(t *testing.T) {
		_, err := svc.CreateFolder(ctx, connect.NewRequest(&v1.CreateFolderRequest{ServerId: "abc", Path: "../abc-created"}))
		rejected("CreateFolder", err)
		if _, statErr := os.Stat(filepath.Join(base, "servers", "abc-created")); statErr == nil {
			t.Fatalf("CreateFolder escaped the server directory")
		}
	})

	t.Run("DeleteFile sibling prefix rejected", func(t *testing.T) {
		_, err := svc.DeleteFile(ctx, connect.NewRequest(&v1.DeleteFileRequest{ServerId: "abc", Path: "../abc-evil/secret.txt"}))
		rejected("DeleteFile", err)
		if _, statErr := os.Stat(filepath.Join(siblingDir, "secret.txt")); statErr != nil {
			t.Fatalf("DeleteFile removed a file outside the server directory")
		}
	})

	t.Run("deep escape rejected", func(t *testing.T) {
		_, err := svc.ListFiles(ctx, connect.NewRequest(&v1.ListFilesRequest{ServerId: "abc", Path: "../../../../etc"}))
		rejected("ListFiles deep escape", err)
	})

	t.Run("normal path still works", func(t *testing.T) {
		resp, err := svc.ListFiles(ctx, connect.NewRequest(&v1.ListFilesRequest{ServerId: "abc", Path: "."}))
		if err != nil {
			t.Fatalf("normal listing failed: %v", err)
		}
		found := false
		for _, f := range resp.Msg.Files {
			if f.Name == "ok.txt" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected ok.txt in directory listing, got %d entries", len(resp.Msg.Files))
		}
	})
}

// TestFileServiceSymlinkContainment verifies that symbolic links pointing
// outside the server data directory are strictly rejected from read, write,
// archive, and directory listing operations.
func TestFileServiceSymlinkContainment(t *testing.T) {
	base := t.TempDir()
	serverDir := filepath.Join(base, "servers", "srv1")
	outsideDir := filepath.Join(base, "outside")
	for _, d := range []string{serverDir, outsideDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	outsideSecret := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideSecret, []byte("SUPER_SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Symlink to outside file
	linkToFile := filepath.Join(serverDir, "link_to_secret")
	if err := os.Symlink(outsideSecret, linkToFile); err != nil {
		t.Fatal(err)
	}

	// Symlink to outside dir
	linkToDir := filepath.Join(serverDir, "link_to_outside")
	if err := os.Symlink(outsideDir, linkToDir); err != nil {
		t.Fatal(err)
	}

	// Inside file
	insideFile := filepath.Join(serverDir, "inside.txt")
	if err := os.WriteFile(insideFile, []byte("hello inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Symlink to inside file
	linkToInside := filepath.Join(serverDir, "link_to_inside")
	if err := os.Symlink(insideFile, linkToInside); err != nil {
		t.Fatal(err)
	}

	store := setupTestStore(t)
	defer func() { _ = store.Close() }()
	if err := store.CreateServer(context.Background(), &db.Server{
		ID: "srv1", Name: "srv1", ModLoader: db.ModLoaderVanilla, MCVersion: "1.21",
		Status: db.StatusStopped, DataPath: serverDir, Port: 25565,
	}); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	svc := NewFileService(store, nil, nil, nil, logger.New())
	t.Cleanup(svc.Stop)
	ctx := context.Background()

	// 1. GetFile via symlink pointing outside is rejected
	t.Run("GetFile via outside symlink rejected", func(t *testing.T) {
		_, err := svc.GetFile(ctx, connect.NewRequest(&v1.GetFileRequest{ServerId: "srv1", Path: "link_to_secret"}))
		if err == nil {
			t.Fatalf("expected error reading file through escaping symlink, got nil")
		}
		if code := connect.CodeOf(err); code != connect.CodeInvalidArgument {
			t.Errorf("expected InvalidArgument, got %v", code)
		}
	})

	// 2. GetFile via path inside outside symlink dir is rejected
	t.Run("GetFile via outside dir symlink rejected", func(t *testing.T) {
		_, err := svc.GetFile(ctx, connect.NewRequest(&v1.GetFileRequest{ServerId: "srv1", Path: "link_to_outside/secret.txt"}))
		if err == nil {
			t.Fatalf("expected error reading file through escaping dir symlink, got nil")
		}
	})

	// 3. ListFiles on outside dir symlink is rejected
	t.Run("ListFiles on outside dir symlink rejected", func(t *testing.T) {
		_, err := svc.ListFiles(ctx, connect.NewRequest(&v1.ListFilesRequest{ServerId: "srv1", Path: "link_to_outside"}))
		if err == nil {
			t.Fatalf("expected error listing through escaping symlink, got nil")
		}
	})

	// 4. UpdateFile writing through outside symlink is rejected
	t.Run("UpdateFile through outside symlink rejected", func(t *testing.T) {
		_, err := svc.UpdateFile(ctx, connect.NewRequest(&v1.UpdateFileRequest{
			ServerId: "srv1", Path: "link_to_secret", Content: []byte("PWNED"),
		}))
		if err == nil {
			t.Fatalf("expected error updating file through escaping symlink, got nil")
		}
		content, _ := os.ReadFile(outsideSecret)
		if string(content) != "SUPER_SECRET" {
			t.Fatalf("outside secret was modified: %s", string(content))
		}
	})

	// 5. UpdateFile writing through inside symlink is also rejected (no writing through symlinks)
	t.Run("UpdateFile through inside symlink rejected", func(t *testing.T) {
		_, err := svc.UpdateFile(ctx, connect.NewRequest(&v1.UpdateFileRequest{
			ServerId: "srv1", Path: "link_to_inside", Content: []byte("NEW_CONTENT"),
		}))
		if err == nil {
			t.Fatalf("expected error updating through symlink, got nil")
		}
		content, _ := os.ReadFile(insideFile)
		if string(content) != "hello inside" {
			t.Fatalf("inside file was modified through symlink: %s", string(content))
		}
	})

	// 6. DeleteFile on symlink does not delete outside target
	t.Run("DeleteFile on outside symlink does not delete outside target", func(t *testing.T) {
		_, _ = svc.DeleteFile(ctx, connect.NewRequest(&v1.DeleteFileRequest{
			ServerId: "srv1", Path: "link_to_secret",
		}))
		if _, statErr := os.Stat(outsideSecret); statErr != nil {
			t.Fatalf("outside secret was deleted: %v", statErr)
		}
	})

	// 7. CreateArchive on outside symlink is rejected
	t.Run("CreateArchive on outside symlink rejected", func(t *testing.T) {
		_, err := svc.CreateArchive(ctx, connect.NewRequest(&v1.CreateArchiveRequest{
			ServerId: "srv1", Paths: []string{"link_to_secret"},
		}))
		if err == nil {
			t.Fatalf("expected CreateArchive on escaping symlink to be rejected")
		}
	})

	// 8. listDirectory omits escaping symlinks
	t.Run("listDirectory omits escaping symlinks", func(t *testing.T) {
		resp, err := svc.ListFiles(ctx, connect.NewRequest(&v1.ListFilesRequest{
			ServerId: "srv1", Path: ".",
		}))
		if err != nil {
			t.Fatalf("ListFiles failed: %v", err)
		}
		for _, f := range resp.Msg.Files {
			if f.Name == "link_to_secret" || f.Name == "link_to_outside" {
				t.Fatalf("escaping symlink %s was not omitted from directory listing", f.Name)
			}
		}
	})
}
