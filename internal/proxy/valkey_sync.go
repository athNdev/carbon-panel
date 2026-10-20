// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package proxy

import (
	"context"
	"encoding/json"
	"time"

	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/athNdev/carbon-panel/pkg/routing"
)

const (
	ValkeyChannelRoutingEvents = routing.ValkeyChannelRoutingEvents
	ValkeyRouteKeyPrefix       = routing.ValkeyRouteKeyPrefix
)

// RouteInfo holds stored route details in Valkey / Dragonfly KV.
type RouteInfo = routing.RouteInfo

// RespClient implements a lightweight Go-native Redis RESP protocol client.
type RespClient = routing.RespClient

// ValkeyClient is an interface for Valkey/Dragonfly KV and pub/sub operations.
type ValkeyClient = routing.ValkeyClient

// MockValkeyClient provides an in-memory client for testing or standalone execution.
type MockValkeyClient = routing.MockValkeyClient

// NewRespClient creates a new RESP client for Valkey or Dragonfly.
func NewRespClient(addr string, password ...string) *routing.RespClient {
	return routing.NewRespClient(addr, password...)
}

// NewMockValkeyClient returns an in-memory Valkey client.
func NewMockValkeyClient() *routing.MockValkeyClient {
	return routing.NewMockValkeyClient()
}

// RoutingEvent represents a pub/sub broadcast event for route synchronization.
type RoutingEvent struct {
	Action      string `json:"action"` // "add", "update", "remove"
	ServerID    string `json:"server_id"`
	Hostname    string `json:"hostname"`
	BackendHost string `json:"backend_host,omitempty"`
	BackendPort int    `json:"backend_port,omitempty"`
	ProxyPort   int    `json:"proxy_port"`
	OriginNode  string `json:"origin_node"`
	Timestamp   int64  `json:"timestamp"`
}

// ValkeySyncManager orchestrates routing synchronization with Valkey / Dragonfly.
type ValkeySyncManager struct {
	client     ValkeyClient
	nodeID     string
	logger     *logger.Logger
	onRemoteOp func(event *RoutingEvent)
	cancel     context.CancelFunc
	ctx        context.Context
}

func NewValkeySyncManager(client ValkeyClient, nodeID string, log *logger.Logger) *ValkeySyncManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &ValkeySyncManager{
		client: client,
		nodeID: nodeID,
		logger: log,
		ctx:    ctx,
		cancel: cancel,
	}
}

func (v *ValkeySyncManager) SetRemoteHandler(handler func(event *RoutingEvent)) {
	v.onRemoteOp = handler
}

func (v *ValkeySyncManager) Start(ctx context.Context) error {
	if v.client == nil {
		return nil
	}

	return v.client.Subscribe(ctx, ValkeyChannelRoutingEvents, func(channel, message string) {
		var event RoutingEvent
		if err := json.Unmarshal([]byte(message), &event); err != nil {
			if v.logger != nil {
				v.logger.Warn("Failed to unmarshal routing sync event: %v", err)
			}
			return
		}

		if event.OriginNode == v.nodeID {
			return
		}

		if v.logger != nil {
			v.logger.Info("Received remote routing sync event: %s for host %s from node %s", event.Action, event.Hostname, event.OriginNode)
		}

		if v.onRemoteOp != nil {
			v.onRemoteOp(&event)
		}
	})
}

func (v *ValkeySyncManager) BroadcastRouteAdd(ctx context.Context, serverID, hostname, backendHost string, backendPort, proxyPort int) error {
	if v.client == nil {
		return nil
	}

	info := &RouteInfo{
		ServerID:    serverID,
		Hostname:    hostname,
		BackendHost: backendHost,
		BackendPort: backendPort,
		ProxyPort:   proxyPort,
		UpdatedAt:   time.Now().Unix(),
	}

	_ = v.client.SetRoute(ctx, hostname, info)

	event := &RoutingEvent{
		Action:      "add",
		ServerID:    serverID,
		Hostname:    hostname,
		BackendHost: backendHost,
		BackendPort: backendPort,
		ProxyPort:   proxyPort,
		OriginNode:  v.nodeID,
		Timestamp:   time.Now().Unix(),
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return v.client.Publish(ctx, ValkeyChannelRoutingEvents, string(data))
}

func (v *ValkeySyncManager) BroadcastRouteRemove(ctx context.Context, hostname string, proxyPort int) error {
	if v.client == nil {
		return nil
	}

	_ = v.client.DeleteRoute(ctx, hostname)

	event := &RoutingEvent{
		Action:     "remove",
		Hostname:   hostname,
		ProxyPort:  proxyPort,
		OriginNode: v.nodeID,
		Timestamp:  time.Now().Unix(),
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return v.client.Publish(ctx, ValkeyChannelRoutingEvents, string(data))
}

func (v *ValkeySyncManager) Close() error {
	v.cancel()
	if v.client != nil {
		return v.client.Close()
	}
	return nil
}
