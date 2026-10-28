// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/activity"
	"github.com/athNdev/carbon-panel/internal/db"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestUpdateServerImageFailureKeepsTheAppliedImage(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := ctxWithRole("operator")

	server := &db.Server{
		ID: "image-hold", Name: "Image Hold", NodeID: "test-node",
		MCVersion: "1.20.1", ModLoader: db.ModLoaderVanilla,
		DockerImage: "java21", AppliedImageID: "sha256:known",
		ContainerID: "running-container", Status: db.StatusRunning,
		Port: 25583, Memory: 2048, MaxPlayers: 20, DataPath: t.TempDir(),
	}
	if err := svc.store.CreateServer(ctx, server); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	if err := svc.store.SaveServerConfig(ctx, svc.store.CreateDefaultServerConfig(server.ID)); err != nil {
		t.Fatalf("SaveServerConfig: %v", err)
	}

	_, err := svc.UpdateServer(ctx, connect.NewRequest(&v1.UpdateServerRequest{
		Id:          server.ID,
		DockerImage: "java25",
	}))
	if err == nil {
		t.Fatal("UpdateServer() error = nil, want the image pull to fail")
	}

	updated, err := svc.store.GetServer(ctx, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DockerImage != "java25" {
		t.Fatalf("stored docker image = %q, want the requested tag", updated.DockerImage)
	}
	if updated.AppliedImageID != "sha256:known" {
		t.Fatalf("applied image = %q, want the previous identity", updated.AppliedImageID)
	}
	if updated.ContainerID != "running-container" {
		t.Fatalf("container ID = %q, want the running container", updated.ContainerID)
	}

	logs, err := activity.List(svc.store.DB(), server.ID, "", time.Time{}, 20)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, entry := range logs {
		if entry.Event != activity.EventServerImage {
			continue
		}
		found = true
		for _, want := range []string{`"previous":"java21"`, `"requested":"java25"`, `"applied_image_id":"sha256:known"`, `"applied":false`} {
			if !strings.Contains(entry.Properties, want) {
				t.Fatalf("audit properties = %s, want %s", entry.Properties, want)
			}
		}
	}
	if !found {
		t.Fatal("image change was not audited")
	}
}
