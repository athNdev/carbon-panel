// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"fmt"
	"strings"
	"sync"

	models "github.com/athNdev/carbon-panel/internal/db"
	"github.com/containerd/errdefs"
)

// imagePulls shares one in-flight pull per image reference. A waiter that
// cancels does not stop the leader; it returns its own cancellation.
type imagePulls struct {
	mu       sync.Mutex
	inflight map[string]*imagePullCall
}

type imagePullCall struct {
	done    chan struct{}
	err     error
	id      string
	waiters int
}

func (p *imagePulls) do(ctx context.Context, image string, fn func() (string, error)) (string, error) {
	p.mu.Lock()
	if p.inflight == nil {
		p.inflight = map[string]*imagePullCall{}
	}
	if call, ok := p.inflight[image]; ok {
		call.waiters++
		p.mu.Unlock()
		select {
		case <-call.done:
			return call.id, call.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	call := &imagePullCall{done: make(chan struct{})}
	p.inflight[image] = call
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		delete(p.inflight, image)
		p.mu.Unlock()
		close(call.done)
	}()
	call.id, call.err = fn()
	return call.id, call.err
}

func (p *imagePulls) waiterCount(image string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if call, ok := p.inflight[image]; ok {
		return call.waiters
	}
	return 0
}

// ensureImage pulls imageName when the daemon does not already have it.
// A local image skips the registry. Concurrent callers share one pull.
// The returned ID is the daemon image identity. The pull is a failure when
// the daemon still does not report that image.
func (c *Client) ensureImage(ctx context.Context, imageName string) (string, error) {
	imageName = strings.TrimSpace(imageName)
	if imageName == "" || strings.ContainsAny(imageName, " \t\r\n") {
		return "", fmt.Errorf("invalid image reference %q", imageName)
	}
	id, present, err := c.imageIdentity(ctx, imageName)
	if err != nil {
		return "", err
	}
	if present {
		if c.log != nil {
			c.log.Debug("Image %s exists locally, skipping pull", imageName)
		}
		return id, nil
	}
	if c.log != nil {
		c.log.Info("Image %s not present locally, pulling...", imageName)
	}
	return c.pulls.do(ctx, imageName, func() (string, error) {
		id, present, err := c.imageIdentity(ctx, imageName)
		if err != nil {
			return "", err
		}
		if present {
			return id, nil
		}
		if err := c.pullImage(ctx, imageName); err != nil {
			return "", err
		}
		id, present, err = c.imageIdentity(ctx, imageName)
		if err != nil {
			return "", err
		}
		if !present {
			return "", fmt.Errorf("image %s is not present after pull", imageName)
		}
		return id, nil
	})
}

// ServerImageID is the daemon identity of the server's resolved image.
func (c *Client) ServerImageID(ctx context.Context, server *models.Server) (string, error) {
	imageName, err := serverImageName(server)
	if err != nil {
		return "", err
	}
	id, present, err := c.imageIdentity(ctx, imageName)
	if err != nil {
		return "", err
	}
	if !present {
		return "", fmt.Errorf("image %s is not present", imageName)
	}
	return id, nil
}

func (c *Client) imageIdentity(ctx context.Context, imageName string) (string, bool, error) {
	inspected, err := c.docker.ImageInspect(ctx, imageName)
	if err == nil {
		if inspected.ID == "" {
			return "", false, fmt.Errorf("image %s has no identity", imageName)
		}
		return inspected.ID, true, nil
	}
	if errdefs.IsNotFound(err) {
		return "", false, nil
	}
	return "", false, fmt.Errorf("failed to inspect image %s: %w", imageName, err)
}
