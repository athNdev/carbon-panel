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
