// SPDX-License-Identifier: MIT AND MPL-2.0
// This file includes DiscoPanel material under the MIT License,
// Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
// and still applies to that inherited material.
// Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
// If a copy of the MPL was not distributed with this file, You can obtain
// one at https://mozilla.org/MPL/2.0/.


package docker

import (
	"context"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// CleanupOrphanedContainers removes containers that are no longer tracked in the database
func (c *Client) CleanupOrphanedContainers(ctx context.Context, trackedContainerIDs map[string]bool, log *logger.Logger) error {
	// List all containers managed by CARBONPANEL
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "CARBONPANEL.managed=true")

	containers, err := c.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return err
	}

	for _, cont := range containers {
		// Check if this container is tracked in the database
		if !trackedContainerIDs[cont.ID] {
			log.Info("Found orphaned container %s (%s), removing...", cont.ID[:12], cont.Names[0])

			// Stop container if running
			if cont.State == "running" {
				timeout := 30
				if err := c.docker.ContainerStop(ctx, cont.ID, container.StopOptions{
					Timeout: &timeout,
				}); err != nil {
					log.Error("Failed to stop orphaned container %s: %v", cont.ID[:12], err)
				}
			}

			// Remove container
			if err := c.docker.ContainerRemove(ctx, cont.ID, container.RemoveOptions{
				Force: true,
			}); err != nil {
				log.Error("Failed to remove orphaned container %s: %v", cont.ID[:12], err)
			} else {
				log.Info("Successfully removed orphaned container %s", cont.ID[:12])
			}
		}
	}

	return nil
}
