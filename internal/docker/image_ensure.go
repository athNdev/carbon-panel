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
	waiters int
}

func (p *imagePulls) do(ctx context.Context, image string, fn func() error) error {
	p.mu.Lock()
	if p.inflight == nil {
		p.inflight = map[string]*imagePullCall{}
	}
	if call, ok := p.inflight[image]; ok {
		call.waiters++
		p.mu.Unlock()
		select {
		case <-call.done:
			return call.err
		case <-ctx.Done():
			return ctx.Err()
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
	call.err = fn()
	return call.err
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
// The pull is a failure when the daemon still does not report the image.
func (c *Client) ensureImage(ctx context.Context, imageName string) error {
	imageName = strings.TrimSpace(imageName)
	if imageName == "" || strings.ContainsAny(imageName, " \t\r\n") {
		return fmt.Errorf("invalid image reference %q", imageName)
	}
	present, err := c.imagePresent(ctx, imageName)
	if err != nil {
		return err
	}
	if present {
		if c.log != nil {
			c.log.Debug("Image %s exists locally, skipping pull", imageName)
		}
		return nil
	}
	if c.log != nil {
		c.log.Info("Image %s not present locally, pulling...", imageName)
	}
	return c.pulls.do(ctx, imageName, func() error {
		present, err := c.imagePresent(ctx, imageName)
		if err != nil {
			return err
		}
		if present {
			return nil
		}
		if err := c.pullImage(ctx, imageName); err != nil {
			return err
		}
		present, err = c.imagePresent(ctx, imageName)
		if err != nil {
			return err
		}
		if !present {
			return fmt.Errorf("image %s is not present after pull", imageName)
		}
		return nil
	})
}

func (c *Client) imagePresent(ctx context.Context, imageName string) (bool, error) {
	_, err := c.docker.ImageInspect(ctx, imageName)
	if err == nil {
		return true, nil
	}
	if errdefs.IsNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("failed to inspect image %s: %w", imageName, err)
}
