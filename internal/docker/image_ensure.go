// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	models "github.com/athNdev/carbon-panel/internal/db"
	"github.com/containerd/errdefs"
	"github.com/moby/moby/client"
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
// The returned ID is the daemon image identity. A cached image for another
// OS or architecture is pulled once; if it still does not match the daemon,
// the pull is a failure and no container is created from it.
func (c *Client) ensureImage(ctx context.Context, imageName string) (string, error) {
	imageName = strings.TrimSpace(imageName)
	if imageName == "" || strings.ContainsAny(imageName, " \t\r\n") {
		return "", fmt.Errorf("invalid image reference %q", imageName)
	}
	id, usable, err := c.usableImage(ctx, imageName)
	if err != nil && !isImagePlatformMismatch(err) {
		return "", err
	}
	if usable {
		if c.log != nil {
			c.log.Debug("Image %s exists locally, skipping pull", imageName)
		}
		return id, nil
	}
	if c.log != nil {
		c.log.Info("Image %s not present locally, pulling...", imageName)
	}
	return c.pulls.do(ctx, imageName, func() (string, error) {
		id, usable, err := c.usableImage(ctx, imageName)
		if err != nil && !isImagePlatformMismatch(err) {
			return "", err
		}
		if usable {
			return id, nil
		}
		if err := c.pullImage(ctx, imageName); err != nil {
			return "", err
		}
		id, usable, err = c.usableImage(ctx, imageName)
		if err != nil {
			return "", err
		}
		if !usable {
			return "", fmt.Errorf("image %s is not present after pull", imageName)
		}
		return id, nil
	})
}

// ServerImageID is the daemon identity of the server's resolved image.
// An image built for another OS or architecture is not that identity.
func (c *Client) ServerImageID(ctx context.Context, server *models.Server) (string, error) {
	imageName, err := serverImageName(server)
	if err != nil {
		return "", err
	}
	id, usable, err := c.usableImage(ctx, imageName)
	if err != nil {
		return "", err
	}
	if !usable {
		return "", fmt.Errorf("image %s is not present", imageName)
	}
	return id, nil
}

type imageRecord struct {
	id   string
	os   string
	arch string
}

type imagePlatformError struct {
	image      string
	imageOS    string
	imageArch  string
	daemonOS   string
	daemonArch string
}

func (e *imagePlatformError) Error() string {
	return fmt.Sprintf("image %s platform %s does not match daemon platform %s", e.image, formatPlatform(e.imageOS, e.imageArch), formatPlatform(e.daemonOS, e.daemonArch))
}

func isImagePlatformMismatch(err error) bool {
	var mismatch *imagePlatformError
	return errors.As(err, &mismatch)
}

func formatPlatform(osName, arch string) string {
	osName = strings.TrimSpace(osName)
	arch = strings.TrimSpace(arch)
	switch {
	case osName != "" && arch != "":
		return osName + "/" + arch
	case arch != "":
		return arch
	case osName != "":
		return osName
	default:
		return "unknown"
	}
}

func (c *Client) usableImage(ctx context.Context, imageName string) (string, bool, error) {
	rec, present, err := c.imageIdentity(ctx, imageName)
	if err != nil || !present {
		return "", false, err
	}
	if err := c.requirePlatform(ctx, imageName, rec); err != nil {
		return "", false, err
	}
	return rec.id, true, nil
}

func (c *Client) imageIdentity(ctx context.Context, imageName string) (imageRecord, bool, error) {
	inspected, err := c.docker.ImageInspect(ctx, imageName)
	if err == nil {
		if inspected.ID == "" {
			return imageRecord{}, false, fmt.Errorf("image %s has no identity", imageName)
		}
		return imageRecord{id: inspected.ID, os: inspected.Os, arch: inspected.Architecture}, true, nil
	}
	if errdefs.IsNotFound(err) {
		return imageRecord{}, false, nil
	}
	return imageRecord{}, false, fmt.Errorf("failed to inspect image %s: %w", imageName, err)
}

// daemonPlatform reads the daemon OS and architecture. A daemon that does
// not report either is not a mismatch; the caller skips the comparison.
func (c *Client) daemonPlatform(ctx context.Context) (osName, arch string, ok bool) {
	info, err := c.docker.Info(ctx, client.InfoOptions{})
	if err != nil {
		if c.log != nil {
			c.log.Debug("Image platform check skipped: %v", err)
		}
		return "", "", false
	}
	osName = strings.TrimSpace(info.Info.OSType)
	arch = strings.TrimSpace(info.Info.Architecture)
	if osName == "" && arch == "" {
		return "", "", false
	}
	return osName, arch, true
}

func (c *Client) requirePlatform(ctx context.Context, imageName string, rec imageRecord) error {
	osName, arch, ok := c.daemonPlatform(ctx)
	if !ok {
		return nil
	}
	if platformsMatch(osName, arch, rec.os, rec.arch) {
		return nil
	}
	return &imagePlatformError{
		image:      imageName,
		imageOS:    rec.os,
		imageArch:  rec.arch,
		daemonOS:   osName,
		daemonArch: arch,
	}
}

func platformsMatch(daemonOS, daemonArch, imageOS, imageArch string) bool {
	if daemonOS != "" && imageOS != "" && !strings.EqualFold(strings.TrimSpace(daemonOS), strings.TrimSpace(imageOS)) {
		return false
	}
	if daemonArch != "" && imageArch != "" && normalizeArch(daemonArch) != normalizeArch(imageArch) {
		return false
	}
	return true
}

func normalizeArch(arch string) string {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "amd64", "x86_64":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	case "arm", "armhf", "armv7", "armv7l":
		return "arm"
	case "386", "i386", "i686":
		return "386"
	default:
		return strings.ToLower(strings.TrimSpace(arch))
	}
}
