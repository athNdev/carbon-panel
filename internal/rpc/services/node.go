package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"connectrpc.com/connect"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1/carbonpanelv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ carbonpanelv1connect.NodeServiceHandler = (*NodeService)(nil)

// NodeService handles Docker daemon node management RPCs
type NodeService struct {
	store *storage.Store
	pool  *docker.ClientPool
	log   *logger.Logger
}

// NewNodeService creates a new NodeService instance
func NewNodeService(store *storage.Store, pool *docker.ClientPool, log *logger.Logger) *NodeService {
	return &NodeService{
		store: store,
		pool:  pool,
		log:   log,
	}
}

// dbNodeToProto converts a database Node model to proto Node message
func dbNodeToProto(node *storage.Node) *v1.Node {
	if node == nil {
		return nil
	}

	var status v1.NodeStatus
	switch node.Status {
	case storage.NodeStatusOnline:
		status = v1.NodeStatus_NODE_STATUS_ONLINE
	case storage.NodeStatusOffline:
		status = v1.NodeStatus_NODE_STATUS_OFFLINE
	case storage.NodeStatusError:
		status = v1.NodeStatus_NODE_STATUS_ERROR
	default:
		status = v1.NodeStatus_NODE_STATUS_UNSPECIFIED
	}

	var lastHeartbeat *timestamppb.Timestamp
	if node.LastHeartbeat != nil {
		lastHeartbeat = timestamppb.New(*node.LastHeartbeat)
	}

	return &v1.Node{
		Id:                node.ID,
		Name:              node.Name,
		Host:              node.Host,
		AdvertisedIp:      node.AdvertisedIP,
		TlsCaCert:         node.TLSCACert,
		TlsCert:           node.TLSCert,
		TlsKey:            node.TLSKey,
		TlsEnabled:        node.TLSEnabled,
		TlsSkipVerify:     node.TLSSkipVerify,
		MaxMemoryMb:       node.MaxMemoryMB,
		MaxServers:        int32(node.MaxServers),
		Enabled:           node.Enabled,
		Status:            status,
		IsLocal:           node.IsLocal,
		LastHeartbeat:     lastHeartbeat,
		CreatedAt:         timestamppb.New(node.CreatedAt),
		UpdatedAt:         timestamppb.New(node.UpdatedAt),
		AllocatedMemoryMb: node.AllocatedMemoryMB,
		ServerCount:       int32(node.ServerCount),
		RunningCount:      int32(node.RunningCount),
	}
}

// ListNodes lists all nodes along with resource allocation stats
func (s *NodeService) ListNodes(ctx context.Context, req *connect.Request[v1.ListNodesRequest]) (*connect.Response[v1.ListNodesResponse], error) {
	nodes, err := s.store.ListNodesWithStats(ctx)
	if err != nil {
		s.log.Error("Failed to list nodes: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to list nodes"))
	}

	protoNodes := make([]*v1.Node, 0, len(nodes))
	for _, node := range nodes {
		protoNodes = append(protoNodes, dbNodeToProto(node))
	}

	return connect.NewResponse(&v1.ListNodesResponse{
		Nodes: protoNodes,
	}), nil
}

// GetNode retrieves a single node by ID with stats
func (s *NodeService) GetNode(ctx context.Context, req *connect.Request[v1.GetNodeRequest]) (*connect.Response[v1.GetNodeResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("node ID is required"))
	}

	node, err := s.store.GetNode(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("node not found"))
	}

	mem, count, running, err := s.store.GetNodeStats(ctx, node.ID)
	if err == nil {
		node.AllocatedMemoryMB = mem
		node.ServerCount = count
		node.RunningCount = running
	}

	return connect.NewResponse(&v1.GetNodeResponse{
		Node: dbNodeToProto(node),
	}), nil
}

// CreateNode creates a new remote node
func (s *NodeService) CreateNode(ctx context.Context, req *connect.Request[v1.CreateNodeRequest]) (*connect.Response[v1.CreateNodeResponse], error) {
	msg := req.Msg
	if msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("node name is required"))
	}
	if msg.Host == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("node host is required"))
	}
	if err := validateNodeHost(msg.Host); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	node := &storage.Node{
		Name:          msg.Name,
		Host:          msg.Host,
		AdvertisedIP:  msg.AdvertisedIp,
		TLSCACert:     msg.TlsCaCert,
		TLSCert:       msg.TlsCert,
		TLSKey:        msg.TlsKey,
		TLSEnabled:    msg.TlsEnabled,
		TLSSkipVerify: msg.TlsSkipVerify,
		MaxMemoryMB:   msg.MaxMemoryMb,
		MaxServers:    int(msg.MaxServers),
		Enabled:       msg.Enabled,
		Status:        storage.NodeStatusOffline,
		IsLocal:       false,
	}

	if err := s.store.CreateNode(ctx, node); err != nil {
		s.log.Error("Failed to create node: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create node: %w", err))
	}

	if node.Enabled && s.pool != nil {
		go func() {
			_, _ = s.pool.PingNode(context.Background(), node.ID)
		}()
	}

	return connect.NewResponse(&v1.CreateNodeResponse{
		Node: dbNodeToProto(node),
	}), nil
}

// UpdateNode updates an existing node's configuration
func (s *NodeService) UpdateNode(ctx context.Context, req *connect.Request[v1.UpdateNodeRequest]) (*connect.Response[v1.UpdateNodeResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("node ID is required"))
	}

	node, err := s.store.GetNode(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("node not found"))
	}

	hostOrTLSChanged := false
	if msg.Name != nil {
		node.Name = *msg.Name
	}
	if msg.Host != nil && *msg.Host != node.Host {
		if err := validateNodeHost(*msg.Host); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		node.Host = *msg.Host
		hostOrTLSChanged = true
	}
	if msg.AdvertisedIp != nil {
		node.AdvertisedIP = *msg.AdvertisedIp
	}
	if msg.TlsCaCert != nil && *msg.TlsCaCert != node.TLSCACert {
		node.TLSCACert = *msg.TlsCaCert
		hostOrTLSChanged = true
	}
	if msg.TlsCert != nil && *msg.TlsCert != node.TLSCert {
		node.TLSCert = *msg.TlsCert
		hostOrTLSChanged = true
	}
	if msg.TlsKey != nil && *msg.TlsKey != node.TLSKey {
		node.TLSKey = *msg.TlsKey
		hostOrTLSChanged = true
	}
	if msg.TlsEnabled != nil && *msg.TlsEnabled != node.TLSEnabled {
		node.TLSEnabled = *msg.TlsEnabled
		hostOrTLSChanged = true
	}
	if msg.TlsSkipVerify != nil && *msg.TlsSkipVerify != node.TLSSkipVerify {
		node.TLSSkipVerify = *msg.TlsSkipVerify
		hostOrTLSChanged = true
	}
	if msg.MaxMemoryMb != nil {
		node.MaxMemoryMB = *msg.MaxMemoryMb
	}
	if msg.MaxServers != nil {
		node.MaxServers = int(*msg.MaxServers)
	}
	if msg.Enabled != nil {
		node.Enabled = *msg.Enabled
	}

	if err := s.store.UpdateNode(ctx, node); err != nil {
		s.log.Error("Failed to update node: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update node: %w", err))
	}

	if hostOrTLSChanged && s.pool != nil {
		_ = s.pool.RemoveNode(node.ID)
	}

	mem, count, running, err := s.store.GetNodeStats(ctx, node.ID)
	if err == nil {
		node.AllocatedMemoryMB = mem
		node.ServerCount = count
		node.RunningCount = running
	}

	return connect.NewResponse(&v1.UpdateNodeResponse{
		Node: dbNodeToProto(node),
	}), nil
}

// DeleteNode removes a remote node if no servers/modules are assigned to it
func (s *NodeService) DeleteNode(ctx context.Context, req *connect.Request[v1.DeleteNodeRequest]) (*connect.Response[v1.DeleteNodeResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("node ID is required"))
	}

	if s.pool != nil {
		_ = s.pool.RemoveNode(msg.Id)
	}

	if err := s.store.DeleteNode(ctx, msg.Id); err != nil {
		s.log.Error("Failed to delete node %s: %v", msg.Id, err)
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}

	return connect.NewResponse(&v1.DeleteNodeResponse{
		Success: true,
	}), nil
}

// PingNode tests connectivity to the specified Docker node
func (s *NodeService) PingNode(ctx context.Context, req *connect.Request[v1.PingNodeRequest]) (*connect.Response[v1.PingNodeResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("node ID is required"))
	}

	// Verify node exists
	if _, err := s.store.GetNode(ctx, msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("node %s not found: %w", msg.Id, err))
	}

	if s.pool == nil {
		return connect.NewResponse(&v1.PingNodeResponse{
			Success:   false,
			Message:   "docker client pool not initialized",
			Status:    v1.NodeStatus_NODE_STATUS_ERROR,
			LatencyMs: 0,
		}), nil
	}

	start := time.Now()
	_, err := s.pool.PingNode(ctx, msg.Id)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return connect.NewResponse(&v1.PingNodeResponse{
			Success:   false,
			Message:   err.Error(),
			Status:    v1.NodeStatus_NODE_STATUS_OFFLINE,
			LatencyMs: latency,
		}), nil
	}

	return connect.NewResponse(&v1.PingNodeResponse{
		Success:   true,
		Message:   "Node is online",
		Status:    v1.NodeStatus_NODE_STATUS_ONLINE,
		LatencyMs: latency,
	}), nil
}

// validateNodeHost rejects Docker daemon endpoints that would make the panel
// connect to an unintended target. Docker hosts are unix sockets, tcp://, or
// ssh://; loopback and RFC1918 addresses are legitimate (local socket and
// homelab nodes) but link-local/metadata and unspecified addresses are not.
func validateNodeHost(host string) error {
	allowedSchemes := []string{"unix", "tcp", "http", "https", "ssh", "npipe"}
	scheme := ""
	rest := host
	if idx := strings.Index(host, "://"); idx != -1 {
		scheme = strings.ToLower(host[:idx])
		rest = host[idx+3:]
	}

	switch scheme {
	case "unix", "npipe":
		if rest == "" {
			return fmt.Errorf("node host %q is missing a socket path", host)
		}
		return nil
	case "tcp", "http", "https", "ssh":
		// fall through to address validation below
	default:
		if scheme == "" {
			return fmt.Errorf("node host %q is missing a scheme (expected one of %s)", host, strings.Join(allowedSchemes, ", "))
		}
		return fmt.Errorf("node host scheme %q is not allowed (expected one of %s)", scheme, strings.Join(allowedSchemes, ", "))
	}

	// Strip optional credentials (ssh://user@host).
	if at := strings.LastIndex(rest, "@"); at != -1 {
		rest = rest[at+1:]
	}
	// Strip a trailing path (tcp://host:2375/foo).
	if slash := strings.IndexAny(rest, "/?"); slash != -1 {
		rest = rest[:slash]
	}

	hostname := rest
	if h, _, err := net.SplitHostPort(rest); err == nil {
		hostname = h
	}
	hostname = strings.Trim(hostname, "[]")
	if hostname == "" {
		return fmt.Errorf("node host %q has no address", host)
	}

	if ip := net.ParseIP(hostname); ip != nil {
		if err := validateNodeIP(ip); err != nil {
			return fmt.Errorf("node host %q is not allowed: %w", host, err)
		}
		return nil
	}

	ips, err := net.LookupIP(hostname)
	if err != nil {
		return fmt.Errorf("node host %q cannot be resolved: %w", host, err)
	}
	for _, ip := range ips {
		if err := validateNodeIP(ip); err != nil {
			return fmt.Errorf("node host %q resolves to a disallowed address: %w", host, err)
		}
	}
	return nil
}

// validateNodeIP allows loopback (a locally exposed Docker daemon) and RFC1918
// (homelab nodes) while refusing link-local/metadata, unspecified and multicast
// addresses. utils.ValidateIP is stricter than Docker nodes need (it always
// refuses loopback), so the policy lives here.
func validateNodeIP(ip net.IP) error {
	if ip == nil {
		return errors.New("invalid IP address")
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("link-local address %s is prohibited", ip)
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("unspecified address %s is prohibited", ip)
	}
	if ip.IsMulticast() {
		return fmt.Errorf("multicast address %s is prohibited", ip)
	}
	return nil
}
