package portalloc

import (
	"context"
	"testing"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	store, err := db.Open(db.Options{Driver: "sqlite", DSN: ":memory:", AutoMigrate: true})
	require.NoError(t, err)
	return store.DB()
}

func TestPortAllocator(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()
	alloc := NewAllocator(25565, 25568) // Small range for testing: 25565, 25566, 25567, 25568

	nodeID := uuid.NewString()

	// 1. First allocation: dynamic should yield 25565
	port1, err := alloc.Allocate(ctx, database, nodeID, 0, "")
	require.NoError(t, err)
	require.Equal(t, 25565, port1)

	// Save workload 1
	w1 := &db.Workload{
		TenantBase: db.TenantBase{ID: "w-1", OrgID: "org-1"},
		NodeID:     nodeID,
		Name:       "server-1",
		Status:     "running",
		HostPort:   port1,
	}
	require.NoError(t, database.Create(w1).Error)

	// 2. Second allocation: dynamic should yield 25566
	port2, err := alloc.Allocate(ctx, database, nodeID, 0, "")
	require.NoError(t, err)
	require.Equal(t, 25566, port2)

	w2 := &db.Workload{
		TenantBase: db.TenantBase{ID: "w-2", OrgID: "org-1"},
		NodeID:     nodeID,
		Name:       "server-2",
		Status:     "running",
		HostPort:   port2,
	}
	require.NoError(t, database.Create(w2).Error)

	// 3. Collision check: requesting port 25565 explicitly should fail with ErrPortConflict
	_, err = alloc.Allocate(ctx, database, nodeID, 25565, "")
	require.ErrorIs(t, err, ErrPortConflict)

	// 4. Update self check: w-1 updating should be allowed to keep 25565
	rePort1, err := alloc.Allocate(ctx, database, nodeID, 25565, "w-1")
	require.NoError(t, err)
	require.Equal(t, 25565, rePort1)

	// 5. Fill remaining ports
	w3 := &db.Workload{
		TenantBase: db.TenantBase{ID: "w-3", OrgID: "org-1"},
		NodeID:     nodeID,
		Name:       "server-3",
		Status:     "running",
		HostPort:   25567,
	}
	require.NoError(t, database.Create(w3).Error)

	w4 := &db.Workload{
		TenantBase: db.TenantBase{ID: "w-4", OrgID: "org-1"},
		NodeID:     nodeID,
		Name:       "server-4",
		Status:     "running",
		HostPort:   25568,
	}
	require.NoError(t, database.Create(w4).Error)

	// Pool is now exhausted (25565..25568 occupied)
	_, err = alloc.Allocate(ctx, database, nodeID, 0, "")
	require.ErrorIs(t, err, ErrNoPortsAvailable)

	// 6. Delete/terminate w2: frees 25566
	require.NoError(t, database.Model(&db.Workload{}).Where("id = ?", "w-2").Update("status", "terminated").Error)

	portFreed, err := alloc.Allocate(ctx, database, nodeID, 0, "")
	require.NoError(t, err)
	require.Equal(t, 25566, portFreed)

	// 7. Verify GetAllocations
	allocs, err := alloc.GetAllocations(ctx, database, nodeID)
	require.NoError(t, err)
	require.Len(t, allocs, 3) // w1 (25565), w3 (25567), w4 (25568)
}

func TestFormatSRVRecord(t *testing.T) {
	srv := FormatSRVRecord("play.carbon.local", "node1.carbon.cloud", 25567)
	require.Equal(t, "_minecraft._tcp.play.carbon.local. 3600 IN SRV 0 5 25567 node1.carbon.cloud.", srv)

	require.Empty(t, FormatSRVRecord("", "node1.carbon.cloud", 25567))
	require.Empty(t, FormatSRVRecord("play.carbon.local", "", 25567))
	require.Empty(t, FormatSRVRecord("play.carbon.local", "node1.carbon.cloud", 0))
}
