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
	done       chan struct{}
	err        error
	id         string
	repoDigest string
	waiters    int
}

func (p *imagePulls) do(ctx context.Context, image string, fn func() (string, string, error)) (string, string, error) {
	p.mu.Lock()
	if p.inflight == nil {
		p.inflight = map[string]*imagePullCall{}
	}
	if call, ok := p.inflight[image]; ok {
		call.waiters++
		p.mu.Unlock()
		select {
		case <-call.done:
			return call.id, call.repoDigest, call.err
		case <-ctx.Done():
			return "", "", ctx.Err()
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
	call.id, call.repoDigest, call.err = fn()
	return call.id, call.repoDigest, call.err
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
	id, _, err := c.ensureImageWithDigest(ctx, imageName)
	return id, err
}

// ensureImageWithDigest is ensureImage plus the registry repo digest from
// the same inspect. The digest is empty when the daemon has no repo digest
// for that repository. It is not the image config ID.
func (c *Client) ensureImageWithDigest(ctx context.Context, imageName string) (string, string, error) {
	imageName = strings.TrimSpace(imageName)
	if imageName == "" || strings.ContainsAny(imageName, " \t\r\n") {
		return "", "", fmt.Errorf("invalid image reference %q", imageName)
	}
	rec, usable, err := c.usableImage(ctx, imageName)
	if err != nil && !isImagePlatformMismatch(err) {
		return "", "", err
	}
	if usable {
		if c.log != nil {
			c.log.Debug("Image %s exists locally, skipping pull", imageName)
		}
		return rec.id, rec.repoDigest, nil
	}
	if c.log != nil {
		c.log.Info("Image %s not present locally, pulling...", imageName)
	}
	return c.pulls.do(ctx, imageName, func() (string, string, error) {
		rec, usable, err := c.usableImage(ctx, imageName)
		if err != nil && !isImagePlatformMismatch(err) {
			return "", "", err
		}
		if usable {
			return rec.id, rec.repoDigest, nil
		}
		if err := c.pullImage(ctx, imageName); err != nil {
			return "", "", err
		}
		rec, usable, err = c.usableImage(ctx, imageName)
		if err != nil {
			return "", "", err
		}
		if !usable {
			return "", "", fmt.Errorf("image %s is not present after pull", imageName)
		}
		return rec.id, rec.repoDigest, nil
	})
}

// ServerImageID is the daemon identity of the server's resolved image.
// An image built for another OS or architecture is not that identity.
func (c *Client) ServerImageID(ctx context.Context, server *models.Server) (string, error) {
	id, _, err := c.ServerImageIdentity(ctx, server)
	return id, err
}

// ServerImageIdentity is the daemon image ID and the registry repo digest
// for the server's resolved image. The digest is empty when this repository
// has no repo digest on the daemon. An image built for another OS or
// architecture is not an identity.
func (c *Client) ServerImageIdentity(ctx context.Context, server *models.Server) (string, string, error) {
	imageName, err := serverImageName(server)
	if err != nil {
		return "", "", err
	}
	rec, usable, err := c.usableImage(ctx, imageName)
	if err != nil {
		return "", "", err
	}
	if !usable {
		return "", "", fmt.Errorf("image %s is not present", imageName)
	}
	return rec.id, rec.repoDigest, nil
}

type imageRecord struct {
	id         string
	repoDigest string
	os         string
	arch       string
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

func (c *Client) usableImage(ctx context.Context, imageName string) (imageRecord, bool, error) {
	rec, present, err := c.imageIdentity(ctx, imageName)
	if err != nil || !present {
		return imageRecord{}, false, err
	}
	if err := c.requirePlatform(ctx, imageName, rec); err != nil {
		return imageRecord{}, false, err
	}
	return rec, true, nil
}

func (c *Client) imageIdentity(ctx context.Context, imageName string) (imageRecord, bool, error) {
	inspected, err := c.docker.ImageInspect(ctx, imageName)
	if err == nil {
		if inspected.ID == "" {
			return imageRecord{}, false, fmt.Errorf("image %s has no identity", imageName)
		}
		return imageRecord{
			id:         inspected.ID,
			repoDigest: selectRepoDigest(imageName, inspected.RepoDigests),
			os:         inspected.Os,
			arch:       inspected.Architecture,
		}, true, nil
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

// selectRepoDigest picks the registry digest for imageName. It prefers an
// entry for the requested repository and ignores another repository that
// happens to share the image config ID. The result is repository@sha256:...,
// never the image ID itself.
func selectRepoDigest(imageName string, digests []string) string {
	wantRepo := imageRepository(imageName)
	wantDigest := imagePinnedDigest(imageName)
	fallback := ""
	for _, entry := range digests {
		repo, digest, ok := splitRepoDigest(entry)
		if !ok || digest == "" {
			continue
		}
		full := repo + "@" + digest
		if wantDigest != "" && digest == wantDigest && (wantRepo == "" || repo == wantRepo) {
			return full
		}
		if wantDigest == "" && wantRepo != "" && repo == wantRepo && fallback == "" {
			fallback = full
		}
	}
	return fallback
}

func imagePinnedDigest(ref string) string {
	ref = strings.TrimSpace(ref)
	at := strings.LastIndex(ref, "@")
	if at < 0 || at == len(ref)-1 {
		return ""
	}
	digest := strings.ToLower(ref[at+1:])
	if _, _, ok := splitRepoDigest("repo@" + digest); !ok {
		return ""
	}
	return digest
}

func splitRepoDigest(entry string) (repo, digest string, ok bool) {
	entry = strings.TrimSpace(entry)
	at := strings.LastIndex(entry, "@")
	if at <= 0 || at == len(entry)-1 {
		return "", "", false
	}
	digest = strings.ToLower(entry[at+1:])
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
		return "", "", false
	}
	for _, r := range digest[len("sha256:"):] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return "", "", false
		}
	}
	repo = imageRepository(entry[:at])
	if repo == "" {
		return "", "", false
	}
	return repo, digest, true
}

func imageRepository(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "docker.io/")
	ref = strings.TrimPrefix(ref, "index.docker.io/")
	if at := strings.LastIndex(ref, "@"); at >= 0 {
		ref = ref[:at]
	}
	if slash := strings.LastIndex(ref, "/"); slash >= 0 {
		if colon := strings.LastIndex(ref[slash:], ":"); colon >= 0 {
			ref = ref[:slash+colon]
		}
	} else if colon := strings.LastIndex(ref, ":"); colon >= 0 {
		ref = ref[:colon]
	}
	return ref
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
