// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package docker

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	models "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/docker/go-connections/tlsconfig"
	"github.com/moby/moby/client"
)

// NodeStore defines the DB interface needed by ClientPool
type NodeStore interface {
	GetNode(ctx context.Context, id string) (*models.Node, error)
	ListNodes(ctx context.Context) ([]*models.Node, error)
	UpdateNode(ctx context.Context, node *models.Node) error
}

// PoolStats tracks telemetry for Docker client pool access (PERF-5).
type PoolStats struct {
	TotalRequests uint64
	CacheHits     uint64
	CacheMisses   uint64
	Fallbacks     uint64
	CreateErrors  uint64
}

type poolStatsInternal struct {
	totalRequests uint64
	cacheHits     uint64
	cacheMisses   uint64
	fallbacks     uint64
	createErrors  uint64
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
	allowFallback bool
	stopChan      chan struct{}
	checkerActive bool
	stats         poolStatsInternal
	hostMemories  map[string]int64
}

// NewClientPool creates a new multi-node Docker ClientPool.
func NewClientPool(store NodeStore, defaultClient *Client, log *logger.Logger, cfg ...ClientConfig) *ClientPool {
	var baseCfg ClientConfig
	if len(cfg) > 0 {
		baseCfg = cfg[0]
	} else if defaultClient != nil {
		baseCfg = defaultClient.GetConfig()
		if baseCfg.NetworkName == "" {
			baseCfg.NetworkName = "carbon-panel-network"
		}
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
		hostMemories:  make(map[string]int64),
	}

	if defaultClient != nil {
		p.clients[p.defaultNodeID] = defaultClient
	}

	return p
}

// RegisterClient explicitly registers a client for a given nodeID.
// SetProxyEnabled propagates the proxy mode setting across all pooled clients and base config.
func (p *ClientPool) SetProxyEnabled(enabled bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.baseConfig.ProxyEnabled = enabled
	for _, cli := range p.clients {
		if cli != nil {
			cli.SetProxyEnabled(enabled)
		}
	}
}

func (p *ClientPool) RegisterClient(nodeID string, cli *Client) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clients[nodeID] = cli
	if nodeID == p.defaultNodeID && p.defaultClient == nil {
		p.defaultClient = cli
	}
}

// SetFallbackEnabled controls whether GetClient may fall back to the default client
// when a specific requested node cannot be found or fails to connect.
// By default, fallback is disabled for security and correctness so that operations
// targeted to a specific remote node are never silently executed on the local daemon.
func (p *ClientPool) SetFallbackEnabled(enabled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.allowFallback = enabled
}

// IsFallbackEnabled reports whether silent fallback to the default client is enabled.
func (p *ClientPool) IsFallbackEnabled() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.allowFallback
}

// GetDefaultNodeID returns the configured ID of the default node (e.g. "default").
func (p *ClientPool) GetDefaultNodeID() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.defaultNodeID
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

// Stats returns a point-in-time snapshot of the ClientPool metrics.
func (p *ClientPool) Stats() PoolStats {
	return PoolStats{
		TotalRequests: atomic.LoadUint64(&p.stats.totalRequests),
		CacheHits:     atomic.LoadUint64(&p.stats.cacheHits),
		CacheMisses:   atomic.LoadUint64(&p.stats.cacheMisses),
		Fallbacks:     atomic.LoadUint64(&p.stats.fallbacks),
		CreateErrors:  atomic.LoadUint64(&p.stats.createErrors),
	}
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
	atomic.AddUint64(&p.stats.totalRequests, 1)

	if nodeID == "" || nodeID == p.defaultNodeID || nodeID == "local" {
		def := p.GetDefaultClient()
		if def != nil {
			atomic.AddUint64(&p.stats.cacheHits, 1)
			return def, nil
		}
	}

	// Read lock check
	p.mu.RLock()
	cli, exists := p.clients[nodeID]
	allowFallback := p.allowFallback
	p.mu.RUnlock()
	if exists && cli != nil {
		atomic.AddUint64(&p.stats.cacheHits, 1)
		return cli, nil
	}

	atomic.AddUint64(&p.stats.cacheMisses, 1)

	if p.store == nil {
		if allowFallback && p.defaultClient != nil {
			atomic.AddUint64(&p.stats.fallbacks, 1)
			p.log.Warn("NodeStore is nil; falling back to default Docker client for node: %s", nodeID)
			return p.defaultClient, nil
		}
		return nil, fmt.Errorf("node store is nil and client not found for node: %s", nodeID)
	}

	node, err := p.store.GetNode(context.Background(), nodeID)
	if err != nil {
		if allowFallback && p.defaultClient != nil {
			atomic.AddUint64(&p.stats.fallbacks, 1)
			p.log.Warn("Node %q not found in store (%v); falling back to default Docker client", nodeID, err)
			return p.defaultClient, nil
		}
		return nil, fmt.Errorf("node %s not found in store: %w", nodeID, err)
	}

	if node.IsLocal && p.defaultClient != nil {
		p.mu.Lock()
		p.clients[nodeID] = p.defaultClient
		p.mu.Unlock()
		atomic.AddUint64(&p.stats.cacheHits, 1)
		return p.defaultClient, nil
	}

	// Build the client outside the pool lock: NewClientForNode performs a
	// bounded API-version ping, and holding p.mu across that network call
	// would stall every other node's callers behind an unresponsive daemon.
	newCli, err := p.createClientForNode(node)
	if err != nil {
		atomic.AddUint64(&p.stats.createErrors, 1)
		if allowFallback && p.defaultClient != nil {
			atomic.AddUint64(&p.stats.fallbacks, 1)
			p.log.Warn("Failed to create client for node %q (%v); falling back to default Docker client", nodeID, err)
			return p.defaultClient, nil
		}
		return nil, fmt.Errorf("failed to create client for node %s: %w", nodeID, err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Another goroutine may have registered a client while this one was built.
	if existing, ok := p.clients[nodeID]; ok && existing != nil {
		_ = newCli.Close()
		return existing, nil
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
	atomic.AddUint64(&p.stats.totalRequests, 1)

	if nodeID == "" || nodeID == p.defaultNodeID || nodeID == "local" {
		def := p.GetDefaultClient()
		if def != nil {
			atomic.AddUint64(&p.stats.cacheHits, 1)
			return def, nil
		}
	}

	p.mu.RLock()
	cli, exists := p.clients[nodeID]
	p.mu.RUnlock()
	if exists && cli != nil {
		atomic.AddUint64(&p.stats.cacheHits, 1)
		return cli, nil
	}

	atomic.AddUint64(&p.stats.cacheMisses, 1)

	if p.store == nil {
		return nil, fmt.Errorf("node store is nil and client not found for node: %s", nodeID)
	}

	node, err := p.store.GetNode(context.Background(), nodeID)
	if err != nil {
		return nil, fmt.Errorf("node %s not found: %w", nodeID, err)
	}

	if node.IsLocal && p.defaultClient != nil {
		p.mu.Lock()
		p.clients[nodeID] = p.defaultClient
		p.mu.Unlock()
		atomic.AddUint64(&p.stats.cacheHits, 1)
		return p.defaultClient, nil
	}

	// See GetClient: build the client outside the pool lock so its bounded
	// API-version ping cannot stall callers for other nodes.
	newCli, err := p.createClientForNode(node)
	if err != nil {
		atomic.AddUint64(&p.stats.createErrors, 1)
		return nil, fmt.Errorf("failed to create client for node %s: %w", nodeID, err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if existing, ok := p.clients[nodeID]; ok && existing != nil {
		_ = newCli.Close()
		return existing, nil
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
	delete(p.hostMemories, nodeID)
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
		if nodeID != p.defaultNodeID && !node.IsLocal {
			p.mu.Lock()
			if cached, ok := p.clients[nodeID]; ok && cached == cli {
				_ = cached.Close()
				delete(p.clients, nodeID)
			}
			p.mu.Unlock()
		}
		return false, fmt.Errorf("ping failed for node %s: %w", nodeID, err)
	}

	// Ensure network and cache host memory capacity
	_ = cli.EnsureNetwork()
	if mem, memErr := cli.HostMemoryMB(ctx); memErr == nil && mem > 0 {
		p.mu.Lock()
		p.hostMemories[nodeID] = mem
		p.mu.Unlock()
	}

	now := time.Now().UTC()
	node.LastHeartbeat = &now
	node.Status = models.NodeStatusOnline
	if err := p.store.UpdateNode(ctx, node); err != nil && p.log != nil {
		p.log.Warn("Failed to update status for node %s: %v", nodeID, err)
	}

	return true, nil
}

// GetHostMemoryMB returns the cached host memory capacity in MB for a node,
// or queries the Docker daemon directly if not yet cached.
func (p *ClientPool) GetHostMemoryMB(ctx context.Context, nodeID string) (int64, error) {
	p.mu.RLock()
	if mem, ok := p.hostMemories[nodeID]; ok && mem > 0 {
		p.mu.RUnlock()
		return mem, nil
	}
	p.mu.RUnlock()

	cli, err := p.GetClientStrict(nodeID)
	if err != nil || cli == nil {
		if (nodeID == p.defaultNodeID || nodeID == "") && p.defaultClient != nil {
			cli = p.defaultClient
		} else {
			return 0, fmt.Errorf("no client available for node %s: %w", nodeID, err)
		}
	}

	mem, err := cli.HostMemoryMB(ctx)
	if err != nil {
		return 0, err
	}

	p.mu.Lock()
	p.hostMemories[nodeID] = mem
	p.mu.Unlock()
	return mem, nil
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

	var wg sync.WaitGroup
	for _, node := range nodes {
		if !node.Enabled {
			continue
		}
		n := node
		wg.Add(1)
		go func() {
			defer wg.Done()
			nodeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			_, _ = p.PingNode(nodeCtx, n.ID)
		}()
	}
	wg.Wait()
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
	cli, err := NewClientForNode(node, p.log, p.baseConfig)
	if err != nil {
		return nil, err
	}
	if p.defaultClient != nil && p.defaultClient.logStreamer != nil {
		cli.SetLogStreamer(p.defaultClient.logStreamer)
	}
	return cli, nil
}

// NewClientForNode constructs a *Client configured specifically for a Node.
// Supports unix:// sockets, tcp:// (with optional TLS certs or TLS skip verify), and ssh://.
func NewClientForNode(node *models.Node, log *logger.Logger, baseConfig ClientConfig) (*Client, error) {
	opts := []client.Opt{
		client.FromEnv,
	}

	if baseConfig.APIVersion != "" {
		opts = append(opts, client.WithVersion(baseConfig.APIVersion))
	}
	if baseConfig.NetworkName == "" {
		baseConfig.NetworkName = "carbon-panel-network"
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

	// TLS configuration. The SDK no longer exposes the underlying transport,
	// so build the TLS config here and hand the client a transport carrying
	// it (this branch only runs for TCP hosts, where DefaultTransport
	// semantics apply).
	if node.TLSEnabled || node.TLSCert != "" || node.TLSCACert != "" || node.TLSSkipVerify {
		caFile, certFile, keyFile, err := resolveTLSCertFiles(node)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare TLS certs: %w", err)
		}

		tlsConf, err := tlsconfig.Client(tlsconfig.Options{
			CAFile:             caFile,
			CertFile:           certFile,
			KeyFile:            keyFile,
			InsecureSkipVerify: node.TLSSkipVerify,
			ExclusiveRootPools: caFile != "",
		})
		if err != nil {
			return nil, fmt.Errorf("failed to build TLS config: %w", err)
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = tlsConf
		opts = append(opts, client.WithHTTPClient(&http.Client{Transport: transport}))
	}

	dockerCli, err := NewAPIClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client for host %s: %w", host, err)
	}

	c := &Client{
		docker: dockerCli,
		host:   host,
		log:    log,
		config: baseConfig,
	}

	go func() {
		_ = c.EnsureNetwork()
	}()

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
