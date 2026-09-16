package db

import (
	"context"
	"testing"
)

// TestStore_GetServerByContainerID exercises the reverse lookup used by identity-resolution
// recovery (MINE-103) to find which server owns a given Docker container ID.
func TestStore_GetServerByContainerID(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	server := &Server{
		ID:          "server-lookup-1",
		Name:        "Lookup Test Server",
		ModLoader:   ModLoaderVanilla,
		MCVersion:   "1.20.4",
		NodeID:      "default",
		Status:      StatusStopped,
		Memory:      4096,
		DataPath:    "/tmp/server-lookup-1",
		ContainerID: "abc123containerid",
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	fetched, err := store.GetServerByContainerID(ctx, "abc123containerid")
	if err != nil {
		t.Fatalf("GetServerByContainerID failed: %v", err)
	}
	if fetched.ID != server.ID {
		t.Fatalf("expected server %s, got %s", server.ID, fetched.ID)
	}

	if _, err := store.GetServerByContainerID(ctx, "does-not-exist"); err == nil {
		t.Fatalf("expected error looking up unknown container id, got nil")
	}
}
