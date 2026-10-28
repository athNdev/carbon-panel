// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package snapshot

import (
	"context"
	"fmt"
	"strings"
	"time"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// Consistency classifies how consistent an archive taken from a possibly live
// server can honestly claim to be (OSS-235). A failed save flush is never
// silently reported as consistent.
type Consistency string

const (
	// ConsistencyVerifiedStopped: the server was stopped for the archive.
	ConsistencyVerifiedStopped Consistency = "verified stopped"
	// ConsistencyVerifiedQuiesced: world saves were frozen and flushed before
	// archiving.
	ConsistencyVerifiedQuiesced Consistency = "verified quiesced"
	// ConsistencyCrashConsistent: the files are exactly what the server last
	// wrote; no verified flush happened before archiving.
	ConsistencyCrashConsistent Consistency = "crash-consistent"
)

// CommandSender sends a console command to a server over RCON.
type CommandSender interface {
	SendCommand(ctx context.Context, serverID, command string) (string, error)
}

// QuiesceWorld freezes world saves over RCON so the files on disk are stable
// while they are archived, and reports the consistency level the archive may
// claim. The returned resume function re-enables saving with a bounded,
// independent context and reports failure, so a world left in save-off is
// never silent (OSS-235). Every scheduler, snapshot and pre-update path
// shares these rules.
func QuiesceWorld(ctx context.Context, sender CommandSender, log *logger.Logger, server *storage.Server) (resume func() error, level Consistency) {
	if sender == nil || server.Status != storage.StatusRunning || server.ContainerID == "" {
		return func() error { return nil }, ConsistencyVerifiedStopped
	}

	pauseCtx, pauseCancel := context.WithTimeout(ctx, 30*time.Second)
	defer pauseCancel()

	if _, err := sender.SendCommand(pauseCtx, server.ID, "save-off"); err != nil {
		log.Warn("World quiesce: failed to disable saves on server %s (archiving crash-consistent): %v", server.Name, err)
		return func() error { return nil }, ConsistencyCrashConsistent
	}
	level = ConsistencyVerifiedQuiesced
	if _, err := sender.SendCommand(pauseCtx, server.ID, "save-all flush"); err != nil {
		log.Warn("World quiesce: failed to flush world saves on server %s (archiving crash-consistent): %v", server.Name, err)
		level = ConsistencyCrashConsistent
	} else {
		// Give the server a moment to finish writing chunks to disk.
		select {
		case <-pauseCtx.Done():
		case <-time.After(3 * time.Second):
		}
	}

	resume = func() error {
		return ResumeWorldSaves(context.Background(), sender, server.ID)
	}
	return resume, level
}

// ResumeWorldSaves re-enables world saves. A failure means the world may still
// be in save-off. The command is not sent to a stopped server; callers decide
// that from the container state.
func ResumeWorldSaves(ctx context.Context, sender CommandSender, serverID string) error {
	if sender == nil || strings.TrimSpace(serverID) == "" {
		return fmt.Errorf("world saves are still OFF; run save-on or restart the server")
	}
	resumeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if _, err := sender.SendCommand(resumeCtx, serverID, "save-on"); err != nil {
		return fmt.Errorf("world saves are still OFF; run save-on or restart the server: %w", err)
	}
	return nil
}
