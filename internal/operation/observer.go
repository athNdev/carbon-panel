// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package operation

import (
	"context"
	"errors"

	"github.com/athNdev/carbon-panel/internal/docker"
)

type dockerObserver struct {
	client *docker.Client
}

// NewDockerObserver adapts a Docker client to recovery's observation interface.
func NewDockerObserver(client *docker.Client) Observer {
	return &dockerObserver{client: client}
}

func (d *dockerObserver) Observe(ctx context.Context, serverID, containerID string) (Observed, error) {
	summary, err := d.client.ResolveContainer(ctx, serverID)
	if errors.Is(err, docker.ErrContainerNotResolved) {
		return Observed{}, nil
	}
	if err != nil {
		return Observed{}, err
	}
	return Observed{
		Exists:      true,
		Running:     summary.State == "running",
		ContainerID: summary.ID,
	}, nil
}
