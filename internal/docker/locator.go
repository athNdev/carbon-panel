// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	models "github.com/athNdev/carbon-panel/internal/db"
)

// Typed locator failures. Callers can distinguish an unknown node from a node
// whose daemon is temporarily unavailable; neither is ever (nil, nil).
var (
	ErrUnknownNode = errors.New("docker: unknown node")
	ErrNoClient    = errors.New("docker: no client available")
)

// Executor is the command-execution subset used by the command sender.
type Executor interface {
	ExecCommand(ctx context.Context, containerID string, command string) (string, error)
}

// Locator is the single adapter from a logical node to the Docker daemon or
// command executor that serves it. It owns local aliases, the standalone-client
// fallback, and store-backed local identity checks.
type Locator struct {
	pool          *ClientPool
	localClient   *Client
	localExecutor Executor
	store         NodeStore
}

// NewLocator returns a client locator. local may be nil when a pool is
// configured. store may be nil when the pool carries its own NodeStore.
func NewLocator(pool *ClientPool, local *Client, store NodeStore) *Locator {
	l := &Locator{pool: pool, localClient: local, store: store}
	if !hasNodeStore(l.store) && pool != nil && hasNodeStore(pool.store) {
		l.store = pool.store
	}
	if local != nil {
		l.localExecutor = local
	}
	return l
}

// NewExecutorLocator returns a locator for command executors. It exists because
// the command sender accepts a testable Executor rather than a concrete client.
func NewExecutorLocator(pool *ClientPool, local Executor, store NodeStore) *Locator {
	l := &Locator{pool: pool, localExecutor: local, store: store}
	if !hasNodeStore(l.store) && pool != nil && hasNodeStore(pool.store) {
		l.store = pool.store
	}
	return l
}

// For resolves the Docker client for nodeID. A specified remote node never
// falls back to the local daemon: the caller gets ErrUnknownNode instead.
func (l *Locator) For(ctx context.Context, nodeID string) (*Client, error) {
	if l == nil {
		return nil, ErrNoClient
	}
	if l.pool != nil {
		cli, err := l.pool.GetClientStrict(nodeID)
		if err == nil && cli != nil {
			return cli, nil
		}
		if err != nil && !l.isLocal(ctx, nodeID) {
			return nil, fmt.Errorf("%w: node %q: %v", ErrUnknownNode, nodeID, err)
		}
	}
	if l.isLocal(ctx, nodeID) {
		if l.localClient != nil {
			return l.localClient, nil
		}
		return nil, fmt.Errorf("%w: node %q", ErrNoClient, nodeID)
	}
	return nil, fmt.Errorf("%w: node %q", ErrUnknownNode, nodeID)
}

// ExecutorFor is For cast to the command-execution interface.
func (l *Locator) ExecutorFor(ctx context.Context, nodeID string) (Executor, error) {
	if l == nil {
		return nil, ErrNoClient
	}
	if l.pool != nil {
		cli, err := l.pool.GetClientStrict(nodeID)
		if err == nil && cli != nil {
			return cli, nil
		}
		if err != nil && !l.isLocal(ctx, nodeID) {
			return nil, fmt.Errorf("%w: node %q: %v", ErrUnknownNode, nodeID, err)
		}
	}
	if l.isLocal(ctx, nodeID) {
		if l.localExecutor != nil {
			return l.localExecutor, nil
		}
		if l.localClient != nil {
			return l.localClient, nil
		}
		return nil, fmt.Errorf("%w: node %q", ErrNoClient, nodeID)
	}
	return nil, fmt.Errorf("%w: node %q", ErrUnknownNode, nodeID)
}

// IsLocal reports whether nodeID addresses the controller's local daemon.
func (l *Locator) IsLocal(ctx context.Context, nodeID string) (bool, error) {
	if l == nil {
		return false, ErrNoClient
	}
	if l.isBuiltinLocal(nodeID) {
		return true, nil
	}
	node, err := l.node(ctx, nodeID)
	if err != nil {
		return false, err
	}
	return isLocalNode(node), nil
}

func (l *Locator) isLocal(ctx context.Context, nodeID string) bool {
	if l.isBuiltinLocal(nodeID) {
		return true
	}
	node, err := l.node(ctx, nodeID)
	return err == nil && isLocalNode(node)
}

func (l *Locator) isBuiltinLocal(nodeID string) bool {
	if nodeID == "" || nodeID == "local" {
		return true
	}
	if l.pool != nil {
		return nodeID == l.pool.GetDefaultNodeID()
	}
	return nodeID == "default"
}

func (l *Locator) node(ctx context.Context, nodeID string) (*models.Node, error) {
	if !hasNodeStore(l.store) || strings.TrimSpace(nodeID) == "" {
		return nil, fmt.Errorf("%w: node %q", ErrUnknownNode, nodeID)
	}
	node, err := l.store.GetNode(ctx, nodeID)
	if err != nil || node == nil {
		return nil, fmt.Errorf("%w: node %q: %v", ErrUnknownNode, nodeID, err)
	}
	return node, nil
}

func hasNodeStore(store NodeStore) bool {
	if store == nil {
		return false
	}
	v := reflect.ValueOf(store)
	return v.Kind() != reflect.Ptr || !v.IsNil()
}

func isLocalNode(node *models.Node) bool {
	if node == nil {
		return false
	}
	if node.IsLocal {
		return true
	}
	return node.Host == "" || strings.HasPrefix(node.Host, "unix://")
}
