package docker

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/client"
	"github.com/docker/go-connections/tlsconfig"
	models "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// NodeStore defines the DB interface needed by ClientPool
type NodeStore interface {
	GetNode(ctx context.Context, id string) (*models.Node, error)
	ListNodes(ctx context.Context) ([]*models.Node, error)
	UpdateNode(ctx context.Context, node *models.Node) error
}

// ClientPool manages Docker client instances for multiple nodes concurrently.
type ClientPool struct {
	mu            sync.RWMutex
	clients       map[string]*Client
	nodeConfigs   map[string]*models.Node
	store         NodeStore
	log           *logger.Logger
	baseConfig    ClientConfig
	defaultClient *Client
	defaultNodeID string
	stopChan      chan struct{}
	checkerActive bool
}

// NewClientPool creates a new multi-node Docker ClientPool.
func NewClientPool(store NodeStore, defaultClient *Client, log *logger.Logger, cfg ...ClientConfig) *ClientPool {
	var baseCfg ClientConfig
	if len(cfg) > 0 {
		baseCfg = cfg[0]
	} else {
		baseCfg = ClientConfig{
			NetworkName: "carbon-panel-network",
		}
	}

	p := &ClientPool{
		clients:       make(map[string]*Client),
		nodeConfigs:   make(map[string]*models.Node),
		store:         store,
		log:           log,
		baseConfig:    baseCfg,
		defaultClient: defaultClient,
		defaultNodeID: "default",
		stopChan:      make(chan struct{}),
	}

	if defaultClient != nil {
		p.clients[p.defaultNodeID] = defaultClient
	}

	return p
}

// RegisterClient explicitly registers a client for a given nodeID.
func (p *ClientPool) RegisterClient(nodeID string, cli *Client) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clients[nodeID] = cli
	if nodeID == p.defaultNodeID && p.defaultClient == nil {
		p.defaultClient = cli
	}
}

// GetDefaultClient returns the default Docker client (typically local host).
func (p *ClientPool) GetDefaultClient() *Client {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.defaultClient != nil {
		return p.defaultClient
	}
	return p.clients[p.defaultNodeID]
}

// GetClient retrieves the Docker client for the given nodeID.
// If the node cannot be found, the store is unavailable, or its client cannot be
// created, it gracefully falls back to the default client (typically the local
// Docker daemon), logging only a Warn. This fallback is convenient but dangerous
// for multi-node correctness: if a remote node blips, callers can silently end up
// operating against the LOCAL daemon instead of the intended remote node (e.g.
// creating a "remote" container locally by mistake).
//
// Prefer GetClientStrict for any operation where acting on the wrong node would
// be a correctness or safety problem (container create/start/stop/remove,
// recreate, etc.). Only use GetClient where a best-effort fallback to local is
// truly acceptable (e.g. read-only status checks where local UI needs to
// degrade gracefully) and other code may already depend on this behavior.
func (p *ClientPool) GetClient(nodeID string) (*Client, error) {
	if nodeID == "" || nodeID == p.defaultNodeID {
		def := p.GetDefaultClient()
		if def != nil {
			return def, nil
		}
	}

	// Read lock check
	p.mu.RLock()
	cli, exists := p.clients[nodeID]
	p.mu.RUnlock()
	if exists && cli != nil {
		return cli, nil
	}

	// Write lock check and initialize
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double check
	if cli, exists := p.clients[nodeID]; exists && cli != nil {
		return cli, nil
	}

	if p.store == nil {
		if p.defaultClient != nil {
			p.log.Warn("NodeStore is nil; falling back to default Docker client for node: %s", nodeID)
			return p.defaultClient, nil
		}
		return nil, fmt.Errorf("node store is nil and client not found for node: %s", nodeID)
	}

	node, err := p.store.GetNode(context.Background(), nodeID)
	if err != nil {
		if p.defaultClient != nil {
			p.log.Warn("Node %q not found in store (%v); falling back to default Docker client", nodeID, err)
			return p.defaultClient, nil
		}
		return nil, fmt.Errorf("node %s not found: %w", nodeID, err)
	}

	newCli, err := p.createClientForNode(node)
	if err != nil {
		if p.defaultClient != nil {
			p.log.Warn("Failed to create client for node %q (%v); falling back to default Docker client", nodeID, err)
			return p.defaultClient, nil
		}
		return nil, fmt.Errorf("failed to create client for node %s: %w", nodeID, err)
	}

	p.clients[nodeID] = newCli
	p.nodeConfigs[nodeID] = node
	return newCli, nil
}

// GetClientStrict retrieves the Docker client for the given nodeID without fallback to default.
// This is the recommended path for multi-node-sensitive operations: callers get an
// explicit error instead of silently executing against the local Docker daemon
// when the requested node is unavailable. See GetClient's doc comment for why the
// fallback there is risky.
func (p *ClientPool) GetClientStrict(nodeID string) (*Client, error) {
	if nodeID == "" || nodeID == p.defaultNodeID {
		def := p.GetDefaultClient()
		if def != nil {
			return def, nil
		}
	}

	p.mu.RLock()
	cli, exists := p.clients[nodeID]
	p.mu.RUnlock()
	if exists && cli != nil {
		return cli, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if cli, exists := p.clients[nodeID]; exists && cli != nil {
		return cli, nil
	}

	if p.store == nil {
		return nil, fmt.Errorf("node store is nil and client not found for node: %s", nodeID)
	}

	node, err := p.store.GetNode(context.Background(), nodeID)
	if err != nil {
		return nil, fmt.Errorf("node %s not found: %w", nodeID, err)
	}

	newCli, err := p.createClientForNode(node)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for node %s: %w", nodeID, err)
	}

	p.clients[nodeID] = newCli
	p.nodeConfigs[nodeID] = node
	return newCli, nil
}

// RemoveNode closes and unregisters a node client from the pool.
func (p *ClientPool) RemoveNode(nodeID string) error {
	if nodeID == p.defaultNodeID {
		return fmt.Errorf("cannot remove default node from client pool")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if cli, ok := p.clients[nodeID]; ok {
		if err := cli.Close(); err != nil && p.log != nil {
			p.log.Warn("Error closing client for node %s: %v", nodeID, err)
		}
		delete(p.clients, nodeID)
	}
	delete(p.nodeConfigs, nodeID)
	return nil
}

// PingNode tests connectivity to a specific Docker node and updates its DB status.
func (p *ClientPool) PingNode(ctx context.Context, nodeID string) (bool, error) {
	if p.store == nil {
		return false, fmt.Errorf("node store is nil")
	}

	node, err := p.store.GetNode(ctx, nodeID)
	if err != nil {
		return false, fmt.Errorf("failed to get node %s: %w", nodeID, err)
	}

	cli, err := p.GetClientStrict(nodeID)
	if err != nil {
		node.Status = models.NodeStatusOffline
		_ = p.store.UpdateNode(ctx, node)
		return false, fmt.Errorf("failed to get client for node %s: %w", nodeID, err)
	}

	_, err = cli.Ping(ctx)
	if err != nil {
		node.Status = models.NodeStatusOffline
		_ = p.store.UpdateNode(ctx, node)
		return false, fmt.Errorf("ping failed for node %s: %w", nodeID, err)
	}

	now := time.Now().UTC()
	node.LastHeartbeat = &now
	node.Status = models.NodeStatusOnline
	if err := p.store.UpdateNode(ctx, node); err != nil && p.log != nil {
		p.log.Warn("Failed to update status for node %s: %v", nodeID, err)
	}

	return true, nil
}

// StartHealthChecker starts background health checking for all enabled nodes.
func (p *ClientPool) StartHealthChecker(interval time.Duration) {
	p.mu.Lock()
	if p.checkerActive {
		p.mu.Unlock()
		return
	}
	p.checkerActive = true
	p.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-p.stopChan:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), interval/2)
				p.checkAllNodes(ctx)
				cancel()
			}
		}
	}()
}

// StopHealthChecker stops background health checking.
func (p *ClientPool) StopHealthChecker() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.checkerActive {
		return
	}
	close(p.stopChan)
	p.checkerActive = false
}

func (p *ClientPool) checkAllNodes(ctx context.Context) {
	if p.store == nil {
		return
	}

	nodes, err := p.store.ListNodes(ctx)
	if err != nil {
		if p.log != nil {
			p.log.Error("Failed to list nodes for health check: %v", err)
		}
		return
	}

	for _, node := range nodes {
		if !node.Enabled {
			continue
		}
		_, _ = p.PingNode(ctx, node.ID)
	}
}

// Close closes all managed clients and stops background tasks.
func (p *ClientPool) Close() error {
	p.StopHealthChecker()

	p.mu.Lock()
	defer p.mu.Unlock()

	var firstErr error
	for id, cli := range p.clients {
		if cli != nil {
			if err := cli.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		delete(p.clients, id)
	}

	return firstErr
}

func (p *ClientPool) createClientForNode(node *models.Node) (*Client, error) {
	return NewClientForNode(node, p.log, p.baseConfig)
}

// NewClientForNode constructs a *Client configured specifically for a Node.
// Supports unix:// sockets, tcp:// (with optional TLS certs or TLS skip verify), and ssh://.
func NewClientForNode(node *models.Node, log *logger.Logger, baseConfig ClientConfig) (*Client, error) {
	opts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}

	if baseConfig.APIVersion != "" {
		opts = append(opts, client.WithVersion(baseConfig.APIVersion))
	}

	host := strings.TrimSpace(node.Host)
	if host == "" && node.IsLocal {
		host = "unix:///var/run/docker.sock"
	}

	if host != "" {
		if !strings.Contains(host, "://") {
			if strings.HasPrefix(host, "/") {
				host = "unix://" + host
			} else {
				host = "tcp://" + host
			}
		}
		opts = append(opts, client.WithHost(host))
	}

	// TLS configuration
	if node.TLSEnabled || node.TLSCert != "" || node.TLSCACert != "" || node.TLSSkipVerify {
		caFile, certFile, keyFile, err := resolveTLSCertFiles(node)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare TLS certs: %w", err)
		}

		if node.TLSSkipVerify {
			tlsConf, err := tlsconfig.Client(tlsconfig.Options{
				CAFile:             caFile,
				CertFile:           certFile,
				KeyFile:            keyFile,
				InsecureSkipVerify: true,
				ExclusiveRootPools: caFile != "",
			})
			if err != nil {
				return nil, fmt.Errorf("failed to build TLS config: %w", err)
			}
			tr := &http.Transport{
				TLSClientConfig: tlsConf,
			}
			opts = append(opts, client.WithHTTPClient(&http.Client{Transport: tr}))
		} else {
			opts = append(opts, client.WithTLSClientConfig(caFile, certFile, keyFile))
		}
	}

	dockerCli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client for host %s: %w", host, err)
	}

	c := &Client{
		docker: dockerCli,
		log:    log,
		config: baseConfig,
	}

	return c, nil
}

func resolveTLSCertFiles(node *models.Node) (caFile, certFile, keyFile string, err error) {
	certDir := filepath.Join(os.TempDir(), "carbon-panel-node-certs", node.ID)

	writeIfPEM := func(name, content string) (string, error) {
		trimmed := strings.TrimSpace(content)
		if trimmed == "" {
			return "", nil
		}
		// If it looks like a path without newlines and exists, return path
		if !strings.Contains(trimmed, "\n") && !strings.Contains(trimmed, "-----BEGIN") {
			if _, err := os.Stat(trimmed); err == nil {
				return trimmed, nil
			}
		}
		if err := os.MkdirAll(certDir, 0700); err != nil {
			return "", err
		}
		filePath := filepath.Join(certDir, name)
		if err := os.WriteFile(filePath, []byte(trimmed), 0600); err != nil {
			return "", err
		}
		return filePath, nil
	}

	caFile, err = writeIfPEM("ca.pem", node.TLSCACert)
	if err != nil {
		return "", "", "", err
	}
	certFile, err = writeIfPEM("cert.pem", node.TLSCert)
	if err != nil {
		return "", "", "", err
	}
	keyFile, err = writeIfPEM("key.pem", node.TLSKey)
	if err != nil {
		return "", "", "", err
	}

	return caFile, certFile, keyFile, nil
}
