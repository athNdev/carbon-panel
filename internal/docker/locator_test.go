// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"errors"
	"testing"

	models "github.com/athNdev/carbon-panel/internal/db"
)

type locatorStore struct {
	nodes map[string]*models.Node
	err   error
}

func (s *locatorStore) GetNode(_ context.Context, id string) (*models.Node, error) {
	if s.err != nil {
		return nil, s.err
	}
	node, ok := s.nodes[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return node, nil
}

func (s *locatorStore) ListNodes(context.Context) ([]*models.Node, error) { return nil, nil }
func (s *locatorStore) UpdateNode(context.Context, *models.Node) error    { return nil }

type locatorExecutor struct{ calls int }

func (e *locatorExecutor) ExecCommand(context.Context, string, string) (string, error) {
	e.calls++
	return "", nil
}

func TestLocatorFor(t *testing.T) {
	ctx := context.Background()
	local := &Client{}
	remote := &Client{}
	store := &locatorStore{nodes: map[string]*models.Node{
		"local-id":  {ID: "local-id", IsLocal: true},
		"remote-id": {ID: "remote-id", Host: "tcp://10.0.0.5:2376"},
	}}
	pool := NewClientPool(store, local, nil)
	pool.RegisterClient("remote-id", remote)

	tests := []struct {
		name    string
		locator *Locator
		nodeID  string
		want    *Client
		wantErr error
	}{
		{name: "empty alias", locator: NewLocator(pool, local, store), nodeID: ""},
		{name: "local alias", locator: NewLocator(pool, local, store), nodeID: "local"},
		{name: "default alias", locator: NewLocator(pool, local, store), nodeID: "default"},
		{name: "stored local node", locator: NewLocator(pool, local, store), nodeID: "local-id"},
		{name: "registered remote node", locator: NewLocator(pool, local, store), nodeID: "remote-id", want: remote},
		{name: "missing remote node", locator: NewLocator(pool, local, store), nodeID: "missing", wantErr: ErrUnknownNode},
		{name: "remote without pool", locator: NewLocator(nil, local, store), nodeID: "remote-id", wantErr: ErrUnknownNode},
		{name: "local without client", locator: NewLocator(nil, nil, store), nodeID: "local", wantErr: ErrNoClient},
		{name: "typed nil store does not panic", locator: NewLocator(nil, local, (*locatorStore)(nil)), nodeID: "remote-id", wantErr: ErrUnknownNode},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.locator.For(ctx, tt.nodeID)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("For(%q) error = %v", tt.nodeID, err)
			}
			if tt.want != nil && got != tt.want {
				t.Fatalf("For(%q) = %p, want %p", tt.nodeID, got, tt.want)
			}
			if tt.want == nil && got != local {
				t.Fatalf("For(%q) = %p, want local %p", tt.nodeID, got, local)
			}
		})
	}
}

func TestLocatorExecutorFor(t *testing.T) {
	ctx := context.Background()
	local := &locatorExecutor{}
	loc := NewExecutorLocator(nil, local, nil)
	got, err := loc.ExecutorFor(ctx, "default")
	if err != nil {
		t.Fatalf("ExecutorFor(default) error = %v", err)
	}
	if got != local {
		t.Fatalf("ExecutorFor(default) = %T, want local executor", got)
	}

	if _, err := loc.ExecutorFor(ctx, "remote"); !errors.Is(err, ErrUnknownNode) {
		t.Fatalf("ExecutorFor(remote) error = %v, want ErrUnknownNode", err)
	}
}

func TestLocatorIsLocal(t *testing.T) {
	ctx := context.Background()
	store := &locatorStore{nodes: map[string]*models.Node{
		"local-id":  {ID: "local-id", IsLocal: true},
		"remote-id": {ID: "remote-id", Host: "tcp://10.0.0.5:2376"},
	}}
	loc := NewLocator(nil, &Client{}, store)

	for _, nodeID := range []string{"", "local", "default", "local-id"} {
		got, err := loc.IsLocal(ctx, nodeID)
		if err != nil || !got {
			t.Fatalf("IsLocal(%q) = %v, %v; want true, nil", nodeID, got, err)
		}
	}
	got, err := loc.IsLocal(ctx, "remote-id")
	if err != nil || got {
		t.Fatalf("IsLocal(remote-id) = %v, %v; want false, nil", got, err)
	}
	if _, err := loc.IsLocal(ctx, "missing"); !errors.Is(err, ErrUnknownNode) {
		t.Fatalf("IsLocal(missing) error = %v, want ErrUnknownNode", err)
	}
}
