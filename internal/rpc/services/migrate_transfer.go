// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/nodestorage"
	"github.com/athNdev/carbon-panel/internal/worldstore"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// migrationPlayabilityTimeout bounds how long a migration waits for the
// target server to accept player connections before it rolls back to the
// source. Package-level so tests can shorten it.
var migrationPlayabilityTimeout = 5 * time.Minute

const migrationPlayabilityPoll = 5 * time.Second

// migrateServerNow performs one journaled migration attempt: quiesce the
// source world, transfer and verify it at the target, cut the container over
// per the preserved intent, commit DB and route ownership, and only then
// clean up the source container. Every failure before the final cleanup rolls
// back to the source identity, so exactly one authoritative playable world
// remains (OSS-234).
func (s *ServerService) migrateServerNow(ctx context.Context, server *storage.Server, targetNode *storage.Node, sourceDockerCli, targetDockerCli *docker.Client, srcRoot, dstRoot string, isRunning bool, oldContainerID string, startTime time.Time) (*v1.MigrateServerResponse, error) {
	sourceNodeID := server.NodeID

	serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get server configuration: %w", err)
	}

	// Quiesce the source world. The flush is best-effort (a clean stop saves
	// the world itself); the stop must succeed or the migration aborts.
	if isRunning && oldContainerID != "" {
		if s.sender != nil {
			if _, ferr := s.sender.SendCommand(ctx, server.ID, "save-all flush"); ferr != nil {
				s.log.Warn("World save flush before migration failed; relying on clean stop: %v", ferr)
			}
		}
		if sourceDockerCli != nil {
			if _, serr := sourceDockerCli.StopContainer(ctx, oldContainerID); serr != nil {
				return nil, fmt.Errorf("source container did not stop cleanly; migration aborted: %w", serr)
			}
		}
	}

	// rollback returns the server to its source container identity. Every
	// failure path below uses it so the source world stays authoritative.
	rollback := func(targetContainerID string) {
		if targetContainerID != "" && targetDockerCli != nil {
			if _, rerr := targetDockerCli.StopContainer(ctx, targetContainerID); rerr != nil {
				s.log.Warn("Failed to stop target container %s during rollback: %v", targetContainerID, rerr)
			}
			if rerr := targetDockerCli.RemoveContainer(ctx, targetContainerID); rerr != nil {
				s.log.Error("Failed to remove target container %s during rollback: %v", targetContainerID, rerr)
			}
		}
		if isRunning && oldContainerID != "" && sourceDockerCli != nil {
			if rerr := sourceDockerCli.StartContainer(ctx, oldContainerID); rerr != nil {
				s.log.Error("Failed to restart source container %s after rollback: %v", oldContainerID, rerr)
			}
		}
	}

	// Transfer and verify the world before any container change.
	if err := s.transferWorld(server, targetNode.ID, srcRoot, dstRoot); err != nil {
		rollback("")
		return nil, fmt.Errorf("world transfer failed; source preserved: %w", err)
	}

	// Target container per preserved intent: running servers are created and
	// started, stopped/paused/sleep-intent servers stay stopped and get their
	// container on the next start.
	newContainerID := ""
	endpoint := ""
	if isRunning {
		if targetDockerCli == nil {
			rollback("")
			return nil, errors.New("target docker client unavailable")
		}
		s.prepareServerConfig(ctx, server, serverConfig)
		id, cerr := targetDockerCli.CreateContainer(ctx, server, serverConfig)
		if cerr != nil {
			rollback("")
			return nil, fmt.Errorf("failed to create container on target node: %w", cerr)
		}
		newContainerID = id
		if serr := targetDockerCli.StartContainer(ctx, newContainerID); serr != nil {
			rollback(newContainerID)
			return nil, fmt.Errorf("failed to start container on target node: %w", serr)
		}

		// Players must be able to connect before the migration is called
		// successful (OSS-234 acceptance); roll back a target that never
		// becomes playable.
		endpoint = gameEndpoint(server, targetNode, s.proxyListenPort())
		if perr := waitForPlayable(ctx, endpoint, migrationPlayabilityTimeout); perr != nil {
			rollback(newContainerID)
			return nil, fmt.Errorf("target did not become playable: %w", perr)
		}
	}

	// Commit ownership before any source cleanup (OSS-234 acceptance): a DB
	// or route failure must stay reversible and must never leave the world
	// without its recoverable source.
	prevNodeID, prevContainerID, prevStatus := server.NodeID, server.ContainerID, server.Status
	server.NodeID = targetNode.ID
	server.ContainerID = newContainerID
	if isRunning {
		now := time.Now()
		server.Status = storage.StatusRunning
		server.LastStarted = &now
	}
	if derr := s.store.UpdateServer(ctx, server); derr != nil {
		server.NodeID, server.ContainerID, server.Status = prevNodeID, prevContainerID, prevStatus
		rollback(newContainerID)
		return nil, fmt.Errorf("world transferred but ownership commit failed; rolled back: %w", derr)
	}

	if s.proxy != nil && server.ProxyHostname != "" {
		if rerr := s.proxy.UpdateServerRoute(server); rerr != nil {
			// Route and record ownership roll back to the source together.
			server.NodeID, server.ContainerID, server.Status = prevNodeID, prevContainerID, prevStatus
			if uerr := s.store.UpdateServer(ctx, server); uerr != nil {
				s.log.Error("Failed to restore server record after route cutover failure: %v", uerr)
			}
			rollback(newContainerID)
			return nil, fmt.Errorf("route cutover failed; rolled back: %w", rerr)
		}
	}

	// Cutover committed: the source container can go now — synchronously, so
	// no unscoped cleanup outlives the operation. The source world DATA is
	// preserved for recovery (world disposition is OSS-244's lane).
	if oldContainerID != "" && sourceDockerCli != nil {
		if rmerr := sourceDockerCli.RemoveContainer(ctx, oldContainerID); rmerr != nil {
			s.log.Warn("Retained source container %s could not be cleaned up after migration: %v", oldContainerID, rmerr)
		}
	}

	durationMs := time.Since(startTime).Milliseconds()
	detail := "; target will start on next boot per its stopped intent"
	if isRunning {
		detail = "; player connections verified on " + endpoint
	}
	msg := fmt.Sprintf("Migrated '%s' to node '%s' (%dms downtime; world transfer verified%s)", server.Name, targetNode.ID, durationMs, detail)
	s.log.Info("%s", msg)

	return &v1.MigrateServerResponse{
		Success:      true,
		Message:      msg,
		SourceNodeId: sourceNodeID,
		TargetNodeId: targetNode.ID,
		DurationMs:   durationMs,
		Server:       dbServerToProto(server),
	}, nil
}

// migrationResponseError maps migration failures onto connect errors: an
// unreachable storage side is a refusal, everything else is internal. The
// journaled operation stays visible for recovery in either case.
func migrationResponseError(err error) error {
	if nodestorage.IsUnavailable(err) {
		return connect.NewError(connect.CodeUnavailable, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}

// migrationWorldRoots resolves the controller-side world roots for the source
// and target nodes of a migration (ADR 0005). Both must be reachable from the
// controller: a migration must never claim success without transferring the
// world it can actually read and write.
func (s *ServerService) migrationWorldRoots(ctx context.Context, server *storage.Server, targetNode *storage.Node) (srcRoot, dstRoot string, err error) {
	srcInfo, err := storageNodeInfo(ctx, s.store, server.NodeID)
	if err != nil {
		return "", "", fmt.Errorf("cannot identify source storage: %w", err)
	}
	srcRoot, err = nodestorage.MapDataPath(srcInfo, server.DataPath)
	if err != nil {
		return "", "", fmt.Errorf("source node %q storage is not reachable from the controller: %w", server.NodeID, err)
	}
	if _, serr := os.Stat(srcRoot); serr != nil {
		return "", "", fmt.Errorf("source world %q is not reachable: %w", srcRoot, serr)
	}

	dstRoot, err = nodestorage.EnsureDataRoot(targetNode.StorageInfo(), server.DataPath)
	if err != nil {
		return "", "", fmt.Errorf("target node %q storage is not reachable from the controller: %w", targetNode.ID, err)
	}
	return srcRoot, dstRoot, nil
}

// transferWorld moves the server's full data tree from srcRoot to dstRoot
// through the world-data seam (ADR 0004): a manifest archive with per-file
// hashes is restored into the target, then the restored tree is re-archived
// and compared so success means verified content equality, not just a
// completed copy. Source and target sharing one storage root (a common mount)
// need no copy.
func (s *ServerService) transferWorld(server *storage.Server, targetNodeID, srcRoot, dstRoot string) error {
	if filepath.Clean(srcRoot) == filepath.Clean(dstRoot) {
		s.log.Info("Source and target share world storage %s; no transfer needed", srcRoot)
		return nil
	}
	if s.config == nil || s.config.Storage.DataDir == "" {
		return errors.New("world transfer requires a configured storage data dir")
	}
	if err := os.MkdirAll(s.config.Storage.DataDir, 0o755); err != nil {
		return fmt.Errorf("prepare world transfer workspace: %w", err)
	}
	owner, err := docker.LoadOrCreateInstallationID(s.config.Storage.DataDir)
	if err != nil {
		return fmt.Errorf("installation identity: %w", err)
	}
	journal := filepath.Join(s.config.Storage.DataDir, "worldstore-journal")

	src, err := worldstore.OpenServerWorlds(owner, server.ID, server.NodeID, journal, srcRoot)
	if err != nil {
		return fmt.Errorf("open source world: %w", err)
	}
	defer func() { _ = src.Close() }()
	if err := src.Recover(); err != nil {
		return fmt.Errorf("source world recovery: %w", err)
	}

	dst, err := worldstore.OpenServerWorlds(owner, server.ID, targetNodeID, journal, dstRoot)
	if err != nil {
		return fmt.Errorf("open target world: %w", err)
	}
	defer func() { _ = dst.Close() }()
	if err := dst.Recover(); err != nil {
		return fmt.Errorf("target world recovery: %w", err)
	}

	archivePath, err := tempWorldArchive("carbon-world-transfer-*.zip")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(archivePath) }()

	if _, err := src.BackupTo(src.Identity(), archivePath, nil); err != nil {
		return fmt.Errorf("archive source world: %w", err)
	}
	// RestoreFrom verifies every file against the source manifest while it
	// extracts, so a corrupted transfer can never pass as complete.
	if err := dst.RestoreFrom(dst.Identity(), archivePath); err != nil {
		return fmt.Errorf("restore world at target (verified against the source manifest): %w", err)
	}

	// Content equality: re-archive the restored target and compare digests.
	verifyPath, err := tempWorldArchive("carbon-world-verify-*.zip")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(verifyPath) }()

	if _, err := dst.BackupTo(dst.Identity(), verifyPath, nil); err != nil {
		return fmt.Errorf("verify target world: %w", err)
	}
	srcSum, err := fileSHA256(archivePath)
	if err != nil {
		return err
	}
	dstSum, err := fileSHA256(verifyPath)
	if err != nil {
		return err
	}
	if srcSum != dstSum {
		return fmt.Errorf("target world content does not match the source after transfer (source %s, target %s)", srcSum, dstSum)
	}
	s.log.Info("World transfer for %s verified: digest %s", server.ID, srcSum)
	return nil
}

func tempWorldArchive(pattern string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("create world archive: %w", err)
	}
	path := f.Name()
	_ = f.Close()
	return path, nil
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read archive %q: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// gameEndpoint returns the address players connect to for a server: the
// proxy listener for proxied servers, otherwise the game port on the target
// node's advertised address.
func gameEndpoint(server *storage.Server, targetNode *storage.Node, proxyListenPort int) string {
	if server.ProxyHostname != "" && proxyListenPort > 0 {
		return net.JoinHostPort(server.ProxyHostname, strconv.Itoa(proxyListenPort))
	}
	host := strings.TrimSpace(targetNode.AdvertisedIP)
	if host == "" {
		host = dockerHostAddress(targetNode.Host)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(server.Port))
}

// dockerHostAddress extracts the host part of a Docker daemon URL such as
// "tcp://192.168.0.10:2376".
func dockerHostAddress(host string) string {
	if u, err := url.Parse(host); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return ""
}

// waitForPlayable waits until the server's player-facing endpoint accepts TCP
// connections. Migration is only reported as successful after players can
// connect; the bounded wait fails otherwise so callers roll back (OSS-234
// acceptance).
func waitForPlayable(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("target server did not accept player connections on %s within %s: %w", addr, timeout, err)
		}
		wait := migrationPlayabilityPoll
		if rem := time.Until(deadline); rem < wait {
			wait = rem
		}
		if wait <= 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// proxyListenPort is the player-facing proxy listener used for playability
// probes of proxied servers; zero when no proxy listener is configured.
func (s *ServerService) proxyListenPort() int {
	if s.config != nil && s.config.Proxy.Enabled && len(s.config.Proxy.ListenPorts) > 0 {
		return int(s.config.Proxy.ListenPorts[0])
	}
	return 0
}
