package docker

import (
	"context"
	"errors"
	"testing"

	db "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// TestDecideReconcile exercises the pure adopt/orphan/skip decision in isolation, since this is
// the logic that ultimately decides whether a container survives startup reconciliation.
func TestDecideReconcile(t *testing.T) {
	tests := []struct {
		name           string
		cont           managedContainer
		dbRecordExists bool
		dbContainerID  string
		want           reconcileAction
	}{
		{
			name:           "matching server container is left alone",
			cont:           managedContainer{ID: "abc123", ServerID: "srv-1"},
			dbRecordExists: true,
			dbContainerID:  "abc123",
			want:           actionNone,
		},
		{
			name:           "server record with stale ContainerID is adopted, not purged",
			cont:           managedContainer{ID: "abc123", ServerID: "srv-1"},
			dbRecordExists: true,
			dbContainerID:  "old-stale-id",
			want:           actionAdopt,
		},
		{
			name:           "server record with empty ContainerID (crash mid-create) is adopted",
			cont:           managedContainer{ID: "abc123", ServerID: "srv-1"},
			dbRecordExists: true,
			dbContainerID:  "",
			want:           actionAdopt,
		},
		{
			name:           "no DB record at all is a genuine orphan",
			cont:           managedContainer{ID: "abc123", ServerID: "srv-missing"},
			dbRecordExists: false,
			dbContainerID:  "",
			want:           actionOrphan,
		},
		{
			name:           "matching module container is left alone",
			cont:           managedContainer{ID: "def456", ModuleID: "mod-1"},
			dbRecordExists: true,
			dbContainerID:  "def456",
			want:           actionNone,
		},
		{
			name:           "module record with drifted ContainerID is adopted",
			cont:           managedContainer{ID: "def456", ModuleID: "mod-1"},
			dbRecordExists: true,
			dbContainerID:  "other-id",
			want:           actionAdopt,
		},
		{
			name:           "orphan module container with no DB record",
			cont:           managedContainer{ID: "def456", ModuleID: "mod-missing"},
			dbRecordExists: false,
			dbContainerID:  "",
			want:           actionOrphan,
		},
		{
			name:           "managed container without a recognizable id label is skipped, never orphaned",
			cont:           managedContainer{ID: "ghi789"},
			dbRecordExists: false,
			dbContainerID:  "",
			want:           actionSkip,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decideReconcile(tt.cont, tt.dbRecordExists, tt.dbContainerID)
			if got != tt.want {
				t.Errorf("decideReconcile(%+v, %v, %q) = %v, want %v", tt.cont, tt.dbRecordExists, tt.dbContainerID, got, tt.want)
			}
		})
	}
}

// mockReconcileStore implements ReconcileStore for testing the fail-safe boot guard without a
// real database or a live Docker daemon.
type mockReconcileStore struct {
	servers    []*db.Server
	serversErr error
	modules    []*db.Module
	modulesErr error

	updateServerCalls int
	updateModuleCalls int
}

func (m *mockReconcileStore) ListServers(ctx context.Context) ([]*db.Server, error) {
	return m.servers, m.serversErr
}

func (m *mockReconcileStore) UpdateServer(ctx context.Context, server *db.Server) error {
	m.updateServerCalls++
	return nil
}

func (m *mockReconcileStore) ListModules(ctx context.Context) ([]*db.Module, error) {
	return m.modules, m.modulesErr
}

func (m *mockReconcileStore) UpdateModule(ctx context.Context, module *db.Module) error {
	m.updateModuleCalls++
	return nil
}

// TestReconcileAndCleanupContainers_AbortsOnListServersError verifies the fail-safe boot guard:
// if the DB can't be read, reconciliation must abort before it ever reaches Docker, since an
// empty/error'd server list would otherwise make every managed container look orphaned. A nil
// *Client is passed deliberately — if the guard is ever removed or reordered, this test panics
// instead of silently passing, which is exactly the failure mode we want to catch.
func TestReconcileAndCleanupContainers_AbortsOnListServersError(t *testing.T) {
	store := &mockReconcileStore{serversErr: errors.New("db is on fire")}
	log := logger.New()

	err := ReconcileAndCleanupContainers(context.Background(), store, nil, log)
	if err == nil {
		t.Fatal("expected ReconcileAndCleanupContainers to return an error when ListServers fails, got nil")
	}
	if store.updateServerCalls != 0 || store.updateModuleCalls != 0 {
		t.Fatal("expected no adoption writes when the boot guard aborts")
	}
}

// TestReconcileAndCleanupContainers_AbortsOnListModulesError mirrors the above for the modules
// list, which is fetched right after servers and before any Docker call is made.
func TestReconcileAndCleanupContainers_AbortsOnListModulesError(t *testing.T) {
	store := &mockReconcileStore{modulesErr: errors.New("db is on fire")}
	log := logger.New()

	err := ReconcileAndCleanupContainers(context.Background(), store, nil, log)
	if err == nil {
		t.Fatal("expected ReconcileAndCleanupContainers to return an error when ListModules fails, got nil")
	}
	if store.updateServerCalls != 0 || store.updateModuleCalls != 0 {
		t.Fatal("expected no adoption writes when the boot guard aborts")
	}
}
