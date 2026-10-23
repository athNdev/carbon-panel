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
	"errors"
	"fmt"
	"strings"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/moby/moby/client"
)

// ReconcileStore is the subset of *db.Store that startup reconciliation needs. It exists so
// the adopt-vs-orphan decision can be exercised in tests against an in-memory fake, without
// standing up a real database. *db.Store satisfies this interface.
type ReconcileStore interface {
	ListServers(ctx context.Context) ([]*db.Server, error)
	UpdateServer(ctx context.Context, server *db.Server) error

	ListModules(ctx context.Context) ([]*db.Module, error)
	UpdateModule(ctx context.Context, module *db.Module) error
}

// managedContainer is a minimal, Docker-SDK-decoupled view of a container discovered during
// startup reconciliation. Extracting this representation (and the pure decision function below)
// keeps the adopt-vs-orphan decision unit-testable without a live Docker daemon, which matters
// because a wrong decision here can delete a running game server.
type managedContainer struct {
	ID             string
	Name           string
	Running        bool
	InstallationID string // from the carbon-panel.installation label; empty for legacy containers
	ServerID       string // from the carbon-panel.server.id label; empty if this isn't a server container
	ModuleID       string // from the carbon-panel.module.id label; empty if this isn't a module container
}

// reconcileAction is the phase-1 verdict for a single managed container.
type reconcileAction int

const (
	// actionNone means the DB record already agrees with reality; nothing to do.
	actionNone reconcileAction = iota
	// actionAdopt means a DB record exists but its ContainerID is stale or empty; it should be
	// updated to match the real container ID. The container itself is never touched.
	actionAdopt
	// actionOrphan means this installation owns the container but no DB record matches it,
	// and the operator explicitly enabled orphan purging. Only this case reaches phase 2.
	actionOrphan
	// actionQuarantine means the container has no matching DB record but is legacy or
	// auto-purge is disabled: report it, leave it running, never delete it.
	actionQuarantine
	// actionForeign means the container belongs to another Carbon Panel installation;
	// never adopt, stop or remove it.
	actionForeign
	// actionSkip means the container is managed but doesn't carry a server/module id label we
	// recognize; leave it alone rather than guessing.
	actionSkip
)

// planReconcile is the pure decision function behind phase 1. It takes no Docker/DB types so
// it can be tested directly: given a managed container, its installation ownership, what the
// DB knows about the entity it claims to be, and whether the operator enabled purging, it
// decides whether to adopt (fix drift), leave alone, purge, quarantine or skip.
func planReconcile(cont managedContainer, ourInstallationID string, dbRecordExists bool, dbContainerID string, purgeOrphans bool) reconcileAction {
	if cont.ServerID == "" && cont.ModuleID == "" {
		return actionSkip
	}

	ownership := classifyOwnership(cont.InstallationID, ourInstallationID)
	if ownership == ownershipForeign {
		return actionForeign
	}
	if dbRecordExists {
		if dbContainerID == cont.ID {
			return actionNone
		}
		return actionAdopt
	}
	// No DB record: only containers provably created by this installation are
	// eligible for automatic purge, and only when the operator opted in.
	if ownership == ownershipOwned && purgeOrphans {
		return actionOrphan
	}
	return actionQuarantine
}

// listManagedContainers lists all containers Carbon Panel manages and reduces them to the
// decoupled managedContainer view used by decideReconcile.
func (c *Client) listManagedContainers(ctx context.Context) ([]managedContainer, error) {
	ctx, cancel := boundedCall(ctx)
	defer cancel()

	filterArgs := make(client.Filters)
	filterArgs.Add("label", "carbon-panel.managed=true")

	containers, err := c.docker.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, err
	}

	result := make([]managedContainer, 0, len(containers.Items))
	for _, cont := range containers.Items {
		name := ""
		if len(cont.Names) > 0 {
			name = cont.Names[0]
		}
		result = append(result, managedContainer{
			ID:             cont.ID,
			Name:           name,
			Running:        cont.State == "running",
			InstallationID: cont.Labels[LabelInstallation],
			ServerID:       cont.Labels["carbon-panel.server.id"],
			ModuleID:       cont.Labels["carbon-panel.module.id"],
		})
	}
	return result, nil
}

// ReconcileOptions controls startup reconciliation behaviour.
type ReconcileOptions struct {
	// PurgeOrphans removes containers that this installation provably created but
	// the DB no longer knows about. It defaults to false: orphaned, legacy and
	// foreign containers are quarantined (reported, left running) unless an
	// operator explicitly opts in after verifying them.
	PurgeOrphans bool
}

// ReconcileAndCleanupContainers performs safe, two-phase startup reconciliation of containers
// Carbon Panel manages.
//
// Ownership: every container is classified by its carbon-panel.installation label. Containers
// created by another installation are never adopted, stopped or removed. Legacy containers
// (created before installation labels existed) may be adopted when a live DB record matches,
// because adoption only repairs the stored ContainerID; they are never purged automatically.
//
// Phase 1 (adoption): every container labeled carbon-panel.managed=true is matched back to a
// DB record via its carbon-panel.server.id / carbon-panel.module.id label. If the record exists
// but its stored ContainerID is stale or empty, it is updated to match reality (adoption). If no
// record exists at all for that ID, the container is quarantined or, only for containers owned
// by this installation with ReconcileOptions.PurgeOrphans set, queued for phase 2.
//
// Phase 2 (purge): only containers queued as owned orphans in phase 1 are stopped and removed,
// and ownership is revalidated against the daemon immediately before removal. Every container
// that matches a live DB record — even one whose ContainerID had drifted, now fixed in phase 1 —
// is left alone. Detached servers/modules (Server.Detached / Module.Detached) can never end up in
// the orphan set in the first place, since "detached" is a property of an existing DB record and
// orphan status requires the absence of one; they are therefore always safe from phase-2 purge
// by construction.
//
// Fail-safe: if the DB cannot be read (ListServers/ListModules error), reconciliation aborts
// entirely and returns an error without ever reaching phase 2. Proceeding with a partial or
// empty server/module list would make every real container look orphaned and destroy them.
func ReconcileAndCleanupContainers(ctx context.Context, store ReconcileStore, dockerClient *Client, log *logger.Logger, opts ReconcileOptions) error {
	// Fail-safe boot guard: if we can't reliably read the DB, abort before phase 2 ever runs.
	// Proceeding with a partial/empty server or module list would make every real container
	// look orphaned, since the ID lookups below would find nothing for everything.
	servers, err := store.ListServers(ctx)
	if err != nil {
		log.Error("Reconciliation aborted: failed to list servers, refusing to purge any containers: %v", err)
		return err
	}
	modules, err := store.ListModules(ctx)
	if err != nil {
		log.Error("Reconciliation aborted: failed to list modules, refusing to purge any containers: %v", err)
		return err
	}
	log.Info("Reconciling containers against %d known servers and %d known modules", len(servers), len(modules))

	// Look up by ID against the lists we just fetched (and already gated on error above) rather
	// than issuing a fresh per-container GetServer/GetModule call. Store.GetServer/GetModule
	// collapse "not found" and genuine transient DB errors into the same untyped error, which
	// would be unsafe to interpret as "orphan" here; a map built from an already-validated list
	// has no such ambiguity.
	serversByID := make(map[string]*db.Server, len(servers))
	for _, s := range servers {
		serversByID[s.ID] = s
	}
	modulesByID := make(map[string]*db.Module, len(modules))
	for _, m := range modules {
		modulesByID[m.ID] = m
	}

	containers, err := dockerClient.listManagedContainers(ctx)
	if err != nil {
		log.Error("Reconciliation aborted: failed to list managed containers: %v", err)
		return err
	}

	installationID := ""
	if dockerClient != nil {
		installationID = dockerClient.GetConfig().InstallationID
	}

	var orphans []managedContainer

	for _, cont := range containers {
		var (
			kind         string
			recordExists bool
			currentID    string
			adopt        func()
		)

		switch {
		case cont.ServerID != "":
			kind = "server"
			server, exists := serversByID[cont.ServerID]
			recordExists = exists
			if exists {
				currentID = server.ContainerID
				adopt = func() {
					log.Info("Adopting server container %s (%s): DB ContainerID %q -> %q", shortID(cont.ID), cont.Name, currentID, cont.ID)
					server.ContainerID = cont.ID
					if err := store.UpdateServer(ctx, server); err != nil {
						log.Error("Failed to adopt server container %s into DB record %s: %v", shortID(cont.ID), cont.ServerID, err)
					}
				}
			}

		case cont.ModuleID != "":
			kind = "module"
			mod, exists := modulesByID[cont.ModuleID]
			recordExists = exists
			if exists {
				currentID = mod.ContainerID
				adopt = func() {
					log.Info("Adopting module container %s (%s): DB ContainerID %q -> %q", shortID(cont.ID), cont.Name, currentID, cont.ID)
					mod.ContainerID = cont.ID
					if err := store.UpdateModule(ctx, mod); err != nil {
						log.Error("Failed to adopt module container %s into DB record %s: %v", shortID(cont.ID), cont.ModuleID, err)
					}
				}
			}

		default:
			log.Warn("Managed container %s (%s) has no recognizable server/module id label; leaving it alone", shortID(cont.ID), cont.Name)
			continue
		}

		switch planReconcile(cont, installationID, recordExists, currentID, opts.PurgeOrphans) {
		case actionAdopt:
			adopt()
		case actionOrphan:
			orphans = append(orphans, cont)
		case actionQuarantine:
			log.Warn("Quarantining %s container %s (%s): no matching DB record and automatic purge is disabled (or the container predates installation labels). Verify it, then set docker.reconcile_purge_orphans=true to remove it.", kind, shortID(cont.ID), cont.Name)
		case actionForeign:
			log.Warn("Ignoring %s container %s (%s): it belongs to another Carbon Panel installation (%s=%q)", kind, shortID(cont.ID), cont.Name, LabelInstallation, cont.InstallationID)
		case actionNone:
			// Already consistent; nothing to do.
		case actionSkip:
			// Unreachable: the switch above already skipped containers without an id label.
		}
	}

	for _, cont := range orphans {
		if err := dockerClient.confirmContainerOwnership(ctx, cont.ID, installationID); err != nil {
			log.Error("Refusing to remove orphaned container %s: %v", shortID(cont.ID), err)
			continue
		}

		log.Info("Removing orphaned container %s (%s) owned by this installation...", shortID(cont.ID), cont.Name)

		if cont.Running {
			timeout := 30
			if _, err := dockerClient.docker.ContainerStop(ctx, cont.ID, client.ContainerStopOptions{
				Timeout: &timeout,
			}); err != nil {
				log.Error("Failed to stop orphaned container %s: %v", shortID(cont.ID), err)
			}
		}

		if _, err := dockerClient.docker.ContainerRemove(ctx, cont.ID, client.ContainerRemoveOptions{
			Force: true,
		}); err != nil {
			log.Error("Failed to remove orphaned container %s: %v", shortID(cont.ID), err)
		} else {
			log.Info("Successfully removed orphaned container %s", shortID(cont.ID))
		}
	}

	return nil
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// confirmContainerOwnership revalidates a container's installation label against the
// daemon immediately before a destructive action. The startup inventory can be stale by
// the time phase 2 runs; this second look makes "remove" conditional on current proof.
func (c *Client) confirmContainerOwnership(ctx context.Context, containerID, ourInstallationID string) error {
	if strings.TrimSpace(ourInstallationID) == "" {
		return errors.New("no installation identity configured; refusing to remove any container")
	}
	ctx, cancel := boundedCall(ctx)
	defer cancel()

	result, err := c.docker.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspect %s: %w", shortID(containerID), err)
	}
	var labels map[string]string
	if result.Container.Config != nil {
		labels = result.Container.Config.Labels
	}
	if got := strings.TrimSpace(labels[LabelInstallation]); got != strings.TrimSpace(ourInstallationID) {
		return fmt.Errorf("container %s ownership changed (%s=%q, want %q)", shortID(containerID), LabelInstallation, got, ourInstallationID)
	}
	return nil
}
