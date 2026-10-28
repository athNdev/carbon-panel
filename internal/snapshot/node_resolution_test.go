// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package snapshot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSnapshot_Rollback_UnresolvableRemoteNodeMakesNoDockerCalls verifies that
// a rollback aborts before mutating anything when the server's remote node
// cannot be resolved, and never falls back to the controller's local daemon.
func TestSnapshot_Rollback_UnresolvableRemoteNodeMakesNoDockerCalls(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	var localDockerCalls atomic.Int32
	dockerAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		localDockerCalls.Add(1)
		http.NotFound(w, r)
	}))
	defer dockerAPI.Close()

	localClient, err := docker.NewClient(strings.Replace(dockerAPI.URL, "http://", "tcp://", 1), logger.New())
	require.NoError(t, err)

	pool := docker.NewClientPool(store, localClient, logger.New())
	defer func() { _ = pool.Close() }()

	serverDir := t.TempDir()
	testFile := filepath.Join(serverDir, "server.properties")
	require.NoError(t, os.WriteFile(testFile, []byte("port=25565\n"), 0644))

	server := &storage.Server{
		ID:       uuid.New().String(),
		Name:     "remote-rollback-server",
		NodeID:   "missing-remote-node",
		DataPath: serverDir,
		Status:   storage.StatusStopped,
	}
	require.NoError(t, store.CreateServer(ctx, server))

	engine := NewEngineWithPool(store, localClient, pool, nil, logger.New(), Config{
		MaxSnapshots: 5,
	})
	// The fixture archive is this local directory. Rollback is what must refuse
	// the missing remote node before it touches Docker or the files.
	server.NodeID = ""
	snap, err := engine.CreatePreUpdateSnapshot(ctx, server, "baseline")
	require.NoError(t, err)
	server.NodeID = "missing-remote-node"

	// Simulate the modification a rollback would overwrite, and a running
	// container that the rollback would have to stop.
	require.NoError(t, os.WriteFile(testFile, []byte("port=25577\nmodified=true\n"), 0644))
	server.Status = storage.StatusRunning
	server.ContainerID = "running-container"

	// The sentinel daemon is contacted once during client construction (API
	// negotiation); only calls made from here on belong to the rollback.
	localDockerCalls.Store(0)

	err = engine.Rollback(ctx, server, snap.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing-remote-node")

	if got := localDockerCalls.Load(); got != 0 {
		t.Fatalf("local Docker daemon received %d calls while resolving a remote node; want 0", got)
	}

	content, readErr := os.ReadFile(testFile)
	require.NoError(t, readErr)
	assert.Contains(t, string(content), "modified=true", "rollback must not extract files when node resolution fails")
}
