// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"testing"

	"connectrpc.com/connect"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestCreateServerRejectsAnUnapprovedImage(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := ctxWithRole("operator")

	_, err := svc.CreateServer(ctx, connect.NewRequest(&v1.CreateServerRequest{
		Name:        "foreign-image",
		NodeId:      "test-node",
		McVersion:   "1.20.1",
		DockerImage: "ghcr.io/example/evil:1",
		Port:        25580,
	}))
	if err == nil {
		t.Fatal("CreateServer accepted an image outside the Minecraft server repositories")
	}
	connErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T: %v", err, err)
	}
	if connErr.Code() != connect.CodeInvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument (%v)", connErr.Code(), connErr)
	}
}

func TestCreateServerKeepsABedrockImageReference(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := ctxWithRole("operator")

	created, err := svc.CreateServer(ctx, connect.NewRequest(&v1.CreateServerRequest{
		Name:        "bedrock-image",
		NodeId:      "test-node",
		McVersion:   "1.20.1",
		DockerImage: "docker.io/itzg/minecraft-bedrock-server:latest",
		Port:        25581,
	}))
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	if got := created.Msg.GetServer().GetDockerImage(); got != "itzg/minecraft-bedrock-server:latest" {
		t.Fatalf("stored docker image = %q", got)
	}
}

func TestCreateServerKeepsAJavaImageAsATag(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := ctxWithRole("operator")

	created, err := svc.CreateServer(ctx, connect.NewRequest(&v1.CreateServerRequest{
		Name:        "java-image",
		NodeId:      "test-node",
		McVersion:   "1.20.1",
		DockerImage: "itzg/minecraft-server:java21",
		Port:        25582,
	}))
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	if got := created.Msg.GetServer().GetDockerImage(); got != "java21" {
		t.Fatalf("stored docker image = %q, want java21", got)
	}
}
