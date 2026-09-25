package docker

import (
	"context"

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
	ID       string
	Name     string
	Running  bool
	ServerID string // from the carbon-panel.server.id label; empty if this isn't a server container
	ModuleID string // from the carbon-panel.module.id label; empty if this isn't a module container
}

// reconcileAction is the phase-1 verdict for a single managed container.
type reconcileAction int

const (
	// actionNone means the DB record already agrees with reality; nothing to do.
	actionNone reconcileAction = iota
	// actionAdopt means a DB record exists but its ContainerID is stale or empty; it should be
	// updated to match the real container ID. The container itself is never touched.
	actionAdopt
	// actionOrphan means no DB record could be found for the label on this container. It is a
	// genuine orphan and is the only case eligible for phase-2 purge.
	actionOrphan
	// actionSkip means the container is managed but doesn't carry a server/module id label we
	// recognize; leave it alone rather than guessing.
	actionSkip
)

// decideReconcile is the pure decision function behind phase 1. It takes no Docker/DB types so
// it can be tested directly: given a managed container and whether/what the DB currently knows
// about the entity it claims to be, it decides whether to adopt (fix drift), leave alone, mark
// as an orphan for phase-2 purge, or skip (label we don't recognize).
func decideReconcile(cont managedContainer, dbRecordExists bool, dbContainerID string) reconcileAction {
	if cont.ServerID == "" && cont.ModuleID == "" {
		return actionSkip
	}
	if !dbRecordExists {
		return actionOrphan
	}
	if dbContainerID == cont.ID {
		return actionNone
	}
	return actionAdopt
}

// listManagedContainers lists all containers Carbon Panel manages and reduces them to the
// decoupled managedContainer view used by decideReconcile.
func (c *Client) listManagedContainers(ctx context.Context) ([]managedContainer, error) {
	filters := client.Filters{}.Add("label", "carbon-panel.managed=true")

	res, err := c.docker.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: filters,
	})
	if err != nil {
		return nil, err
	}

	result := make([]managedContainer, 0, len(res.Items))
	for _, cont := range res.Items {
		name := ""
		if len(cont.Names) > 0 {
			name = cont.Names[0]
		}
		result = append(result, managedContainer{
			ID:       cont.ID,
			Name:     name,
			Running:  cont.State == "running",
			ServerID: cont.Labels["carbon-panel.server.id"],
			ModuleID: cont.Labels["carbon-panel.module.id"],
		})
	}
	return result, nil
}

// ReconcileAndCleanupContainers performs safe, two-phase startup reconciliation of containers
// Carbon Panel manages.
//
// Phase 1 (adoption): every container labeled carbon-panel.managed=true is matched back to a
// DB record via its carbon-panel.server.id / carbon-panel.module.id label. If the record exists
// but its stored ContainerID is stale or empty, it is updated to match reality (adoption). If no
// record exists at all for that ID, the container is a genuine orphan and is queued for phase 2
// — it is never deleted in phase 1.
//
// Phase 2 (purge): only containers queued as genuine orphans in phase 1 are stopped and removed.
// Every container that matches a live DB record — even one whose ContainerID had drifted, now
// fixed in phase 1 — is left alone. Detached servers/modules (Server.Detached / Module.Detached)
// can never end up in the orphan set in the first place, since "detached" is a property of an
// existing DB record and orphan status requires the absence of one; they are therefore always
// safe from phase-2 purge by construction.
//
// Fail-safe: if the DB cannot be read (ListServers/ListModules error), reconciliation aborts
// entirely and returns an error without ever reaching phase 2. Proceeding with a partial or
// empty server/module list would make every real container look orphaned and destroy them.
func ReconcileAndCleanupContainers(ctx context.Context, store ReconcileStore, dockerClient *Client, log *logger.Logger) error {
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

	var orphans []managedContainer

	for _, cont := range containers {
		switch {
		case cont.ServerID != "":
			server, exists := serversByID[cont.ServerID]
			var currentID string
			if exists {
				currentID = server.ContainerID
			}

			switch decideReconcile(cont, exists, currentID) {
			case actionAdopt:
				log.Info("Adopting server container %s (%s): DB ContainerID %q -> %q", shortID(cont.ID), cont.Name, currentID, cont.ID)
				server.ContainerID = cont.ID
				if err := store.UpdateServer(ctx, server); err != nil {
					log.Error("Failed to adopt server container %s into DB record %s: %v", shortID(cont.ID), cont.ServerID, err)
				}
			case actionOrphan:
				orphans = append(orphans, cont)
			case actionNone:
				// Already consistent; nothing to do.
			}

		case cont.ModuleID != "":
			mod, exists := modulesByID[cont.ModuleID]
			var currentID string
			if exists {
				currentID = mod.ContainerID
			}

			switch decideReconcile(cont, exists, currentID) {
			case actionAdopt:
				log.Info("Adopting module container %s (%s): DB ContainerID %q -> %q", shortID(cont.ID), cont.Name, currentID, cont.ID)
				mod.ContainerID = cont.ID
				if err := store.UpdateModule(ctx, mod); err != nil {
					log.Error("Failed to adopt module container %s into DB record %s: %v", shortID(cont.ID), cont.ModuleID, err)
				}
			case actionOrphan:
				orphans = append(orphans, cont)
			case actionNone:
				// Already consistent; nothing to do.
			}

		default:
			log.Warn("Managed container %s (%s) has no recognizable server/module id label; leaving it alone", shortID(cont.ID), cont.Name)
		}
	}

	for _, cont := range orphans {
		log.Info("Found orphaned container %s (%s), removing...", shortID(cont.ID), cont.Name)

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
