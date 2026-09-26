package svc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/portalloc"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
)

// GetWorkloadNetworking returns network routing, ports, and DNS guidance for a workload.
func (s *WorkloadService) GetWorkloadNetworking(ctx context.Context, req *connect.Request[v1.GetWorkloadNetworkingRequest]) (*connect.Response[v1.GetWorkloadNetworkingResponse], error) {
	w, err := s.load(ctx, req.Msg.Id)
	if err != nil {
		if errors.Is(err, errNoOrgCtx) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}

	var node db.Node
	_ = q.Where("id = ?", w.NodeID).First(&node).Error

	nodeAddress := node.PublicIP
	if nodeAddress == "" {
		nodeAddress = node.Hostname
	}
	if nodeAddress == "" {
		nodeAddress = node.PrivateIP
	}
	if nodeAddress == "" {
		nodeAddress = "127.0.0.1"
	}

	hostPort := int32(w.HostPort)
	if hostPort <= 0 {
		hostPort = 25565
	}
	containerPort := int32(25565)

	primaryAddress := fmt.Sprintf("%s:%d", nodeAddress, hostPort)
	if hostPort == 25565 && w.Hostname != "" {
		primaryAddress = w.Hostname
	}

	srvRecord := ""
	if w.Hostname != "" && hostPort != 25565 {
		srvRecord = portalloc.FormatSRVRecord(w.Hostname, nodeAddress, int(hostPort))
	}

	alloc := portalloc.NewAllocator(0, 0)
	allocs, _ := alloc.GetAllocations(ctx, q, w.NodeID)
	portConflict := false
	for _, a := range allocs {
		if a.WorkloadID != w.ID && a.Port == int(hostPort) {
			portConflict = true
			break
		}
	}

	return connect.NewResponse(&v1.GetWorkloadNetworkingResponse{
		WorkloadId:     w.ID,
		NodeId:         w.NodeID,
		NodeAddress:    nodeAddress,
		HostPort:       hostPort,
		ContainerPort:  containerPort,
		Hostname:       w.Hostname,
		PrimaryAddress: primaryAddress,
		SrvRecord:      srvRecord,
		PortConflict:   portConflict,
	}), nil
}

// ListNodePorts returns all active port leases and available range for a node.
func (s *WorkloadService) ListNodePorts(ctx context.Context, req *connect.Request[v1.ListNodePortsRequest]) (*connect.Response[v1.ListNodePortsResponse], error) {
	nodeID := strings.TrimSpace(req.Msg.NodeId)
	if nodeID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("node_id is required"))
	}

	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}

	alloc := portalloc.NewAllocator(0, 0)
	minPort, maxPort := alloc.PortRange()

	allocs, err := alloc.GetAllocations(ctx, q, nodeID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	protoAllocs := make([]*v1.NodePortAllocation, 0, len(allocs))
	for _, a := range allocs {
		protoAllocs = append(protoAllocs, &v1.NodePortAllocation{
			Port:         int32(a.Port),
			WorkloadId:   a.WorkloadID,
			WorkloadName: a.WorkloadName,
			Status:       a.Status,
			Hostname:     a.Hostname,
		})
	}

	totalPorts := int32((maxPort - minPort) + 1)
	available := totalPorts - int32(len(allocs))
	if available < 0 {
		available = 0
	}

	return connect.NewResponse(&v1.ListNodePortsResponse{
		NodeId:         nodeID,
		PortRangeMin:   int32(minPort),
		PortRangeMax:   int32(maxPort),
		Allocations:    protoAllocs,
		AvailablePorts: available,
	}), nil
}
