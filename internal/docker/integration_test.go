//go:build integration

// SPDX-License-Identifier: MPL-2.0
package docker

import (
	"context"
	"testing"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

// TestIntegrationDockerDaemonReachable verifies that a Docker client can be
// constructed and that the daemon responds to a Ping. It is skipped
// automatically when no Docker daemon is available, so it never blocks the
// default `go test ./...` run (which excludes the `integration` build tag).
func TestIntegrationDockerDaemonReachable(t *testing.T) {
	log := logger.New()
	client, err := NewClient("", log)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()
	res, err := client.Ping(ctx)
	if err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
	}
	if res.APIVersion == "" {
		t.Fatalf("ping returned empty API version")
	}
	t.Logf("docker daemon reachable: API version %s", res.APIVersion)
}

// TestIntegrationDockerClientHostConfig verifies that a client constructed with
// an explicit (empty) host still defaults to environment-based discovery and
// reports a consistent IsRemote value for a local socket.
func TestIntegrationDockerClientHostConfig(t *testing.T) {
	log := logger.New()
	client, err := NewClient("", log)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()
	if _, err := client.Ping(ctx); err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
	}
	if client.IsRemote() {
		t.Fatalf("expected local client to report IsRemote=false")
	}
}
