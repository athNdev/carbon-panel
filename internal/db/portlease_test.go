// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package db

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
)

func TestReserveBackendPortPairDistinctAndReleased(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	gameA, rconA, err := store.ReserveBackendPortPair(ctx, "node-a", "server-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	gameB, _, err := store.ReserveBackendPortPair(ctx, "node-a", "server-b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if gameA == gameB || gameA == rconA || gameB == rconA {
		t.Fatalf("ports collided: a=%d/%d b=%d", gameA, rconA, gameB)
	}
	if rconA != gameA+10 {
		t.Fatalf("rcon = %d, want %d", rconA, gameA+10)
	}

	other, _, err := store.ReserveBackendPortPair(ctx, "node-b", "server-c", nil)
	if err != nil {
		t.Fatal(err)
	}
	if other != gameA {
		t.Fatalf("other node got %d, want the same first port %d", other, gameA)
	}

	if err := store.ReleaseBackendPorts(ctx, "server-a"); err != nil {
		t.Fatal(err)
	}
	again, _, err := store.ReserveBackendPortPair(ctx, "node-a", "server-d", nil)
	if err != nil {
		t.Fatal(err)
	}
	if again != gameA {
		t.Fatalf("released port %d was not reused, got %d", gameA, again)
	}
}

func TestReserveBackendPortPairSkipsDockerAndServerPorts(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	if err := store.CreateServer(ctx, &Server{
		ID: "held", Name: "held", ModLoader: ModLoaderVanilla, MCVersion: "1.21",
		NodeID: "node-a", Status: StatusStopped, Port: 25566, DataPath: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	game, _, err := store.ReserveBackendPortPair(ctx, "node-a", "server-new", map[int]bool{25567: true, 25577: true})
	if err != nil {
		t.Fatal(err)
	}
	if game == 25566 || game == 25567 {
		t.Fatalf("allocated a used port %d", game)
	}
}

func TestReserveBackendPortPairConcurrent(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	const n = 8
	ports := make([]int, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ports[i], _, errs[i] = store.ReserveBackendPortPair(ctx, "node-a", fmt.Sprintf("server-%d", i), nil)
		}(i)
	}
	wg.Wait()
	seen := map[int]bool{}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if seen[ports[i]] {
			t.Fatalf("duplicate port %d", ports[i])
		}
		seen[ports[i]] = true
	}
}

func TestAdditionalPortLeaseAllowsTheOtherProtocol(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	t.Cleanup(func() { _ = store.Close() })

	game, _, err := store.ReserveBackendPortPair(ctx, "node-a", "server-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceAdditionalPortLeases(ctx, "node-a", "server-b", []HostPort{{Port: game, Protocol: "udp"}}); err != nil {
		t.Fatalf("UDP lease of the TCP game port: %v", err)
	}
	err = store.ReplaceAdditionalPortLeases(ctx, "node-a", "server-c", []HostPort{{Port: game, Protocol: "tcp"}})
	if !errors.Is(err, ErrPortLeaseHeld) {
		t.Fatalf("TCP lease error = %v, want ErrPortLeaseHeld", err)
	}
	if err := store.ReleaseBackendPorts(ctx, "server-b"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceAdditionalPortLeases(ctx, "node-a", "server-d", []HostPort{{Port: game, Protocol: "udp"}}); err != nil {
		t.Fatalf("released UDP port was not reusable: %v", err)
	}
}

func TestMovePortLeasesStopsWhenTheTargetHoldsTheBind(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	t.Cleanup(func() { _ = store.Close() })

	if _, _, err := store.ReserveBackendPortPair(ctx, "node-a", "server-a", nil); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceAdditionalPortLeases(ctx, "node-b", "server-b", []HostPort{{Port: 25566, Protocol: "tcp"}}); err != nil {
		t.Fatal(err)
	}
	err := store.MovePortLeases(ctx, "server-a", "node-b")
	if !errors.Is(err, ErrPortLeaseHeld) {
		t.Fatalf("move error = %v, want ErrPortLeaseHeld", err)
	}
	var stayed []BackendPortLease
	if err := store.DB().Where("server_id = ?", "server-a").Find(&stayed).Error; err != nil {
		t.Fatal(err)
	}
	if len(stayed) == 0 || stayed[0].NodeID != "node-a" {
		t.Fatalf("source leases = %+v, want them kept on node-a", stayed)
	}

	if err := store.ReleaseBackendPorts(ctx, "server-b"); err != nil {
		t.Fatal(err)
	}
	if err := store.MovePortLeases(ctx, "server-a", "node-b"); err != nil {
		t.Fatal(err)
	}
	var moved []BackendPortLease
	if err := store.DB().Where("server_id = ?", "server-a").Find(&moved).Error; err != nil {
		t.Fatal(err)
	}
	for _, lease := range moved {
		if lease.NodeID != "node-b" || lease.Protocol != "tcp" {
			t.Fatalf("moved lease = %+v, want node-b tcp", lease)
		}
	}
}

func TestLoopbackLeaseDoesNotOccupyIPv6(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	t.Cleanup(func() { _ = store.Close() })

	if err := store.ReplaceAdditionalPortLeases(ctx, "node-a", "server-v4", []HostPort{{
		Port: 25570, Protocol: "tcp", Address: "127.0.0.1",
	}}); err != nil {
		t.Fatal(err)
	}
	err := store.ReplaceAdditionalPortLeases(ctx, "node-a", "server-wild", []HostPort{{
		Port: 25570, Protocol: "tcp", Address: "0.0.0.0",
	}})
	if !errors.Is(err, ErrPortLeaseHeld) {
		t.Fatalf("wildcard error = %v, want ErrPortLeaseHeld", err)
	}
	if err := store.ReplaceAdditionalPortLeases(ctx, "node-a", "server-v6", []HostPort{{
		Port: 25570, Protocol: "tcp", Address: "::",
	}}); err != nil {
		t.Fatalf("IPv6 wildcard should not overlap an IPv4 loopback lease: %v", err)
	}
}

func TestPortLeaseAddressMigrationKeepsWildcardOverlap(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Path:        filepath.Join(t.TempDir(), "addr.db"),
			AutoMigrate: false,
		},
	}
	store, err := NewSQLiteStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.DB().Exec(`CREATE TABLE backend_port_leases (
		node_id text NOT NULL,
		port integer NOT NULL,
		protocol text NOT NULL DEFAULT 'tcp',
		server_id text,
		purpose text NOT NULL DEFAULT '',
		PRIMARY KEY (node_id, port, protocol)
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB().Exec(`INSERT INTO backend_port_leases (node_id, port, protocol, server_id, purpose) VALUES ('node-a', 25566, 'tcp', 'server-old', '')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}
	err = store.ReplaceAdditionalPortLeases(ctx, "node-a", "server-loop", []HostPort{{
		Port: 25566, Protocol: "tcp", Address: "127.0.0.1",
	}})
	if !errors.Is(err, ErrPortLeaseHeld) {
		t.Fatalf("loopback beside a migrated wildcard = %v, want ErrPortLeaseHeld", err)
	}
	if err := store.ReplaceAdditionalPortLeases(ctx, "node-a", "server-v6", []HostPort{{
		Port: 25566, Protocol: "tcp", Address: "::",
	}}); err != nil {
		t.Fatalf("IPv6 beside a migrated IPv4 wildcard: %v", err)
	}
}

func TestPortLeaseProtocolMigrationKeepsExistingTCPLease(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Path:        filepath.Join(t.TempDir(), "test.db"),
			AutoMigrate: false,
		},
	}
	store, err := NewSQLiteStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.DB().Exec(`CREATE TABLE backend_port_leases (
		node_id text NOT NULL,
		port integer NOT NULL,
		server_id text,
		PRIMARY KEY (node_id, port)
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB().Exec(`INSERT INTO backend_port_leases (node_id, port, server_id) VALUES ('node-a', 25566, 'server-old')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}

	if err := store.ReplaceAdditionalPortLeases(ctx, "node-a", "server-udp", []HostPort{{Port: 25566, Protocol: "udp"}}); err != nil {
		t.Fatalf("UDP lease beside a migrated TCP row: %v", err)
	}
	err = store.ReplaceAdditionalPortLeases(ctx, "node-a", "server-tcp", []HostPort{{Port: 25566, Protocol: "tcp"}})
	if !errors.Is(err, ErrPortLeaseHeld) {
		t.Fatalf("migrated TCP lease error = %v, want ErrPortLeaseHeld", err)
	}
	game, _, err := store.ReserveBackendPortPair(ctx, "node-a", "server-new", nil)
	if err != nil {
		t.Fatal(err)
	}
	if game == 25566 {
		t.Fatal("backend pair reused the migrated TCP lease")
	}
}
