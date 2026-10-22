// SPDX-License-Identifier: MIT AND MPL-2.0
// This file includes DiscoPanel material under the MIT License,
// Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
// and still applies to that inherited material.
// Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
// If a copy of the MPL was not distributed with this file, You can obtain
// one at https://mozilla.org/MPL/2.0/.

package command

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/proxy"
	rcon "github.com/athNdev/carbon-panel/internal/rcon"
	"github.com/moby/moby/client"
)

type DockerExecutor interface {
	ExecCommand(ctx context.Context, containerID string, command string) (string, error)
}

type Sender struct {
	store  *storage.Store
	config *config.Config
	docker DockerExecutor
	pool   *docker.ClientPool
}

func NewSender(store *storage.Store, cfg *config.Config, dockerExec DockerExecutor, pool *docker.ClientPool) *Sender {
	return &Sender{
		store:  store,
		config: cfg,
		docker: dockerExec,
		pool:   pool,
	}
}

// resolveExecutor returns the DockerExecutor for the given nodeID, preferring a
// strict node-specific client from the pool over the single default executor so
// commands are not silently sent to the wrong (local) daemon for a remote node.
func (s *Sender) resolveExecutor(nodeID string) (DockerExecutor, error) {
	// Servers without a node assignment retain the legacy local execution path.
	if nodeID == "" {
		return s.docker, nil
	}
	if s.pool != nil {
		cli, err := s.pool.GetClientStrict(nodeID)
		if err != nil {
			if nodeID == "default" || nodeID == "local" {
				return s.docker, nil
			}
			return nil, fmt.Errorf("docker executor unavailable for node %q: %w", nodeID, err)
		}
		if cli == nil {
			return nil, fmt.Errorf("docker executor unavailable for node %q", nodeID)
		}
		return cli, nil
	}
	if nodeID == "default" || nodeID == "local" {
		return s.docker, nil
	}
	if s.store != nil {
		node, err := s.store.GetNode(context.Background(), nodeID)
		if err == nil && node != nil && node.IsLocal {
			return s.docker, nil
		}
	}
	return nil, fmt.Errorf("docker executor unavailable for node %q: client pool is not configured", nodeID)
}

func (s *Sender) SendCommand(ctx context.Context, serverID string, command string) (string, error) {
	server, err := s.store.GetServer(ctx, serverID)

	if err != nil || server == nil {
		return "", fmt.Errorf("server container not found")
	}
	if server.ContainerID == "" {
		return "", fmt.Errorf("server container not found")
	}

	executor, err := s.resolveExecutor(server.NodeID)
	if err != nil {
		return "", err
	}

	// old docker exec command
	dockerExec := func(cause error) (string, error) {
		if executor == nil {
			if cause != nil {
				return "", fmt.Errorf("no docker executor available: %w", cause)
			}
			return "", fmt.Errorf("no docker executor available")
		}
		output, err := executor.ExecCommand(ctx, server.ContainerID, command)
		if err != nil {
			if cause != nil {
				return "", fmt.Errorf("rcon path failed: %w; fallback exec failed: %v", cause, err)
			}
			return "", fmt.Errorf("fallback exec failed: %w", err)
		}
		return output, nil
	}

	// Remote nodes run on isolated Docker bridge networks that are not routable
	// from the panel over IP. Execute commands directly via Docker exec rather
	// than waiting on an unroutable TCP RCON handshake.
	if server.NodeID != "" && s.store != nil {
		if node, err := s.store.GetNode(ctx, server.NodeID); err == nil && node != nil && !node.IsLocal {
			return dockerExec(nil)
		}
	}

	serverCfg, err := s.store.GetServerConfig(ctx, serverID)
	if err != nil {
		return dockerExec(fmt.Errorf("failed to load server config: %w", err))
	}

	if serverCfg != nil && serverCfg.EnableRCON != nil && !*serverCfg.EnableRCON {
		return dockerExec(fmt.Errorf("rcon is disabled for this server"))
	}

	var rconPort int
	if s.config != nil && s.config.Minecraft.GlobalConfig != nil {
		if v, ok := s.config.Minecraft.GlobalConfig["rconPort"]; ok && v != nil {
			switch t := v.(type) {
			case int:
				rconPort = t
			case int64:
				rconPort = int(t)
			case float64:
				rconPort = int(t)
			case string:
				if p, err := strconv.Atoi(t); err == nil {
					rconPort = p
				}
			}
		}
	}
	if serverCfg != nil && serverCfg.RCONPort != nil {
		rconPort = *serverCfg.RCONPort
	}

	var rconPassword string
	if s.config != nil && s.config.Minecraft.GlobalConfig != nil {
		if v, ok := s.config.Minecraft.GlobalConfig["rconPassword"]; ok && v != nil {
			if p, ok := v.(string); ok {
				rconPassword = p
			} else {
				rconPassword = fmt.Sprint(v)
			}
		}
	}
	if serverCfg != nil && serverCfg.RCONPassword != nil {
		rconPassword = *serverCfg.RCONPassword
	}

	var cli proxy.DockerInspector
	if dc, ok := executor.(interface{ GetDockerClient() *client.Client }); ok {
		cli = dc.GetDockerClient()
	}
	networkName := ""
	if s.config != nil {
		networkName = s.config.Docker.NetworkName
	}
	ip, err := proxy.GetContainerIP(cli, server.ContainerID, networkName)
	if err != nil {
		return dockerExec(fmt.Errorf("failed to resolve container ip: %w", err))
	}

	// run comamand in dedicated context with timeout
	rconCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	output, err := rcon.SendCommand(rconCtx, ip, rconPort, rconPassword, command)

	if err != nil {
		// Redact RCON password from error message to prevent secret leakage in logs (SEC-4)
		errMsg := err.Error()
		if rconPassword != "" && strings.Contains(errMsg, rconPassword) {
			err = errors.New(strings.ReplaceAll(errMsg, rconPassword, "********"))
		}
		return dockerExec(fmt.Errorf("rcon command failed: %w", err))
	}

	return output, nil
}
