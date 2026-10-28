// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	models "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// mockNodeStore implements NodeStore for unit tests
type mockNodeStore struct {
	mu    sync.Mutex
	nodes map[string]*models.Node
}

func newMockNodeStore(nodes ...*models.Node) *mockNodeStore {
	m := &mockNodeStore{
		nodes: make(map[string]*models.Node),
	}
	for _, n := range nodes {
		m.nodes[n.ID] = n
	}
	return m
}

func (m *mockNodeStore) GetNode(ctx context.Context, id string) (*models.Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, exists := m.nodes[id]
	if !exists {
		return nil, fmt.Errorf("node not found")
	}
	// Return a copy
	cp := *n
	return &cp, nil
}

func (m *mockNodeStore) ListNodes(ctx context.Context) ([]*models.Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []*models.Node
	for _, n := range m.nodes {
		cp := *n
		list = append(list, &cp)
	}
	return list, nil
}

func (m *mockNodeStore) UpdateNode(ctx context.Context, node *models.Node) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *node
	m.nodes[node.ID] = &cp
	return nil
}

func TestClientPool_DefaultClient(t *testing.T) {
	log := logger.New()
	mockClient := &Client{log: log}
	store := newMockNodeStore()

	pool := NewClientPool(store, mockClient, log)
	defer pool.Close()

	// GetDefaultClient should return mockClient
	def := pool.GetDefaultClient()
	if def != mockClient {
		t.Fatalf("expected mockClient as default client, got %v", def)
	}

	// GetClient("") should return default
	cEmpty, err := pool.GetClient("")
	if err != nil || cEmpty != mockClient {
		t.Fatalf("expected default client for empty node ID, got %v (err: %v)", cEmpty, err)
	}

	// GetClient("default") should return default
	cDef, err := pool.GetClient("default")
	if err != nil || cDef != mockClient {
		t.Fatalf("expected default client for 'default', got %v (err: %v)", cDef, err)
	}
}

func TestClientPool_Fallback(t *testing.T) {
	log := logger.New()
	mockClient := &Client{log: log}
	store := newMockNodeStore()

	pool := NewClientPool(store, mockClient, log)
	defer pool.Close()

	// By default, fallback is disabled for security and correctness
	if pool.IsFallbackEnabled() {
		t.Fatalf("expected fallback to be disabled by default")
	}

	// Requesting an unknown node with fallback disabled should return an explicit error
	_, err := pool.GetClient("nonexistent-node-123")
	if err == nil {
		t.Fatalf("expected error requesting unknown node with fallback disabled, got nil")
	}

	// A named node still fails when the historical fallback flag is set.
	pool.SetFallbackEnabled(true)
	if !pool.IsFallbackEnabled() {
		t.Fatalf("expected fallback flag to be stored after SetFallbackEnabled(true)")
	}

	fallbackCli, err := pool.GetClient("nonexistent-node-123")
	if err == nil || fallbackCli == mockClient {
		t.Fatalf("named node returned the local client with fallback enabled: %v %v", fallbackCli, err)
	}

	// Strict get should ALWAYS fail and NOT fall back, regardless of allowFallback setting
	_, err = pool.GetClientStrict("nonexistent-node-123")
	if err == nil {
		t.Fatalf("expected error from GetClientStrict for missing node, got nil")
	}
}

func TestClientPool_RegisterAndRemoveNode(t *testing.T) {
	log := logger.New()
	defaultCli := &Client{log: log}
	customCli := &Client{log: log}
	store := newMockNodeStore()

	pool := NewClientPool(store, defaultCli, log)
	defer pool.Close()

	pool.RegisterClient("custom-node", customCli)

	got, err := pool.GetClient("custom-node")
	if err != nil || got != customCli {
		t.Fatalf("expected customCli, got %v (err: %v)", got, err)
	}

	// Trying to remove default node should error
	if err := pool.RemoveNode("default"); err == nil {
		t.Fatalf("expected error removing default node, got nil")
	}

	// Removing custom node should succeed
	if err := pool.RemoveNode("custom-node"); err != nil {
		t.Fatalf("failed to remove custom node: %v", err)
	}

	// Subsequent GetClient for custom-node returns an error with fallback disabled
	_, err = pool.GetClient("custom-node")
	if err == nil {
		t.Fatalf("expected error requesting removed node with fallback disabled, got nil")
	}

	// A removed named node is not served by the local client.
	pool.SetFallbackEnabled(true)
	afterRemove, err := pool.GetClient("custom-node")
	if err == nil || afterRemove == defaultCli {
		t.Fatalf("removed node returned the local client: %v %v", afterRemove, err)
	}
}

func TestGetClientStrictDropsAClientWhenTheEndpointChanges(t *testing.T) {
	log := logger.New()
	stale := &Client{log: log, host: "tcp://10.0.0.2:2375"}
	store := newMockNodeStore(&models.Node{
		ID: "remote-b", Name: "B", Host: "tcp://10.0.0.2:2375", Enabled: true,
	})
	pool := NewClientPool(store, &Client{log: log}, log)
	t.Cleanup(func() { _ = pool.Close() })

	pool.mu.Lock()
	pool.clients["remote-b"] = stale
	recorded := *store.nodes["remote-b"]
	pool.nodeConfigs["remote-b"] = &recorded
	pool.mu.Unlock()

	got, err := pool.GetClientStrict("remote-b")
	if err != nil || got != stale {
		t.Fatalf("unchanged endpoint = %v %v, want the cached client", got, err)
	}

	store.mu.Lock()
	store.nodes["remote-b"].Host = "tcp://10.0.0.3:2375"
	store.mu.Unlock()

	got, err = pool.GetClientStrict("remote-b")
	if err != nil {
		t.Fatalf("rebuild after endpoint change: %v", err)
	}
	if got == stale || got.host != "tcp://10.0.0.3:2375" {
		t.Fatalf("client after endpoint change = host %q, want the new endpoint", got.host)
	}
}

func TestGetClientStrictDropsAClientWhenTheGenerationChanges(t *testing.T) {
	log := logger.New()
	stale := &Client{log: log, host: "tcp://10.0.0.2:2375"}
	store := newMockNodeStore(&models.Node{
		ID: "remote-b", Name: "B", Host: "tcp://10.0.0.2:2375", Enabled: true, EndpointGeneration: 1,
	})
	pool := NewClientPool(store, &Client{log: log}, log)
	t.Cleanup(func() { _ = pool.Close() })

	pool.mu.Lock()
	pool.clients["remote-b"] = stale
	recorded := *store.nodes["remote-b"]
	pool.nodeConfigs["remote-b"] = &recorded
	pool.mu.Unlock()

	store.mu.Lock()
	store.nodes["remote-b"].EndpointGeneration = 2
	store.mu.Unlock()

	got, err := pool.GetClientStrict("remote-b")
	if err != nil {
		t.Fatalf("rebuild after generation change: %v", err)
	}
	if got == stale {
		t.Fatal("generation change reused the cached client")
	}
}

func TestPingNodeAcceptsTheFirstDaemonAndRefusesAReplacement(t *testing.T) {
	log := logger.New()
	remote := &Client{log: log}
	store := newMockNodeStore(&models.Node{
		ID: "remote-b", Name: "B", Host: "tcp://10.0.0.2:2375", Enabled: true,
		Status: models.NodeStatusOffline, EndpointGeneration: 1,
	})
	pool := NewClientPool(store, &Client{log: log}, log)
	t.Cleanup(func() { _ = pool.Close() })
	pool.RegisterClient("remote-b", remote)

	origPing := pingDocker
	origID := readDaemonIdentity
	t.Cleanup(func() {
		pingDocker = origPing
		readDaemonIdentity = origID
	})
	pingDocker = func(context.Context, *Client) error { return nil }
	readDaemonIdentity = func(context.Context, *Client) (string, error) { return "daemon-a", nil }

	ok, err := pool.PingNode(context.Background(), "remote-b")
	if err != nil || !ok {
		t.Fatalf("first ping = %v %v", ok, err)
	}
	got, err := store.GetNode(context.Background(), "remote-b")
	if err != nil || got.DaemonID != "daemon-a" || got.Status != models.NodeStatusOnline || got.EndpointGeneration != 1 {
		t.Fatalf("node after accept = %+v %v", got, err)
	}

	readDaemonIdentity = func(context.Context, *Client) (string, error) { return "daemon-b", nil }
	ok, err = pool.PingNode(context.Background(), "remote-b")
	if ok || !errors.Is(err, ErrDaemonIdentity) {
		t.Fatalf("replacement ping = %v %v, want ErrDaemonIdentity", ok, err)
	}
	got, err = store.GetNode(context.Background(), "remote-b")
	if err != nil || got.DaemonID != "daemon-a" || got.Status != models.NodeStatusOffline {
		t.Fatalf("node after mismatch = %+v %v", got, err)
	}
	pool.mu.RLock()
	_, stillCached := pool.clients["remote-b"]
	pool.mu.RUnlock()
	if stillCached {
		t.Fatal("mismatched daemon kept the cached client")
	}
}

func TestResolveTLSCertFiles(t *testing.T) {
	// Case 1: Empty TLS fields
	nodeEmpty := &models.Node{ID: "node-empty"}
	ca, cert, key, err := resolveTLSCertFiles(nodeEmpty)
	if err != nil || ca != "" || cert != "" || key != "" {
		t.Fatalf("expected empty paths, got %s, %s, %s, err=%v", ca, cert, key, err)
	}

	// Case 2: PEM strings
	pemData := "-----BEGIN CERTIFICATE-----\nMIIDXTCCAkWgAwIBAgIJ...\n-----END CERTIFICATE-----"
	pemKey := "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA...\n-----END RSA PRIVATE KEY-----"

	nodePEM := &models.Node{
		ID:        "node-pem-test",
		TLSCACert: pemData,
		TLSCert:   pemData,
		TLSKey:    pemKey,
	}

	ca, cert, key, err = resolveTLSCertFiles(nodePEM)
	if err != nil {
		t.Fatalf("unexpected error resolving PEM certs: %v", err)
	}
	if ca == "" || cert == "" || key == "" {
		t.Fatalf("expected non-empty cert file paths, got %s, %s, %s", ca, cert, key)
	}

	// Verify files actually exist on disk
	content, err := os.ReadFile(ca)
	if err != nil || string(content) != pemData {
		t.Fatalf("ca file content mismatch or error: %v", err)
	}

	// Case 3: Existing file paths
	tmpDir := t.TempDir()
	caPath := filepath.Join(tmpDir, "custom-ca.pem")
	if err := os.WriteFile(caPath, []byte("test-ca"), 0644); err != nil {
		t.Fatal(err)
	}

	nodePath := &models.Node{
		ID:        "node-path-test",
		TLSCACert: caPath,
	}
	ca2, _, _, err := resolveTLSCertFiles(nodePath)
	if err != nil || ca2 != caPath {
		t.Fatalf("expected file path passthrough %s, got %s, err=%v", caPath, ca2, err)
	}
}

func TestClientPool_Concurrency(t *testing.T) {
	log := logger.New()
	mockClient := &Client{log: log}
	store := newMockNodeStore(
		&models.Node{ID: "node-1", Host: "unix:///var/run/docker.sock"},
		&models.Node{ID: "node-2", Host: "unix:///var/run/docker.sock"},
	)

	pool := NewClientPool(store, mockClient, log)
	defer pool.Close()

	pool.RegisterClient("node-1", &Client{log: log})
	pool.RegisterClient("node-2", &Client{log: log})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			nodeID := fmt.Sprintf("node-%d", (idx%2)+1)
			cli, err := pool.GetClient(nodeID)
			if err != nil || cli == nil {
				t.Errorf("GetClient(%s) failed: %v", nodeID, err)
			}
			def := pool.GetDefaultClient()
			if def == nil {
				t.Errorf("GetDefaultClient returned nil")
			}
		}(i)
	}
	wg.Wait()
}

func TestClientPool_HealthChecker(t *testing.T) {
	log := logger.New()
	store := newMockNodeStore(
		&models.Node{ID: "node-disabled", Enabled: false, Status: models.NodeStatusOffline},
		&models.Node{ID: "node-nonexistent", Enabled: true, Status: models.NodeStatusOnline, Host: "tcp://127.0.0.1:59999"},
	)

	pool := NewClientPool(store, nil, log)
	defer pool.Close()

	// PingNode on node-nonexistent should set status to offline
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ok, err := pool.PingNode(ctx, "node-nonexistent")
	if ok || err == nil {
		t.Fatalf("expected ping failure on nonexistent host, got ok=%v, err=%v", ok, err)
	}

	updated, err := store.GetNode(ctx, "node-nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != models.NodeStatusOffline {
		t.Fatalf("expected node status to be offline, got %s", updated.Status)
	}

	// Start and stop health checker cleanly
	pool.StartHealthChecker(100 * time.Millisecond)
	// Calling StartHealthChecker again should be a no-op
	pool.StartHealthChecker(100 * time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	pool.StopHealthChecker()
	// Calling StopHealthChecker again should be safe
	pool.StopHealthChecker()
}

func TestClientPool_Stats(t *testing.T) {
	log := logger.New()
	mockClient := &Client{log: log}
	store := newMockNodeStore(&models.Node{ID: "node-1", Host: "unix:///var/run/docker.sock"})

	pool := NewClientPool(store, mockClient, log)
	defer pool.Close()

	// Initial stats
	stats := pool.Stats()
	if stats.TotalRequests != 0 || stats.CacheHits != 0 || stats.CacheMisses != 0 || stats.Fallbacks != 0 || stats.CreateErrors != 0 {
		t.Fatalf("expected all stats zero, got %+v", stats)
	}

	// Request default
	_, _ = pool.GetClient("")
	stats = pool.Stats()
	if stats.TotalRequests != 1 || stats.CacheHits != 1 {
		t.Fatalf("expected 1 request, 1 hit, got %+v", stats)
	}

	// Request registered
	pool.RegisterClient("node-custom", &Client{log: log})
	_, _ = pool.GetClient("node-custom")
	stats = pool.Stats()
	if stats.TotalRequests != 2 || stats.CacheHits != 2 {
		t.Fatalf("expected 2 requests, 2 hits, got %+v", stats)
	}

	// A missing named node is a miss, not a fallback to the local daemon.
	pool.SetFallbackEnabled(true)
	_, _ = pool.GetClient("nonexistent")
	stats = pool.Stats()
	if stats.TotalRequests != 3 || stats.CacheMisses != 1 || stats.Fallbacks != 0 {
		t.Fatalf("expected 3 requests, 1 miss, 0 fallbacks, got %+v", stats)
	}
}

func TestClientPool_LocalNodeReturnsDefaultClient(t *testing.T) {
	log := logger.New()
	defaultCli := &Client{log: log}
	localNode := &models.Node{
		ID:      "uuid-local-node-1234",
		Name:    "Local Node",
		IsLocal: true,
		Host:    "unix:///var/run/docker.sock",
	}
	store := newMockNodeStore(localNode)
	pool := NewClientPool(store, defaultCli, log)
	defer func() { _ = pool.Close() }()

	// GetClient should return defaultCli
	cli, err := pool.GetClient("uuid-local-node-1234")
	if err != nil {
		t.Fatalf("GetClient failed: %v", err)
	}
	if cli != defaultCli {
		t.Fatalf("expected defaultClient, got %v", cli)
	}

	// GetClientStrict should return defaultCli
	cliStrict, err := pool.GetClientStrict("uuid-local-node-1234")
	if err != nil {
		t.Fatalf("GetClientStrict failed: %v", err)
	}
	if cliStrict != defaultCli {
		t.Fatalf("expected defaultClient, got %v", cliStrict)
	}
}

func TestClientPool_BaseConfigInheritance(t *testing.T) {
	log := logger.New()
	defaultCli := &Client{
		log: log,
		config: ClientConfig{
			NetworkName: "custom-net",
			NetworkMTU:  1280,
		},
	}
	pool := NewClientPool(nil, defaultCli, log)
	defer func() { _ = pool.Close() }()

	if pool.baseConfig.NetworkName != "custom-net" {
		t.Fatalf("expected custom-net, got %s", pool.baseConfig.NetworkName)
	}
	if pool.baseConfig.NetworkMTU != 1280 {
		t.Fatalf("expected MTU 1280, got %d", pool.baseConfig.NetworkMTU)
	}
}
