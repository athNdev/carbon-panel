// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package docker

import (
	"context"
	"errors"
	"time"

	models "github.com/athNdev/carbon-panel/internal/db"
	"github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

const (
	// DefaultStopWindow is how long a stop waits for the JVM to save before
	// Docker escalates from SIGTERM to SIGKILL. It matches itzg STOP_DURATION.
	DefaultStopWindow = 60 * time.Second
	// MaxStopWindow caps a per-server override so a bad config cannot pin a worker.
	MaxStopWindow = 180 * time.Second
)

// errContainerGone means Docker has no such container. Callers treat that as
// an already-stopped server.
var errContainerGone = errors.New("container not found")

// StopWindow returns the save window for a server. A missing or non-positive
// StopDuration uses DefaultStopWindow. Values above MaxStopWindow are clamped.
func StopWindow(cfg *models.ServerConfig) time.Duration {
	if cfg == nil || cfg.StopDuration == nil {
		return DefaultStopWindow
	}
	return clampStopWindow(time.Duration(*cfg.StopDuration) * time.Second)
}

// StopTimeoutSeconds is the Docker stop timeout for window. Docker sends
// SIGTERM immediately and SIGKILL only after this many seconds.
func StopTimeoutSeconds(window time.Duration) int {
	secs := int(clampStopWindow(window) / time.Second)
	if secs < 1 {
		return 1
	}
	return secs
}

func clampStopWindow(d time.Duration) time.Duration {
	if d <= 0 {
		return DefaultStopWindow
	}
	if d > MaxStopWindow {
		return MaxStopWindow
	}
	return d
}

// stopDriver is the stop ladder: ask the game to save, then let Docker wait
// out the window, and only then SIGKILL.
type stopDriver interface {
	RequestGameStop(ctx context.Context, containerID string) error
	DockerStop(ctx context.Context, containerID string, timeoutSeconds int) error
	ForceKill(ctx context.Context, containerID string) error
}

func applyStopLadder(ctx context.Context, d stopDriver, containerID string, window time.Duration) error {
	reqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	_ = d.RequestGameStop(reqCtx, containerID)
	cancel()

	if err := d.DockerStop(ctx, containerID, StopTimeoutSeconds(window)); err != nil {
		if errors.Is(err, errContainerGone) {
			return err
		}
		return d.ForceKill(ctx, containerID)
	}
	return nil
}

type clientStopDriver struct {
	c *Client
}

func (d clientStopDriver) RequestGameStop(ctx context.Context, containerID string) error {
	if d.c == nil {
		return nil
	}
	if d.c.gameStop != nil {
		return d.c.gameStop(ctx, containerID)
	}
	if d.c.docker == nil {
		return nil
	}
	_, err := d.c.Exec(ctx, containerID, []string{"rcon-cli", "stop"})
	return err
}

func (d clientStopDriver) DockerStop(ctx context.Context, containerID string, timeoutSeconds int) error {
	timeout := timeoutSeconds
	_, err := d.c.docker.ContainerStop(ctx, containerID, client.ContainerStopOptions{Timeout: &timeout})
	if err == nil {
		return nil
	}
	if errdefs.IsNotFound(err) {
		return errContainerGone
	}
	if inspectResult, inspErr := d.c.docker.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{}); inspErr == nil && inspectResult.Container.State != nil && inspectResult.Container.State.Paused {
		if d.c.log != nil {
			d.c.log.Info("Container %s is paused; unpausing before stop", containerID)
		}
		_ = d.c.UnpauseContainer(ctx, containerID)
		if _, retryErr := d.c.docker.ContainerStop(ctx, containerID, client.ContainerStopOptions{Timeout: &timeout}); retryErr == nil {
			return nil
		} else if errdefs.IsNotFound(retryErr) {
			return errContainerGone
		}
	}
	return err
}

func (d clientStopDriver) ForceKill(ctx context.Context, containerID string) error {
	if d.c.log != nil {
		d.c.log.Warn("Graceful stop failed for container %s, attempting force kill", containerID)
	}
	killCtx, killCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer killCancel()
	_, killErr := d.c.docker.ContainerKill(killCtx, containerID, client.ContainerKillOptions{Signal: "KILL"})
	if killErr != nil {
		if errdefs.IsNotFound(killErr) {
			return errContainerGone
		}
		return killErr
	}
	return nil
}
