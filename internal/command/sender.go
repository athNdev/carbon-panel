package command

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/moby/moby/client"
	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/proxy"
	rcon "github.com/athNdev/carbon-panel/internal/rcon"
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
func (s *Sender) resolveExecutor(nodeID string) DockerExecutor {
	if s.pool != nil {
		if cli, err := s.pool.GetClientStrict(nodeID); err == nil && cli != nil {
			return cli
		}
	}
	return s.docker
}

func (s *Sender) SendCommand(ctx context.Context, serverID string, command string) (string, error) {
	server, err := s.store.GetServer(ctx, serverID)

	if err != nil {
		return "", fmt.Errorf("server container not found")
	}
	if server.ContainerID == "" {
		return "", fmt.Errorf("server container not found")
	}

	executor := s.resolveExecutor(server.NodeID)

	// old docker exec command
	dockerExec := func(cause error) (string, error) {
		output, err := executor.ExecCommand(ctx, server.ContainerID, command)
		if err != nil {
			return "", fmt.Errorf("rcon path failed: %w; fallback exec failed: %v", cause, err)
		}
		return output, nil
	}

	serverCfg, err := s.store.GetServerConfig(ctx, serverID)
	if err != nil {
		return dockerExec(fmt.Errorf("failed to load server config: %w", err))
	}

	if serverCfg.EnableRCON != nil && *serverCfg.EnableRCON == false {
		return dockerExec(fmt.Errorf("rcon is disabled for this server"))
	}

	var rconPort int
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
	if serverCfg.RCONPort != nil {
		rconPort = *serverCfg.RCONPort
	}

	var rconPassword string
	if v, ok := s.config.Minecraft.GlobalConfig["rconPassword"]; ok && v != nil {
		if p, ok := v.(string); ok {
			rconPassword = p
		} else {
			rconPassword = fmt.Sprint(v)
		}
	}
	if serverCfg.RCONPassword != nil {
		rconPassword = *serverCfg.RCONPassword
	}

	var cli proxy.ContainerInspector
	if dc, ok := executor.(interface{ GetDockerClient() *client.Client }); ok {
		cli = dc.GetDockerClient()
	}
	ip, err := proxy.GetContainerIP(cli, server.ContainerID, s.config.Docker.NetworkName)
	if err != nil {
		return dockerExec(fmt.Errorf("failed to resolve container ip: %w", err))
	}

	// run comamand in dedicated context with timeout
	rconCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	output, err := rcon.SendCommand(rconCtx, ip, rconPort, rconPassword, command)

	if err != nil {
		return dockerExec(fmt.Errorf("rcon command failed: %w", err))
	}

	return output, nil
}
