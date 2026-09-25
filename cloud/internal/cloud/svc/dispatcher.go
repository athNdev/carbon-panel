package svc

import (
	"log/slog"
	"sync"

	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
)

// NodeSession tracks an active streaming connection with a node agent.
type NodeSession struct {
	NodeID string
	sendCh chan *v1.ControlMessage
}

// NextMessage returns a channel of outbound control envelopes for the node.
func (s *NodeSession) NextMessage() <-chan *v1.ControlMessage {
	return s.sendCh
}

// AgentDispatcher manages active bi-directional gRPC/Connect streams to node agents.
type AgentDispatcher struct {
	mu     sync.RWMutex
	nodes  map[string]*NodeSession
	logger *slog.Logger
}

// NewAgentDispatcher creates a thread-safe registry of connected node agents.
func NewAgentDispatcher(logger *slog.Logger) *AgentDispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &AgentDispatcher{
		nodes:  make(map[string]*NodeSession),
		logger: logger,
	}
}

// Register creates or updates the active session for a given node ID.
func (d *AgentDispatcher) Register(nodeID string) *NodeSession {
	d.mu.Lock()
	defer d.mu.Unlock()
	if old, exists := d.nodes[nodeID]; exists {
		close(old.sendCh)
	}
	sess := &NodeSession{
		NodeID: nodeID,
		sendCh: make(chan *v1.ControlMessage, 64),
	}
	d.nodes[nodeID] = sess
	d.logger.Info("node agent registered in dispatcher", "node_id", nodeID)
	return sess
}

// Unregister cleanly closes the session and removes it from the active registry.
func (d *AgentDispatcher) Unregister(nodeID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if sess, exists := d.nodes[nodeID]; exists {
		close(sess.sendCh)
		delete(d.nodes, nodeID)
		d.logger.Info("node agent unregistered from dispatcher", "node_id", nodeID)
	}
}

// Dispatch queues a control message to be sent to a connected node.
func (d *AgentDispatcher) Dispatch(nodeID string, msg *v1.ControlMessage) bool {
	d.mu.RLock()
	sess, exists := d.nodes[nodeID]
	d.mu.RUnlock()
	if !exists {
		d.logger.Debug("cannot dispatch control message: node not connected", "node_id", nodeID)
		return false
	}
	select {
	case sess.sendCh <- msg:
		return true
	default:
		d.logger.Warn("control message buffer full for node", "node_id", nodeID)
		return false
	}
}

// IsConnected returns whether the node agent currently holds an active stream.
func (d *AgentDispatcher) IsConnected(nodeID string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, exists := d.nodes[nodeID]
	return exists
}
