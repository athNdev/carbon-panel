package db

import (
	"context"
	"testing"
)

// TestStore_UpdateServerContainerID verifies that updating only the
// ContainerID column does not clobber other fields that may have changed
// concurrently on a stale in-memory copy (the race MINE-110 fixes).
func TestStore_UpdateServerContainerID(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	server := &Server{
		ID:        "server-1",
		Name:      "Original Name",
		ModLoader: ModLoaderVanilla,
		MCVersion: "1.20.4",
		Status:    StatusStarting,
		Memory:    4096,
		DataPath:  "/tmp/server-1",
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Simulate a concurrent goroutine changing Status via the DB directly,
	// independent of our stale in-memory struct.
	if err := store.UpdateServerStatus(ctx, "server-1", StatusRunning); err != nil {
		t.Fatalf("failed to set status: %v", err)
	}

	// Now targeted-update just the ContainerID, as if from a container
	// recreate flow that never touched Status.
	if err := store.UpdateServerContainerID(ctx, "server-1", "abc123"); err != nil {
		t.Fatalf("failed to update container id: %v", err)
	}

	fetched, err := store.GetServer(ctx, "server-1")
	if err != nil {
		t.Fatalf("failed to get server: %v", err)
	}
	if fetched.ContainerID != "abc123" {
		t.Fatalf("expected container id abc123, got %q", fetched.ContainerID)
	}
	// Status set by the "concurrent" writer must survive untouched.
	if fetched.Status != StatusRunning {
		t.Fatalf("expected status to remain %q (untouched by ContainerID update), got %q", StatusRunning, fetched.Status)
	}
	// Name should also be untouched.
	if fetched.Name != "Original Name" {
		t.Fatalf("expected name to remain unchanged, got %q", fetched.Name)
	}
}

// TestStore_UpdateServerStatus verifies that updating only the Status column
// does not clobber other fields, e.g. a ContainerID set concurrently.
func TestStore_UpdateServerStatus(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	server := &Server{
		ID:        "server-2",
		Name:      "Second Server",
		ModLoader: ModLoaderVanilla,
		MCVersion: "1.20.4",
		Status:    StatusStopped,
		Memory:    2048,
		DataPath:  "/tmp/server-2",
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Simulate a concurrent goroutine setting ContainerID (e.g. a recreate
	// flow) independent of our stale in-memory struct.
	if err := store.UpdateServerContainerID(ctx, "server-2", "container-xyz"); err != nil {
		t.Fatalf("failed to set container id: %v", err)
	}

	// Now targeted-update just the Status, as if from a status monitor that
	// never touched ContainerID.
	if err := store.UpdateServerStatus(ctx, "server-2", StatusRunning); err != nil {
		t.Fatalf("failed to update status: %v", err)
	}

	fetched, err := store.GetServer(ctx, "server-2")
	if err != nil {
		t.Fatalf("failed to get server: %v", err)
	}
	if fetched.Status != StatusRunning {
		t.Fatalf("expected status running, got %q", fetched.Status)
	}
	// ContainerID set by the "concurrent" writer must survive untouched.
	if fetched.ContainerID != "container-xyz" {
		t.Fatalf("expected container id to remain %q (untouched by status update), got %q", "container-xyz", fetched.ContainerID)
	}
	if fetched.Name != "Second Server" {
		t.Fatalf("expected name to remain unchanged, got %q", fetched.Name)
	}
}

// TestStore_UpdateServerFields verifies the generic multi-column targeted
// update only touches the requested fields.
func TestStore_UpdateServerFields(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	server := &Server{
		ID:        "server-3",
		Name:      "Third Server",
		ModLoader: ModLoaderVanilla,
		MCVersion: "1.20.4",
		Status:    StatusStopped,
		Memory:    2048,
		DataPath:  "/tmp/server-3",
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if err := store.UpdateServerFields(ctx, "server-3", map[string]interface{}{
		"status":       StatusRunning,
		"container_id": "container-multi",
	}); err != nil {
		t.Fatalf("failed to update fields: %v", err)
	}

	fetched, err := store.GetServer(ctx, "server-3")
	if err != nil {
		t.Fatalf("failed to get server: %v", err)
	}
	if fetched.Status != StatusRunning {
		t.Fatalf("expected status running, got %q", fetched.Status)
	}
	if fetched.ContainerID != "container-multi" {
		t.Fatalf("expected container id container-multi, got %q", fetched.ContainerID)
	}
	if fetched.Name != "Third Server" {
		t.Fatalf("expected name to remain unchanged, got %q", fetched.Name)
	}
	if fetched.Memory != 2048 {
		t.Fatalf("expected memory to remain unchanged, got %d", fetched.Memory)
	}
}

// TestStore_UpdateModuleContainerID verifies that updating only the
// ContainerID column on a Module does not clobber other fields.
func TestStore_UpdateModuleContainerID(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	server := &Server{
		ID:        "server-4",
		Name:      "Fourth Server",
		ModLoader: ModLoaderVanilla,
		MCVersion: "1.20.4",
		Status:    StatusRunning,
		Memory:    2048,
		DataPath:  "/tmp/server-4",
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	module := &Module{
		ID:         "module-1",
		Name:       "Companion Module",
		ServerID:   "server-4",
		TemplateID: "template-1",
		Status:     ModuleStatusStopped,
		Memory:     512,
		DataPath:   "/tmp/module-1",
	}
	if err := store.CreateModule(ctx, module); err != nil {
		t.Fatalf("failed to create module: %v", err)
	}

	if err := store.UpdateModuleContainerID(ctx, "module-1", "module-container-abc"); err != nil {
		t.Fatalf("failed to update module container id: %v", err)
	}

	fetched, err := store.GetModule(ctx, "module-1")
	if err != nil {
		t.Fatalf("failed to get module: %v", err)
	}
	if fetched.ContainerID != "module-container-abc" {
		t.Fatalf("expected container id module-container-abc, got %q", fetched.ContainerID)
	}
	if fetched.Name != "Companion Module" {
		t.Fatalf("expected name to remain unchanged, got %q", fetched.Name)
	}
	if fetched.Status != ModuleStatusStopped {
		t.Fatalf("expected status to remain unchanged, got %q", fetched.Status)
	}
}
