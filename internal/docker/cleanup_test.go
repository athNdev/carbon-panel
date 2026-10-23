// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"errors"
	"testing"

	db "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// TestPlanReconcile exercises the pure adopt/orphan/quarantine/foreign/skip decision in
// isolation, since this is the logic that ultimately decides whether a container survives
// startup reconciliation.
func TestPlanReconcile(t *testing.T) {
	const ours = "installation-a"

	tests := []struct {
		name           string
		cont           managedContainer
		ourID          string
		dbRecordExists bool
		dbContainerID  string
		purgeOrphans   bool
		want           reconcileAction
	}{
		{
			name:           "matching owned server container is left alone",
			cont:           managedContainer{ID: "abc123", ServerID: "srv-1", InstallationID: ours},
			ourID:          ours,
			dbRecordExists: true,
			dbContainerID:  "abc123",
			want:           actionNone,
		},
		{
			name:           "owned server record with stale ContainerID is adopted, not purged",
			cont:           managedContainer{ID: "abc123", ServerID: "srv-1", InstallationID: ours},
			ourID:          ours,
			dbRecordExists: true,
			dbContainerID:  "old-stale-id",
			want:           actionAdopt,
		},
		{
			name:           "owned server record with empty ContainerID (crash mid-create) is adopted",
			cont:           managedContainer{ID: "abc123", ServerID: "srv-1", InstallationID: ours},
			ourID:          ours,
			dbRecordExists: true,
			dbContainerID:  "",
			want:           actionAdopt,
		},
		{
			name:           "owned orphan stays quarantined when purge is disabled",
			cont:           managedContainer{ID: "abc123", ServerID: "srv-missing", InstallationID: ours},
			ourID:          ours,
			dbRecordExists: false,
			purgeOrphans:   false,
			want:           actionQuarantine,
		},
		{
			name:           "owned orphan is eligible for purge only when explicitly enabled",
			cont:           managedContainer{ID: "abc123", ServerID: "srv-missing", InstallationID: ours},
			ourID:          ours,
			dbRecordExists: false,
			purgeOrphans:   true,
			want:           actionOrphan,
		},
		{
			name:           "legacy container matching a DB record is adopted (non-destructive)",
			cont:           managedContainer{ID: "abc123", ServerID: "srv-1"},
			ourID:          ours,
			dbRecordExists: true,
			dbContainerID:  "stale",
			want:           actionAdopt,
		},
		{
			name:           "legacy orphan is never purged, even with purge enabled",
			cont:           managedContainer{ID: "abc123", ServerID: "srv-missing"},
			ourID:          ours,
			dbRecordExists: false,
			purgeOrphans:   true,
			want:           actionQuarantine,
		},
		{
			name:           "foreign container matching a DB record is untouched",
			cont:           managedContainer{ID: "abc123", ServerID: "srv-1", InstallationID: "installation-b"},
			ourID:          ours,
			dbRecordExists: true,
			dbContainerID:  "abc123",
			want:           actionForeign,
		},
		{
			name:           "foreign orphan is never purged, even with purge enabled",
			cont:           managedContainer{ID: "abc123", ServerID: "srv-missing", InstallationID: "installation-b"},
			ourID:          ours,
			dbRecordExists: false,
			purgeOrphans:   true,
			want:           actionForeign,
		},
		{
			name:           "labeled container is foreign when this process has no identity",
			cont:           managedContainer{ID: "abc123", ServerID: "srv-1", InstallationID: "installation-b"},
			ourID:          "",
			dbRecordExists: true,
			dbContainerID:  "abc123",
			want:           actionForeign,
		},
		{
			name:           "owned module orphan with purge enabled is purged",
			cont:           managedContainer{ID: "def456", ModuleID: "mod-missing", InstallationID: ours},
			ourID:          ours,
			dbRecordExists: false,
			purgeOrphans:   true,
			want:           actionOrphan,
		},
		{
			name:           "matching module container is left alone",
			cont:           managedContainer{ID: "def456", ModuleID: "mod-1", InstallationID: ours},
			ourID:          ours,
			dbRecordExists: true,
			dbContainerID:  "def456",
			want:           actionNone,
		},
		{
			name:           "module record with drifted ContainerID is adopted",
			cont:           managedContainer{ID: "def456", ModuleID: "mod-1", InstallationID: ours},
			ourID:          ours,
			dbRecordExists: true,
			dbContainerID:  "other-id",
			want:           actionAdopt,
		},
		{
			name:           "managed container without a recognizable id label is skipped, never orphaned",
			cont:           managedContainer{ID: "ghi789"},
			ourID:          ours,
			dbRecordExists: false,
			purgeOrphans:   true,
			want:           actionSkip,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planReconcile(tt.cont, tt.ourID, tt.dbRecordExists, tt.dbContainerID, tt.purgeOrphans)
			if got != tt.want {
				t.Errorf("planReconcile(%+v, %q, %v, %q, %v) = %v, want %v", tt.cont, tt.ourID, tt.dbRecordExists, tt.dbContainerID, tt.purgeOrphans, got, tt.want)
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

	err := ReconcileAndCleanupContainers(context.Background(), store, nil, log, ReconcileOptions{})
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

	err := ReconcileAndCleanupContainers(context.Background(), store, nil, log, ReconcileOptions{})
	if err == nil {
		t.Fatal("expected ReconcileAndCleanupContainers to return an error when ListModules fails, got nil")
	}
	if store.updateServerCalls != 0 || store.updateModuleCalls != 0 {
		t.Fatal("expected no adoption writes when the boot guard aborts")
	}
}
