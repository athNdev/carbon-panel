package docker

import (
	"context"

	"github.com/docker/docker/client"

	models "github.com/athNdev/carbon-panel/internal/db"
)

// ContainerLookup resolves a server by the Docker container ID that backs it.
// *db.Store satisfies this via the MINE-103 reverse lookup.
type ContainerLookup interface {
	GetServerByContainerID(ctx context.Context, containerID string) (*models.Server, error)
}

// ContainerLogClientResolver attributes a container to its node and returns that
// node's Docker SDK client, so the log streamer can tail containers on remote
// nodes instead of silently inspecting the local daemon (MINE-108).
//
// A container it cannot attribute (e.g. a module container, which has no server
// record) yields (nil, nil), which the streamer treats as "use the local
// client" — preserving the previous single-node behaviour.
type ContainerLogClientResolver struct {
	store ContainerLookup
	pool  *ClientPool
}

// NewContainerLogClientResolver constructs a resolver over a node-aware
// ClientPool.
func NewContainerLogClientResolver(store ContainerLookup, pool *ClientPool) *ContainerLogClientResolver {
	return &ContainerLogClientResolver{store: store, pool: pool}
}

// ResolveLogClient implements logger.LogClientResolver.
func (r *ContainerLogClientResolver) ResolveLogClient(ctx context.Context, containerID string) (*client.Client, error) {
	if r == nil || r.store == nil || r.pool == nil || containerID == "" {
		return nil, nil
	}

	server, err := r.store.GetServerByContainerID(ctx, containerID)
	if err != nil || server == nil {
		return nil, nil
	}

	cli, err := r.pool.GetClientStrict(server.NodeID)
	if err != nil {
		return nil, err
	}
	if cli == nil {
		return nil, nil
	}

	return cli.GetDockerClient(), nil
}
