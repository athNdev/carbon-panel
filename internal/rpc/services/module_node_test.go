// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/module"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// TestCreateModule_PersistsServerNodeID verifies that a module created for a
// server on a remote node is pinned to that node, so module lifecycle
// operations target the same Docker daemon as the server.
func TestCreateModule_PersistsServerNodeID(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()

	const remoteNodeID = "node-remote-7"

	server := &storage.Server{
		ID:     "srv-remote-module",
		Name:   "Remote Module Host",
		NodeID: remoteNodeID,
		Status: storage.StatusStopped,
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}

	template := &storage.ModuleTemplate{
		ID:          "tpl-node-id",
		Name:        "Node ID Template",
		DockerImage: "alpine:latest",
	}
	if err := store.CreateModuleTemplate(ctx, template); err != nil {
		t.Fatalf("CreateModuleTemplate: %v", err)
	}

	manager := module.NewManager(store, nil, nil, &config.Config{}, nil, logger.New())
	svc := &ModuleService{store: store, moduleManager: manager, log: logger.New()}

	resp, err := svc.CreateModule(ctx, connect.NewRequest(&v1.CreateModuleRequest{
		Name:       "geyser",
		ServerId:   server.ID,
		TemplateId: template.ID,
	}))
	if err != nil {
		t.Fatalf("CreateModule: %v", err)
	}
	if resp.Msg.Module == nil {
		t.Fatal("CreateModule returned no module")
	}

	persisted, err := store.GetModule(ctx, resp.Msg.Module.Id)
	if err != nil {
		t.Fatalf("GetModule: %v", err)
	}
	if persisted.NodeID != remoteNodeID {
		t.Fatalf("persisted module NodeID = %q, want %q", persisted.NodeID, remoteNodeID)
	}

	// CreateModule provisions the container in the background. The manager has no
	// Docker client, so that attempt fails fast; wait for it to settle before the
	// test closes the store.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current, getErr := store.GetModule(ctx, persisted.ID)
		if getErr == nil && current.Status == storage.ModuleStatusError {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}
