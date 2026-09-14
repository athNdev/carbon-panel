package docker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	models "github.com/athNdev/mineserver/internal/db"
	"github.com/athNdev/mineserver/pkg/logger"
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

	// Requesting an unknown node should gracefully fall back to default
	fallbackCli, err := pool.GetClient("nonexistent-node-123")
	if err != nil {
		t.Fatalf("expected graceful fallback, got error: %v", err)
	}
	if fallbackCli != mockClient {
		t.Fatalf("expected default client on fallback, got %v", fallbackCli)
	}

	// Strict get should fail and NOT fall back
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

	// Subsequent GetClient for custom-node falls back to default
	afterRemove, err := pool.GetClient("custom-node")
	if err != nil || afterRemove != defaultCli {
		t.Fatalf("expected fallback to default after removal, got %v (err: %v)", afterRemove, err)
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
