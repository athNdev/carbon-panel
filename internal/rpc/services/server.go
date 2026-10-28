// SPDX-License-Identifier: MIT AND MPL-2.0
// This file includes DiscoPanel material under the MIT License,
// Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
// and still applies to that inherited material.
// Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
// If a copy of the MPL was not distributed with this file, You can obtain
// one at https://mozilla.org/MPL/2.0/.

package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/activity"
	"github.com/athNdev/carbon-panel/internal/auth"
	"github.com/athNdev/carbon-panel/internal/command"
	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/events"
	"github.com/athNdev/carbon-panel/internal/metrics"
	"github.com/athNdev/carbon-panel/internal/minecraft"
	"github.com/athNdev/carbon-panel/internal/module"
	"github.com/athNdev/carbon-panel/internal/nodestorage"
	"github.com/athNdev/carbon-panel/internal/operation"
	"github.com/athNdev/carbon-panel/internal/proxy"
	"github.com/athNdev/carbon-panel/internal/rbac"
	"github.com/athNdev/carbon-panel/internal/scheduler"
	"github.com/athNdev/carbon-panel/internal/staging"
	"github.com/athNdev/carbon-panel/pkg/files"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1/carbonpanelv1connect"
	"github.com/athNdev/carbon-panel/pkg/utils"
	"github.com/containerd/errdefs"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Compile-time check that ServerService implements the interface
var _ carbonpanelv1connect.ServerServiceHandler = (*ServerService)(nil)

// statusRefreshTimeout bounds the live Docker status refresh done by list/get
// endpoints. It is deliberately far below the panel's HTTP write timeout so a
// single unresponsive node cannot hold the response open until the server
// drops the connection; the background metrics collector and reconciler keep
// the stored status fresh, so falling back to it is safe.
const statusRefreshTimeout = 5 * time.Second

// ServerService implements the Server service
type ServerService struct {
	store            *storage.Store
	docker           *docker.Client
	hostMemoryMB     func(context.Context, string) (int64, error)
	hostPortBound    func(int) bool
	capacityMu       sync.Mutex
	pool             *docker.ClientPool
	placementEngine  *docker.PlacementEngine
	sender           *command.Sender
	config           *config.Config
	proxy            *proxy.Manager
	log              *logger.Logger
	logStreamer      *logger.LogStreamer
	metricsCollector *metrics.Collector
	moduleManager    *module.Manager
	bus              *events.Bus
	enforcer         *rbac.Enforcer
}

// beginServerLifecycleTransition claims the lifecycle transition for a server
// id coming straight from a request. A missing id is an invalid argument, not a
// competing transition: without this, TryBeginServerTransition("") fails with a
// misleading "server transition already in progress". The returned handle owns
// the claim and must be released.
func beginServerLifecycleTransition(id string) (*docker.Transition, error) {
	if strings.TrimSpace(id) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("server id is required"))
	}
	transition, err := docker.TryBeginServerTransition(id)
	if err != nil {
		return nil, connect.NewError(connect.CodeAborted, err)
	}
	return transition, nil
}

func (s *ServerService) getDockerClient(nodeID string) *docker.Client {
	cli, err := s.getDockerClientStrict(nodeID)
	if err != nil {
		return nil
	}
	return cli
}

func (s *ServerService) recreateStagedModule(ctx context.Context, moduleID string) error {
	if s.moduleManager == nil {
		return errors.New("module recreate is not configured")
	}
	return s.moduleManager.RecreateModule(ctx, moduleID)
}

func (s *ServerService) getDockerClientStrict(nodeID string) (*docker.Client, error) {
	return docker.NewLocator(s.pool, s.docker, s.store).For(context.Background(), nodeID)
}

// checkNodeHostPort checks Docker allocations on the selected daemon and uses
// the controller socket table only when that daemon is the local node.
func (s *ServerService) checkNodeHostPort(ctx context.Context, nodeID string, port int) error {
	if port < 1 || port > 65535 {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid host port %d", port))
	}
	if s.store == nil {
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("cannot identify host for port %d", port))
	}
	node, err := s.store.GetNode(ctx, nodeID)
	if err != nil {
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("cannot identify Docker node %q for port %d: %w", nodeID, port, err))
	}
	cli, err := s.getDockerClientStrict(nodeID)
	if err != nil || cli == nil {
		if err == nil {
			err = fmt.Errorf("no Docker client available")
		}
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("cannot check port %d on Docker node %s: %w", port, node.Name, err))
	}
	allocated, err := cli.GetAllocatedHostPorts(ctx)
	if err != nil {
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("cannot check allocated ports on Docker node %s: %w", node.Name, err))
	}
	if allocated[port] {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port %d is already in use on Docker node %s", port, node.Name))
	}
	if node.IsLocal && s.isHostPortBound(port) {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port %d is already in use on the local host", port))
	}
	return nil
}

func (s *ServerService) isHostPortBound(port int) bool {
	if s.hostPortBound != nil {
		return s.hostPortBound(port)
	}
	return isHostPortBound(port)
}

func (s *ServerService) getHostMemoryMB(ctx context.Context, nodeID string) (int64, error) {
	if s.hostMemoryMB != nil {
		return s.hostMemoryMB(ctx, nodeID)
	}
	if s.pool != nil {
		return s.pool.GetHostMemoryMB(ctx, nodeID)
	}
	cli, err := s.getDockerClientStrict(nodeID)
	if err != nil || cli == nil {
		if err == nil {
			err = fmt.Errorf("no docker client available for node %s", nodeID)
		}
		return 0, err
	}
	return cli.HostMemoryMB(ctx)
}

// checkNodeMemoryCapacity prevents operations that would exceed either the
// operator-configured node cap or the capacity reported by its Docker daemon.
// additionalMemoryMB is the new reservation for creation; starts pass zero
// because their server memory is already included in the node allocation.
func (s *ServerService) checkNodeMemoryCapacity(ctx context.Context, nodeID string, additionalMemoryMB int64) error {
	node, err := s.store.GetNode(ctx, nodeID)
	if err != nil {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("failed to load target node capacity: %w", err))
	}
	hostMemoryMB, err := s.getHostMemoryMB(ctx, nodeID)
	if err != nil {
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("cannot verify memory capacity on node %s: %w", node.Name, err))
	}
	allocatedMemoryMB, _, _, err := s.store.GetNodeStats(ctx, nodeID)
	if err != nil {
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("cannot verify allocated memory on node %s: %w", node.Name, err))
	}

	capacityMB := hostMemoryMB
	if node.MaxMemoryMB > 0 && node.MaxMemoryMB < capacityMB {
		capacityMB = node.MaxMemoryMB
	}
	if allocatedMemoryMB+additionalMemoryMB > capacityMB {
		return connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("node %s lacks memory capacity (allocated %d MB + requested %d MB > limit %d MB)", node.Name, allocatedMemoryMB, additionalMemoryMB, capacityMB))
	}
	return nil
}

// checkServerStartCapacity ensures starting a server does not exceed the node's
// memory limit when combined with other currently active (running/starting) servers.
func (s *ServerService) checkServerStartCapacity(ctx context.Context, server *storage.Server) error {
	if server == nil {
		return fmt.Errorf("server is nil")
	}
	node, err := s.store.GetNode(ctx, server.NodeID)
	if err != nil {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("failed to load target node capacity: %w", err))
	}
	hostMemoryMB, err := s.getHostMemoryMB(ctx, server.NodeID)
	if err != nil {
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("cannot verify memory capacity on node %s: %w", node.Name, err))
	}

	capacityMB := hostMemoryMB
	if node.MaxMemoryMB > 0 && node.MaxMemoryMB < capacityMB {
		capacityMB = node.MaxMemoryMB
	}

	// Server itself cannot exceed node capacity
	if int64(server.Memory) > capacityMB {
		return connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("node %s lacks memory capacity (server requires %d MB > limit %d MB)", node.Name, server.Memory, capacityMB))
	}

	// Active running/starting memory + this server (if not already running) cannot exceed capacity
	runningMemoryMB, err := s.store.GetNodeRunningMemoryMB(ctx, server.NodeID)
	if err != nil {
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("cannot verify running memory on node %s: %w", node.Name, err))
	}

	additionalMB := int64(server.Memory)
	if server.Status == storage.StatusRunning || server.Status == storage.StatusStarting {
		additionalMB = 0
	}

	if runningMemoryMB+additionalMB > capacityMB {
		return connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("node %s lacks memory capacity to run server (running %d MB + requested %d MB > limit %d MB)", node.Name, runningMemoryMB, additionalMB, capacityMB))
	}
	return nil
}

// statusAfterContainerRecreation preserves intentional deep sleep when a
// replacement container is created stopped. Other non-running replacements
// represent a normal stopped server; a replacement that was running remains
// running (or starting, where the caller owns that transition).
func statusAfterContainerRecreation(previous storage.ServerStatus, wasRunning bool) storage.ServerStatus {
	if wasRunning {
		return storage.StatusRunning
	}
	if previous == storage.StatusDeepSleep {
		return storage.StatusDeepSleep
	}
	return storage.StatusStopped
}

func (s *ServerService) selectNodeWithHostCapacity(ctx context.Context, strategy docker.PlacementStrategy, requestedMemoryMB int64) (*storage.Node, error) {
	nodes, err := s.store.ListNodesWithStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query Docker nodes: %w", err)
	}

	// Probe hosts concurrently with a short per-node budget. A stale "online"
	// record with an unreachable Docker daemon must not be treated as unlimited.
	var wg sync.WaitGroup
	for _, node := range nodes {
		if !node.Enabled || node.Status != storage.NodeStatusOnline {
			continue
		}
		wg.Add(1)
		go func(node *storage.Node) {
			defer wg.Done()
			probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			hostMemoryMB, err := s.getHostMemoryMB(probeCtx, node.ID)
			if err != nil {
				node.Status = storage.NodeStatusOffline
				return
			}
			if node.MaxMemoryMB == 0 || hostMemoryMB < node.MaxMemoryMB {
				node.MaxMemoryMB = hostMemoryMB
			}
		}(node)
	}
	wg.Wait()

	eligible := docker.FilterEligibleNodes(nodes, requestedMemoryMB)
	if len(eligible) == 0 {
		return nil, fmt.Errorf("no reachable Docker node has capacity for %d MB", requestedMemoryMB)
	}
	switch strategy {
	case docker.StrategyLeastServers:
		return docker.SelectLeastServers(eligible), nil
	case docker.StrategyRoundRobin:
		return s.placementEngine.SelectRoundRobin(eligible), nil
	default:
		return docker.SelectLeastMemory(eligible), nil
	}
}

// SetClientPool sets the client pool on the service
func (s *ServerService) SetClientPool(pool *docker.ClientPool) {
	s.pool = pool
}

// SetPlacementEngine sets the placement engine on the service
func (s *ServerService) SetPlacementEngine(engine *docker.PlacementEngine) {
	s.placementEngine = engine
}

// auditPower records a server power event in the audit trail (MINE-141).
// Audit failures are logged, never returned.
func (s *ServerService) auditPower(ctx context.Context, ip, event, serverID string) {
	s.auditServerLifecycle(ctx, ip, event, serverID)
}

// auditServerLifecycle records a server lifecycle event in the audit trail (MINE-141, SEC-3).
// Audit failures are logged, never returned.
func (s *ServerService) auditServerLifecycle(ctx context.Context, ip, event, serverID string) {
	actorID, actorName := "", ""
	if u := auth.GetUserFromContext(ctx); u != nil {
		actorID, actorName = u.ID, u.Username
	}
	if err := activity.Log(s.store.DB(), activity.Entry{
		ActorID:    actorID,
		ActorName:  actorName,
		IP:         ip,
		Event:      event,
		SubjectTyp: "server",
		SubjectID:  serverID,
	}); err != nil {
		s.log.Error("Audit log failed for %s: %v", event, err)
	}
}

// auditPrivilegedOverrides records an immutable audit log entry whenever privileged
// Docker overrides are granted to a server (SEC-3).
func (s *ServerService) auditPrivilegedOverrides(ctx context.Context, ip, serverID string, overrides *v1.DockerOverrides) {
	if !docker.IsPrivilegedDockerOverride(overrides) {
		return
	}
	actorID, actorName := "", ""
	if u := auth.GetUserFromContext(ctx); u != nil {
		actorID, actorName = u.ID, u.Username
	}
	fields := docker.PrivilegedOverrideFields(overrides)
	propsJSON, _ := json.Marshal(map[string]any{
		"action":          "grant_docker_privileged_overrides",
		"override_fields": fields,
	})
	if err := activity.Log(s.store.DB(), activity.Entry{
		ActorID:    actorID,
		ActorName:  actorName,
		IP:         ip,
		Event:      activity.EventServerPrivilegedOverrides,
		SubjectTyp: "server",
		SubjectID:  serverID,
		Properties: string(propsJSON),
	}); err != nil {
		s.log.Error("Audit log failed for %s: %v", activity.EventServerPrivilegedOverrides, err)
	}
}

// NewServerService creates a new server service
func NewServerService(
	store *storage.Store,
	dockerCli *docker.Client,
	sender *command.Sender,
	config *config.Config,
	proxy *proxy.Manager,
	logStreamer *logger.LogStreamer,
	metricsCollector *metrics.Collector,
	moduleManager *module.Manager,
	bus *events.Bus,
	log *logger.Logger,
	pool *docker.ClientPool,
	placementEngine *docker.PlacementEngine,
	enforcer *rbac.Enforcer,
) *ServerService {
	if pool == nil && dockerCli != nil {
		pool = docker.NewClientPool(store, dockerCli, log)
	}
	if placementEngine == nil && store != nil {
		placementEngine = docker.NewPlacementEngine(store)
	}
	return &ServerService{
		store:            store,
		docker:           dockerCli,
		pool:             pool,
		placementEngine:  placementEngine,
		sender:           sender,
		config:           config,
		proxy:            proxy,
		log:              log,
		logStreamer:      logStreamer,
		metricsCollector: metricsCollector,
		moduleManager:    moduleManager,
		bus:              bus,
		enforcer:         enforcer,
	}
}

// checkDockerPrivilegedOverrides enforces that only callers holding the
// elevated ResourceServers/ActionManageDockerPrivileged permission may set
// host-breakout-capable Docker overrides (Privileged mode, CapAdd, or a
// confinement-weakening SecurityOpt). Benign overrides are always allowed.
// This is a payload-conditional check in addition to the ordinary
// create/update permission already enforced by the RPC auth interceptor.
func (s *ServerService) checkDockerPrivilegedOverrides(ctx context.Context, overrides *v1.DockerOverrides) error {
	if !docker.IsPrivilegedDockerOverride(overrides) {
		return nil
	}

	if s.enforcer == nil {
		return connect.NewError(connect.CodePermissionDenied, errors.New("privileged docker overrides require the servers/manage_docker_privileged permission, but the RBAC enforcer is unavailable"))
	}

	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}

	allowed, err := s.enforcer.Enforce(user.Roles, rbac.ResourceServers, rbac.ActionManageDockerPrivileged, "*")
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("rbac enforcement error: %w", err))
	}
	if !allowed {
		return connect.NewError(connect.CodePermissionDenied, docker.ErrElevatedDockerPermissionRequired)
	}
	return nil
}

// checkDockerOverridesConfinement rejects host-breakout-capable Docker
// overrides (host-path bind mounts outside the server's own data path, host
// device mappings, host/none networking) for ALL callers, including holders
// of the elevated docker permission. Unlike the permission-gated
// checkDockerPrivilegedOverrides above, this is a pure payload check that
// maps to CodeInvalidArgument. It must run before the overrides are persisted
// to the server record; container creation (ApplyOverrides) trusts the stored
// value and does not re-check.
func (s *ServerService) checkDockerOverridesConfinement(overrides *v1.DockerOverrides, serverDataPath string) error {
	if err := docker.ValidateDockerOverridesConfinement(overrides, docker.ServerOverrideSafeRoots(serverDataPath)...); err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	return nil
}

// dbServerToProto converts a database server model to proto server
func dbServerToProto(server *storage.Server) *v1.Server {
	if server == nil {
		return nil
	}

	// Convert JavaVersion string to int32
	javaVersion, _ := strconv.ParseInt(server.JavaVersion, 10, 32)

	protoServer := &v1.Server{
		Id:                      server.ID,
		Name:                    server.Name,
		Description:             server.Description,
		McVersion:               server.MCVersion,
		Port:                    int32(server.Port),
		ProxyHostname:           server.ProxyHostname,
		ProxyListenerId:         server.ProxyListenerID,
		ProxyPort:               int32(server.ProxyPort),
		MaxPlayers:              int32(server.MaxPlayers),
		Memory:                  int32(server.Memory),
		DataPath:                server.DataPath,
		ContainerId:             server.ContainerID,
		JavaVersion:             int32(javaVersion),
		DockerImage:             server.DockerImage,
		AutoStart:               server.AutoStart,
		Detached:                server.Detached,
		TpsCommand:              server.TPSCommand,
		AutoHibernate:           server.AutoHibernate,
		IdleTimeoutMinutes:      int32(server.IdleTimeoutMinutes),
		AutoDeepSleep:           server.AutoDeepSleep,
		DeepSleepTimeoutMinutes: int32(server.DeepSleepTimeoutMinutes),
		MemoryUsage:             int64(server.MemoryUsage),
		CpuPercent:              server.CPUPercent,
		DiskUsage:               server.DiskUsage,
		DiskTotal:               server.DiskTotal,
		WorldSize:               server.WorldSize,
		PlayersOnline:           int32(server.PlayersOnline),
		Tps:                     server.TPS,
		AdditionalPorts:         server.AdditionalPorts,
		CreatedAt:               timestamppb.New(server.CreatedAt),
		UpdatedAt:               timestamppb.New(server.UpdatedAt),

		// SLP fields
		SlpAvailable:    server.SLPAvailable,
		SlpLatencyMs:    server.SLPLatencyMs,
		Motd:            server.MOTD,
		ServerVersion:   server.ServerVersion,
		ProtocolVersion: int32(server.ProtocolVersion),
		PlayerSample:    server.PlayerSample,
		MaxPlayersSlp:   int32(server.MaxPlayersSLP),
		Favicon:         server.Favicon,
		NodeId:          server.NodeID,
	}

	// Apply overrides
	protoServer.DockerOverrides = server.DockerOverrides

	// Map mod loader
	protoServer.ModLoader = dbModLoaderToProto(server.ModLoader)

	// Map status
	protoServer.Status = dbStatusToProto(server.Status)

	// Map optional last started
	if server.LastStarted != nil {
		protoServer.LastStarted = timestamppb.New(*server.LastStarted)
	}

	return protoServer
}

// dbModLoaderToProto converts database mod loader to proto
func dbModLoaderToProto(loader storage.ModLoader) v1.ModLoader {
	switch loader {
	case storage.ModLoaderVanilla:
		return v1.ModLoader_MOD_LOADER_VANILLA
	case storage.ModLoaderForge:
		return v1.ModLoader_MOD_LOADER_FORGE
	case storage.ModLoaderFabric:
		return v1.ModLoader_MOD_LOADER_FABRIC
	case storage.ModLoaderQuilt:
		return v1.ModLoader_MOD_LOADER_QUILT
	case storage.ModLoaderPaper:
		return v1.ModLoader_MOD_LOADER_PAPER
	case storage.ModLoaderFolia:
		return v1.ModLoader_MOD_LOADER_FOLIA
	case storage.ModLoaderSpigot:
		return v1.ModLoader_MOD_LOADER_SPIGOT
	case storage.ModLoaderBukkit:
		return v1.ModLoader_MOD_LOADER_BUKKIT
	case storage.ModLoaderPurpur:
		return v1.ModLoader_MOD_LOADER_PURPUR
	case storage.ModLoaderSpongeVanilla:
		return v1.ModLoader_MOD_LOADER_SPONGE_VANILLA
	case storage.ModLoaderMohist:
		return v1.ModLoader_MOD_LOADER_MOHIST
	case storage.ModLoaderCatserver:
		return v1.ModLoader_MOD_LOADER_CATSERVER
	case storage.ModLoaderArclight:
		return v1.ModLoader_MOD_LOADER_ARCLIGHT
	case storage.ModLoaderAutoCurseForge:
		return v1.ModLoader_MOD_LOADER_AUTO_CURSEFORGE
	case storage.ModLoaderModrinth:
		return v1.ModLoader_MOD_LOADER_MODRINTH
	case storage.ModLoaderNeoForge:
		return v1.ModLoader_MOD_LOADER_NEOFORGE
	default:
		return v1.ModLoader_MOD_LOADER_VANILLA
	}
}

// protoModLoaderToDB converts proto mod loader to database
func protoModLoaderToDB(loader v1.ModLoader) storage.ModLoader {
	switch loader {
	case v1.ModLoader_MOD_LOADER_VANILLA:
		return storage.ModLoaderVanilla
	case v1.ModLoader_MOD_LOADER_FORGE:
		return storage.ModLoaderForge
	case v1.ModLoader_MOD_LOADER_FABRIC:
		return storage.ModLoaderFabric
	case v1.ModLoader_MOD_LOADER_QUILT:
		return storage.ModLoaderQuilt
	case v1.ModLoader_MOD_LOADER_PAPER:
		return storage.ModLoaderPaper
	case v1.ModLoader_MOD_LOADER_FOLIA:
		return storage.ModLoaderFolia
	case v1.ModLoader_MOD_LOADER_SPIGOT:
		return storage.ModLoaderSpigot
	case v1.ModLoader_MOD_LOADER_BUKKIT:
		return storage.ModLoaderBukkit
	case v1.ModLoader_MOD_LOADER_PURPUR:
		return storage.ModLoaderPurpur
	case v1.ModLoader_MOD_LOADER_SPONGE_VANILLA:
		return storage.ModLoaderSpongeVanilla
	case v1.ModLoader_MOD_LOADER_MOHIST:
		return storage.ModLoaderMohist
	case v1.ModLoader_MOD_LOADER_CATSERVER:
		return storage.ModLoaderCatserver
	case v1.ModLoader_MOD_LOADER_ARCLIGHT:
		return storage.ModLoaderArclight
	case v1.ModLoader_MOD_LOADER_AUTO_CURSEFORGE:
		return storage.ModLoaderAutoCurseForge
	case v1.ModLoader_MOD_LOADER_MODRINTH:
		return storage.ModLoaderModrinth
	case v1.ModLoader_MOD_LOADER_NEOFORGE:
		return storage.ModLoaderNeoForge
	default:
		return storage.ModLoaderVanilla
	}
}

// parseModLoaderString normalizes any modloader string or proto enum name to storage.ModLoader
func parseModLoaderString(s string) storage.ModLoader {
	clean := strings.TrimSpace(s)
	if clean == "" {
		return storage.ModLoaderVanilla
	}
	if val, ok := v1.ModLoader_value[clean]; ok {
		return protoModLoaderToDB(v1.ModLoader(val))
	}
	clean = strings.TrimPrefix(clean, "MOD_LOADER_")
	switch strings.ToLower(clean) {
	case "vanilla":
		return storage.ModLoaderVanilla
	case "forge":
		return storage.ModLoaderForge
	case "fabric":
		return storage.ModLoaderFabric
	case "quilt":
		return storage.ModLoaderQuilt
	case "paper":
		return storage.ModLoaderPaper
	case "folia":
		return storage.ModLoaderFolia
	case "spigot":
		return storage.ModLoaderSpigot
	case "bukkit":
		return storage.ModLoaderBukkit
	case "purpur":
		return storage.ModLoaderPurpur
	case "spongevanilla", "sponge_vanilla":
		return storage.ModLoaderSpongeVanilla
	case "mohist":
		return storage.ModLoaderMohist
	case "catserver":
		return storage.ModLoaderCatserver
	case "arclight":
		return storage.ModLoaderArclight
	case "auto_curseforge", "curseforge":
		return storage.ModLoaderAutoCurseForge
	case "modrinth":
		return storage.ModLoaderModrinth
	case "neoforge":
		return storage.ModLoaderNeoForge
	case "custom":
		return storage.ModLoaderCustom
	default:
		return storage.ModLoader(strings.ToLower(clean))
	}
}

// preserveStoredStatus reports whether the stored DB status owns the display
// value and must not be clobbered by a live Docker inspect in List/Get.
//
//   - DeepSleep: the container is EXPECTED to be stopped (exited). Docker
//     reports StatusStopped for it, which would flip the UI to OFFLINE/STOPPED
//     and hide the DEEP SLEEP state + Wake action (MINE-172). The
//     DeepSleepManager owns this state and flips it to Running on wake.
//   - Paused: the container is cgroup-frozen and still exists. A Stopped
//     reading means the daemon lost the pause state; keep PAUSED until the
//     reconciler converges rather than flapping the UI (MINE-172).
func preserveStoredStatus(stored, live storage.ServerStatus) bool {
	if stored == storage.StatusDeepSleep {
		return true
	}
	if stored == storage.StatusPaused && live == storage.StatusStopped {
		return true
	}
	return false
}

// dbStatusToProto converts database status to proto
func dbStatusToProto(status storage.ServerStatus) v1.ServerStatus {
	switch status {
	case storage.StatusCreating:
		return v1.ServerStatus_SERVER_STATUS_CREATING
	case storage.StatusStarting:
		return v1.ServerStatus_SERVER_STATUS_STARTING
	case storage.StatusRunning:
		return v1.ServerStatus_SERVER_STATUS_RUNNING
	case storage.StatusStopping:
		return v1.ServerStatus_SERVER_STATUS_STOPPING
	case storage.StatusStopped:
		return v1.ServerStatus_SERVER_STATUS_STOPPED
	case storage.StatusError:
		return v1.ServerStatus_SERVER_STATUS_ERROR
	case storage.StatusUnhealthy:
		return v1.ServerStatus_SERVER_STATUS_UNHEALTHY
	case storage.StatusPaused:
		return v1.ServerStatus_SERVER_STATUS_PAUSED
	case storage.StatusDeepSleep:
		return v1.ServerStatus_SERVER_STATUS_DEEPSLEEP
	default:
		return v1.ServerStatus_SERVER_STATUS_UNSPECIFIED
	}
}

// ListServers lists all servers
func (s *ServerService) ListServers(ctx context.Context, req *connect.Request[v1.ListServersRequest]) (*connect.Response[v1.ListServersResponse], error) {
	servers, err := s.store.ListServers(ctx)
	if err != nil {
		s.log.Error("Failed to list servers: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list servers"))
	}

	// Get all proxy listeners once for efficiency
	var listeners map[string]*storage.ProxyListener
	if s.config.Proxy.Enabled {
		allListeners, err := s.store.GetProxyListeners(ctx)
		if err == nil {
			listeners = make(map[string]*storage.ProxyListener)
			for _, l := range allListeners {
				listeners[l.ID] = l
			}
		}
	}

	// Update status from Docker and apply cached metrics. The live refresh runs
	// under one shared budget so N servers on an unresponsive node cannot sum
	// up to N times the per-call timeout.
	statusCtx, cancelStatus := context.WithTimeout(ctx, statusRefreshTimeout)
	defer cancelStatus()

	for _, server := range servers {
		// If server uses proxy, ensure ProxyPort is populated from the listener
		if server.ProxyHostname != "" && server.ProxyListenerID != "" && listeners != nil {
			if listener, ok := listeners[server.ProxyListenerID]; ok {
				server.ProxyPort = listener.Port
			}
		}

		if server.ContainerID != "" {
			dockerCli := s.getDockerClient(server.NodeID)
			if dockerCli != nil {
				status, err := dockerCli.GetContainerStatus(statusCtx, server.ContainerID)
				if err == nil && !preserveStoredStatus(server.Status, status) {
					server.Status = status
				}
			}

			// Apply cached metrics from the background collector
			if s.metricsCollector != nil {
				if m := s.metricsCollector.GetMetrics(server.ID); m != nil {
					server.MemoryUsage = m.MemoryUsage
					server.CPUPercent = m.CPUPercent
					server.DiskUsage = m.DiskUsage
					server.DiskTotal = m.DiskTotal
					server.WorldSize = m.WorldSize
					server.PlayersOnline = m.PlayersOnline
					server.TPS = m.TPS

					// SLP fields
					server.SLPAvailable = m.SLPAvailable
					server.SLPLatencyMs = m.SLPLatencyMs
					server.MOTD = m.MOTD
					server.ServerVersion = m.ServerVersion
					server.ProtocolVersion = m.ProtocolVersion
					server.PlayerSample = m.PlayerSample
					server.MaxPlayersSLP = m.MaxPlayers
				}
			}
		}
	}

	// Convert to proto
	protoServers := make([]*v1.Server, len(servers))
	for i, server := range servers {
		protoServers[i] = dbServerToProto(server)
	}

	return connect.NewResponse(&v1.ListServersResponse{
		Servers: protoServers,
	}), nil
}

// GetServer gets a specific server
func (s *ServerService) GetServer(ctx context.Context, req *connect.Request[v1.GetServerRequest]) (*connect.Response[v1.GetServerResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// If server uses proxy, ensure ProxyPort is populated from the listener
	if server.ProxyHostname != "" && server.ProxyListenerID != "" {
		listener, err := s.store.GetProxyListener(ctx, server.ProxyListenerID)
		if err == nil && listener != nil {
			server.ProxyPort = listener.Port
		}
	}

	// Update status from Docker, bounded so an unresponsive node cannot outlive
	// the HTTP write timeout (the stored status is the fallback).
	if server.ContainerID != "" {
		dockerCli := s.getDockerClient(server.NodeID)
		if dockerCli != nil {
			statusCtx, cancelStatus := context.WithTimeout(ctx, statusRefreshTimeout)
			status, err := dockerCli.GetContainerStatus(statusCtx, server.ContainerID)
			cancelStatus()
			if err == nil && !preserveStoredStatus(server.Status, status) {
				server.Status = status
			}
		}
	}

	// Apply cached metrics from the background collector
	if s.metricsCollector != nil {
		if m := s.metricsCollector.GetMetrics(server.ID); m != nil {
			server.MemoryUsage = m.MemoryUsage
			server.CPUPercent = m.CPUPercent
			server.DiskUsage = m.DiskUsage
			server.DiskTotal = m.DiskTotal
			server.WorldSize = m.WorldSize
			server.PlayersOnline = m.PlayersOnline
			server.TPS = m.TPS

			// SLP fields
			server.SLPAvailable = m.SLPAvailable
			server.SLPLatencyMs = m.SLPLatencyMs
			server.MOTD = m.MOTD
			server.ServerVersion = m.ServerVersion
			server.ProtocolVersion = m.ProtocolVersion
			server.PlayerSample = m.PlayerSample
			server.MaxPlayersSLP = m.MaxPlayers
			server.Favicon = m.Favicon
		}
	}

	return connect.NewResponse(&v1.GetServerResponse{
		Server: dbServerToProto(server),
	}), nil
}

// CreateServer creates a new server
func (s *ServerService) CreateServer(ctx context.Context, req *connect.Request[v1.CreateServerRequest]) (*connect.Response[v1.CreateServerResponse], error) {
	msg := req.Msg

	if err := s.checkDockerPrivilegedOverrides(ctx, msg.DockerOverrides); err != nil {
		return nil, err
	}

	// Convert mod loader from proto
	modLoader := protoModLoaderToDB(msg.ModLoader)

	// If modpack is selected, load it and derive settings
	var modpackURL string
	if msg.ModpackId != "" {
		modpack, err := s.store.GetIndexedModpack(ctx, msg.ModpackId)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid modpack"))
		}

		// Set the modpack URL
		modpackURL = modpack.WebsiteURL

		// Override mod loader based on indexer
		switch modpack.Indexer {
		case "fuego", "manual":
			modLoader = storage.ModLoaderAutoCurseForge
		case "modrinth":
			modLoader = storage.ModLoaderModrinth
		}

		// Get MC version from modpack if not explicitly set
		if msg.McVersion == "" {
			var gameVersions []string
			if err := json.Unmarshal([]byte(modpack.GameVersions), &gameVersions); err == nil && len(gameVersions) > 0 {
				msg.McVersion = minecraft.FindMostRecentMinecraftVersion(gameVersions)
			}
		}

		// Set minimum memory for modpacks
		if msg.Memory < 4096 {
			msg.Memory = 4096
		}
	}

	// Validate request
	if msg.Name == "" || msg.McVersion == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name and MC version are required"))
	}

	// Handle proxy configuration
	proxyHostname := msg.ProxyHostname
	proxyListenerID := msg.ProxyListenerId
	port := int(msg.Port)

	// If auto deep sleep is enabled but no proxy hostname is provided,
	// auto-generate one if proxy is enabled globally so wake-on-login works (MINE-172).
	if msg.AutoDeepSleep && proxyHostname == "" && s.config != nil && s.config.Proxy.Enabled {
		proxyConfig, _, err := s.store.GetProxyConfig(ctx)
		if err == nil && proxyConfig != nil && proxyConfig.Enabled {
			baseName := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(msg.Name), " ", "-"))
			if baseName == "" {
				baseName = "mc-server"
			}
			if proxyConfig.BaseURL != "" {
				proxyHostname = baseName + "." + proxyConfig.BaseURL
			} else {
				proxyHostname = baseName
			}
		}
	}

	if proxyHostname != "" {
		// If using base URL, append it to the hostname
		if msg.UseBaseUrl {
			proxyConfig, _, err := s.store.GetProxyConfig(ctx)
			if err == nil && proxyConfig.BaseURL != "" {
				// Only append base URL if hostname doesn't already contain a domain
				if !strings.Contains(proxyHostname, ".") {
					proxyHostname = proxyHostname + "." + proxyConfig.BaseURL
				}
			}
		}

		// Validate listener selection
		if proxyListenerID != "" {
			listener, err := s.store.GetProxyListener(ctx, proxyListenerID)
			if err != nil || !listener.Enabled {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid or disabled proxy listener"))
			}
			port = listener.Port
		} else {
			// No listener specified, get the default one
			listeners, err := s.store.GetProxyListeners(ctx)
			if err != nil || len(listeners) == 0 {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("no proxy listeners configured"))
			}

			// Find default or first enabled listener
			var defaultListener *storage.ProxyListener
			for _, l := range listeners {
				if l.IsDefault && l.Enabled {
					defaultListener = l
					break
				}
			}
			if defaultListener == nil {
				for _, l := range listeners {
					if l.Enabled {
						defaultListener = l
						break
					}
				}
			}
			if defaultListener == nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("no enabled proxy listeners available"))
			}
			proxyListenerID = defaultListener.ID
			port = defaultListener.Port
		}
	} else {
		// For non-proxy servers, must have a unique port
		if port == 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port is required for non-proxy servers"))
		}

		// Check if port is already in use
		existing, err := s.store.GetServerByPort(ctx, port)
		if err != nil {
			s.log.Error("Failed to check port: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check port availability"))
		}
		if existing != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port already in use"))
		}

		// Also check if this port is used by the proxy
		if s.config.Proxy.Enabled {
			if slices.Contains(s.config.Proxy.ListenPorts, port) {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port is already in use by the proxy server"))
			}
		}
	}

	// Determine Docker image if not specified
	dockerImage := msg.DockerImage
	if dockerImage == "" {
		dockerImage = docker.GetOptimalDockerTag(msg.McVersion, modLoader, false)
	}

	// Validate additional ports
	var additionalPorts []*v1.AdditionalPort
	usedPorts := make(map[string]bool)

	for _, protoPort := range msg.AdditionalPorts {
		// Validate port range
		if protoPort.ContainerPort < 1 || protoPort.ContainerPort > 65535 {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid container port %d", protoPort.ContainerPort))
		}
		if protoPort.HostPort < 1 || protoPort.HostPort > 65535 {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid host port %d", protoPort.HostPort))
		}

		// Default protocol to TCP
		protocol := protoPort.Protocol
		if protocol == "" {
			protocol = "tcp"
		} else if protocol != "tcp" && protocol != "udp" {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid protocol %s (must be tcp or udp)", protocol))
		}

		// Check for duplicate ports
		portKey := fmt.Sprintf("%d/%s", protoPort.HostPort, protocol)
		if usedPorts[portKey] {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("duplicate host port %d/%s", protoPort.HostPort, protocol))
		}
		usedPorts[portKey] = true

		// Check if port conflicts
		if int(protoPort.HostPort) == port {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("additional port %d conflicts with main server port", protoPort.HostPort))
		}
		if s.config.Proxy.Enabled && slices.Contains(s.config.Proxy.ListenPorts, int(protoPort.HostPort)) {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port %d is already in use by the proxy server", protoPort.HostPort))
		}

		additionalPorts = append(additionalPorts, &v1.AdditionalPort{
			ContainerPort: protoPort.ContainerPort,
			HostPort:      protoPort.HostPort,
			Protocol:      protocol,
			Name:          protoPort.Name,
			ExposePublic:  protoPort.ExposePublic,
		})
	}

	// Create server object
	serverUUID := uuid.New().String()
	serverDataDir := fmt.Sprintf("%s_%s", files.SanitizePathName(msg.Name), serverUUID)
	serverDataPath := filepath.Join(s.config.Storage.DataDir, "servers", serverDataDir)

	server := &storage.Server{
		ID:              serverUUID,
		Name:            msg.Name,
		Description:     msg.Description,
		ModLoader:       modLoader,
		MCVersion:       msg.McVersion,
		Status:          storage.StatusCreating,
		Port:            port,
		ProxyHostname:   proxyHostname,
		ProxyListenerID: proxyListenerID,
		MaxPlayers:      int(msg.MaxPlayers),
		Memory:          int(msg.Memory),
		DataPath:        serverDataPath,
		JavaVersion:     docker.GetRequiredJavaVersion(msg.McVersion, modLoader),
		DockerImage:     dockerImage,
		AutoStart:       msg.AutoStart,
		Detached:        msg.Detached,
		TPSCommand:      minecraft.GetTPSCommand(modLoader),
		AutoHibernate:   msg.AutoHibernate,
		AutoDeepSleep:   msg.AutoDeepSleep,
		AdditionalPorts: additionalPorts,
		DockerOverrides: msg.DockerOverrides,
	}

	// Confinement check for the volume/device/network gap. This runs here —
	// not next to the privileged-overrides check at the top of this function
	// — because the safe roots are derived from the server's own data path,
	// which is only assigned above.
	if err := s.checkDockerOverridesConfinement(msg.DockerOverrides, serverDataPath); err != nil {
		return nil, err
	}

	// Set defaults
	if server.MaxPlayers == 0 {
		server.MaxPlayers = 20
	}
	if server.Memory == 0 {
		server.Memory = 4096
	}
	if server.ModLoader == "" {
		server.ModLoader = storage.ModLoaderVanilla
	}

	// When using proxy, remember the player-facing listener. The backend port is
	// allocated after the node is known so two servers do not publish the same bind.
	if server.ProxyHostname != "" && proxyListenerID != "" {
		listener, err := s.store.GetProxyListener(ctx, proxyListenerID)
		if err == nil && listener != nil {
			server.ProxyPort = listener.Port
		}
	}

	// Determine node placement
	nodeID := msg.NodeId
	if nodeID == "" && s.placementEngine != nil {
		strategy := docker.StrategyRoundRobin
		if msg.PlacementStrategy != "" {
			strategy = docker.PlacementStrategy(strings.ToLower(msg.PlacementStrategy))
		}
		selectedNode, err := s.selectNodeWithHostCapacity(ctx, strategy, int64(server.Memory))
		if err != nil {
			s.log.Error("Failed to select node using strategy %s: %v", strategy, err)
			return nil, connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("placement failed: %w", err))
		}
		nodeID = selectedNode.ID
	} else if nodeID != "" {
		if s.store != nil {
			node, err := s.store.GetNode(ctx, nodeID)
			if err != nil {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("specified node %s not found: %w", nodeID, err))
			}
			if !node.Enabled {
				return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("specified node %s is disabled", node.Name))
			}
		}
	}
	if nodeID == "" {
		return nil, connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("no Docker nodes available: register and enable a node in Settings before creating servers"))
	}
	server.NodeID = nodeID

	requestID := strings.TrimSpace(msg.GetRequestId())
	if err := operation.ValidateRequestID(requestID); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	releaseClaim := false
	if requestID != "" {
		owner, claimed, claimErr := s.store.ClaimIdempotencyKey(ctx, requestID, server.ID, "create")
		if claimErr != nil {
			code := connect.CodeInternal
			if errors.Is(claimErr, storage.ErrRequestIDTooLong) || errors.Is(claimErr, storage.ErrRequestIDReused) {
				code = connect.CodeInvalidArgument
			}
			return nil, connect.NewError(code, claimErr)
		}
		if !claimed {
			existing, getErr := s.store.GetServer(ctx, owner)
			if getErr != nil {
				return nil, connect.NewError(connect.CodeAborted, fmt.Errorf("create request is already in progress"))
			}
			return connect.NewResponse(&v1.CreateServerResponse{Server: dbServerToProto(existing)}), nil
		}
		releaseClaim = true
		defer func() {
			if releaseClaim {
				_ = s.store.ReleaseIdempotencyKey(context.WithoutCancel(ctx), requestID)
			}
		}()
	}

	backendReserved := false
	if server.ProxyHostname != "" {
		used := map[int]bool{}
		dockerCli, err := s.getDockerClientStrict(server.NodeID)
		if err != nil || dockerCli == nil {
			if err == nil {
				err = fmt.Errorf("no Docker client available")
			}
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("cannot check allocated backend ports on node %s: %w", server.NodeID, err))
		}
		allocated, err := dockerCli.GetAllocatedHostPorts(ctx)
		if err != nil {
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("cannot check allocated backend ports on node %s: %w", server.NodeID, err))
		}
		used = allocated
		gamePort, _, err := s.store.ReserveBackendPortPair(ctx, server.NodeID, server.ID, used)
		if err != nil {
			return nil, connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("no free backend port on node: %w", err))
		}
		server.Port = gamePort
		backendReserved = true
	}
	keepLease := false
	defer func() {
		if backendReserved && !keepLease {
			_ = s.store.ReleaseBackendPorts(context.WithoutCancel(ctx), server.ID)
		}
	}()

	// Check port availability on host and target node for non-proxy servers
	if server.ProxyHostname == "" && server.Port > 0 {
		if err := s.checkNodeHostPort(ctx, server.NodeID, server.Port); err != nil {
			return nil, err
		}
	}

	// The creation task continues after this RPC returns. Claim the same
	// lifecycle transition used by start/stop/update/delete so those operations
	// cannot race container creation or its final database writes.
	transition, err := docker.TryBeginServerTransition(server.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeAborted, err)
	}
	transitionHeld := true
	defer func() {
		if transitionHeld {
			transition.Release()
		}
	}()

	// Serialize capacity check and DB reservation so concurrent create
	// requests cannot both observe the same free memory and overcommit a node.
	s.capacityMu.Lock()
	if err := s.checkNodeMemoryCapacity(ctx, server.NodeID, int64(server.Memory)); err != nil {
		s.capacityMu.Unlock()
		return nil, err
	}

	// Create the world on the node's storage. A remote node with no reachable
	// mount is refused before a controller-local directory can be mistaken for
	// the world the remote daemon will use.
	worldRoot, err := dataRootFor(ctx, s.store, server, true)
	if err != nil {
		s.capacityMu.Unlock()
		if nodestorage.IsUnavailable(err) {
			return nil, connect.NewError(connect.CodeUnavailable, err)
		}
		s.log.Error("Failed to create data directory: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create server directory"))
	}

	// Save to database
	if err := s.store.CreateServer(ctx, server); err != nil {
		s.capacityMu.Unlock()
		s.log.Error("Failed to create server: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create server"))
	}
	keepLease = true
	releaseClaim = false
	s.capacityMu.Unlock()

	// Get the server config
	serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
	if err != nil {
		s.log.Error("Failed to get server config: %v", err)
		serverConfig = s.store.CreateDefaultServerConfig(server.ID)
	}

	// Set memory configuration with dynamic headroom and OOM guard (MINE-4)
	alloc := utils.CalculateMemoryAllocation(server.Memory)
	if serverConfig.MaxMemory == nil && serverConfig.Memory == nil && serverConfig.InitMemory == nil {
		serverConfig.MaxMemory = &alloc.MaxMemoryStr
		serverConfig.InitMemory = &alloc.InitMemoryStr
	}

	if serverConfig.Memory != nil {
		serverConfig.MaxMemory = serverConfig.Memory
		serverConfig.InitMemory = serverConfig.Memory
	}

	if err := s.store.UpdateServerConfig(ctx, serverConfig); err != nil {
		s.log.Error("Failed to update server config with memory settings: %v", err)
	}

	// Configure modpack if selected
	if msg.ModpackId != "" {
		modpack, _ := s.store.GetIndexedModpack(ctx, msg.ModpackId)
		if modpack != nil && modpack.Indexer == "manual" {
			// For manual modpacks, copy the zip file
			modpackFile, err := s.store.GetIndexedModpackFiles(ctx, msg.ModpackId)
			if err == nil && len(modpackFile) > 0 {
				sourcePath := modpackFile[0].DownloadURL
				destPath := filepath.Join(worldRoot, "modpack.zip")

				// Copy the modpack file safely
				copyErr := func() error {
					sourceFile, err := os.Open(sourcePath)
					if err != nil {
						return err
					}
					defer sourceFile.Close()

					destFile, err := os.Create(destPath)
					if err != nil {
						return err
					}
					defer destFile.Close()

					if _, err := io.Copy(destFile, sourceFile); err != nil {
						return err
					}
					return nil
				}()
				if copyErr == nil {
					// Set CF_MODPACK_ZIP for manual modpack
					cfModpackZip := "/data/modpack.zip"
					serverConfig.CFModpackZip = &cfModpackZip

					// Set a dummy slug
					cfSlug := "manual-" + modpack.ID
					serverConfig.CFSlug = &cfSlug
				} else {
					s.log.Error("Failed to copy manual modpack file %s: %v", sourcePath, copyErr)
					_ = os.Remove(destPath)
				}
			}
		} else if modpackURL != "" && server.ModLoader == storage.ModLoaderAutoCurseForge {
			// If version is pinned, append /files/<id>
			if msg.ModpackVersionId != "" {
				versionedURL := fmt.Sprintf("%s/files/%s", modpackURL, msg.ModpackVersionId)
				serverConfig.CFPageURL = &versionedURL
			} else {
				serverConfig.CFPageURL = &modpackURL
			}
		} else if modpack != nil && modpack.Indexer == "modrinth" {
			configureModrinthModpack(serverConfig, modpack.IndexerID, msg.ModpackVersionId)
		}

		// Update config with modpack settings
		if err := s.store.UpdateServerConfig(ctx, serverConfig); err != nil {
			s.log.Error("Failed to update server config with modpack settings: %v", err)
		}
	}

	// The create task can outlive this RPC. Give it a private snapshot and use
	// targeted writes for its lifecycle fields so a later UpdateServer cannot
	// be overwritten by a stale full-row Save from this goroutine.
	createServer := *server
	startImmediately := msg.StartImmediately
	go func(server *storage.Server, startImmediately bool, transition *docker.Transition) {
		defer transition.Release()
		bgCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		s.log.Info("Starting async Docker container creation for server %s", server.ID)

		s.prepareServerConfig(bgCtx, server, serverConfig)
		dockerCli, err := s.getDockerClientStrict(server.NodeID)
		if err != nil {
			s.log.Error("Failed to get docker client for node %q: %v", server.NodeID, err)
			if updateErr := s.store.UpdateServerStatus(bgCtx, server.ID, storage.StatusError); updateErr != nil {
				s.log.Error("Failed to update server status to error: %v", updateErr)
			}
			return
		}
		var containerID string
		if err := s.runJournaledMutation(bgCtx, server, "create", requestID, func() error {
			var createErr error
			containerID, createErr = dockerCli.CreateContainer(bgCtx, server, serverConfig)
			return createErr
		}); err != nil {
			s.log.Error("Failed to create container: %v", err)
			if updateErr := s.store.UpdateServerStatus(bgCtx, server.ID, storage.StatusError); updateErr != nil {
				s.log.Error("Failed to update server status to error: %v", updateErr)
			}
			return
		}

		server.ContainerID = containerID
		s.log.Info("Container created successfully for server %s: %s", server.ID, containerID)

		// Update server with container ID
		if err := s.store.UpdateServerContainerID(bgCtx, server.ID, containerID); err != nil {
			s.log.Error("Failed to update server with container ID: %v", err)
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(bgCtx), 10*time.Second)
			defer cancel()
			if cleanupErr := dockerCli.RemoveContainer(cleanupCtx, containerID); cleanupErr != nil {
				s.log.Error("Failed to remove untracked container %s after identity persistence failed: %v", containerID, cleanupErr)
			}
			if statusErr := s.store.UpdateServerStatus(cleanupCtx, server.ID, storage.StatusError); statusErr != nil {
				s.log.Error("Failed to mark server %s as errored after container identity persistence failed: %v", server.ID, statusErr)
			}
			return
		}

		// Start the container immediately if requested
		if startImmediately {
			if err := s.runJournaledMutation(bgCtx, server, "start", requestID, func() error {
				return dockerCli.StartContainer(bgCtx, containerID)
			}); err != nil {
				s.log.Error("Failed to start container: %v", err)
				server.Status = storage.StatusError
				if updateErr := s.store.UpdateServerStatus(bgCtx, server.ID, storage.StatusError); updateErr != nil {
					s.log.Error("Failed to update server status to error: %v", updateErr)
				}
			} else {
				server.Status = storage.StatusStarting
				// Update last started time
				now := time.Now()
				server.LastStarted = &now
				// Clear ephemeral configuration fields
				if err := s.store.ClearEphemeralConfigFields(bgCtx, server.ID); err != nil {
					s.log.Error("Failed to clear ephemeral config fields: %v", err)
				}
			}
			// Update only lifecycle fields; settings may have changed since this
			// asynchronous operation began.
			fields := map[string]interface{}{"status": server.Status}
			if server.LastStarted != nil {
				fields["last_started"] = server.LastStarted
			}
			if err := s.store.UpdateServerFields(bgCtx, server.ID, fields); err != nil {
				s.log.Error("Failed to update server status: %v", err)
			}
			// Update proxy route if enabled
			if s.proxy != nil && server.ProxyHostname != "" {
				if err := s.proxy.UpdateServerRoute(server); err != nil {
					s.log.Error("Failed to update proxy route for newly created server: %v", err)
				}
			}
		} else {
			// Update status to stopped once container is ready
			server.Status = storage.StatusStopped
			if err := s.store.UpdateServerStatus(bgCtx, server.ID, server.Status); err != nil {
				s.log.Error("Failed to update server status: %v", err)
			}
			s.log.Info("Server %s created but not started immediately", server.ID)
		}
	}(&createServer, startImmediately, transition)
	transitionHeld = false // ownership transferred to the asynchronous task

	peerIP := req.Peer().Addr
	s.auditServerLifecycle(ctx, peerIP, activity.EventServerCreate, server.ID)
	if docker.IsPrivilegedDockerOverride(msg.DockerOverrides) {
		s.auditPrivilegedOverrides(ctx, peerIP, server.ID, msg.DockerOverrides)
	}

	// Return immediately with the server in "creating" state
	return connect.NewResponse(&v1.CreateServerResponse{
		Server: dbServerToProto(server),
	}), nil
}

// UpdateServer updates a server
func (s *ServerService) UpdateServer(ctx context.Context, req *connect.Request[v1.UpdateServerRequest]) (*connect.Response[v1.UpdateServerResponse], error) {
	transition, err := beginServerLifecycleTransition(req.Msg.Id)
	if err != nil {
		return nil, err
	}
	defer transition.Release()

	msg := req.Msg

	server, err := s.store.GetServer(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Check if container recreation is needed
	needsRecreation := false
	originalMemory := server.Memory
	originalModLoader := server.ModLoader
	originalMCVersion := server.MCVersion
	originalDockerImage := server.DockerImage
	capacityLockHeld := false
	defer func() {
		if capacityLockHeld {
			s.capacityMu.Unlock()
		}
	}()

	// Update fields
	if msg.Name != "" {
		server.Name = msg.Name
	}
	if msg.Description != "" {
		server.Description = msg.Description
	}
	if msg.Port != nil && int(*msg.Port) != server.Port {
		newPort := int(*msg.Port)

		if server.ProxyHostname != "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("cannot change port for proxy-enabled servers"))
		}

		if newPort < 1 || newPort > 65535 {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid port %d", newPort))
		}

		existing, err := s.store.GetServerByPort(ctx, newPort)
		if err != nil {
			s.log.Error("Failed to check port: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check port availability"))
		}
		if existing != nil && existing.ID != server.ID {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port already in use"))
		}

		if s.config.Proxy.Enabled && slices.Contains(s.config.Proxy.ListenPorts, newPort) {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port is already in use by the proxy server"))
		}

		if err := s.checkNodeHostPort(ctx, server.NodeID, newPort); err != nil {
			return nil, err
		}

		server.Port = newPort
		needsRecreation = true
	}
	if msg.MaxPlayers > 0 {
		server.MaxPlayers = int(msg.MaxPlayers)
		needsRecreation = true
	}
	if msg.Memory > 0 && int(msg.Memory) != originalMemory {
		if int(msg.Memory) > originalMemory {
			s.capacityMu.Lock()
			capacityLockHeld = true
			if err := s.checkNodeMemoryCapacity(ctx, server.NodeID, int64(msg.Memory)-int64(originalMemory)); err != nil {
				return nil, err
			}
		}
		server.Memory = int(msg.Memory)
		needsRecreation = true
		if err := s.store.UpdateServerConfigMemory(ctx, server.ID, int(msg.Memory)); err != nil {
			s.log.Error("Failed to update server config memory: %v", err)
		}
	}
	if msg.ModLoader != "" {
		newLoader := parseModLoaderString(msg.ModLoader)
		if newLoader != originalModLoader {
			server.ModLoader = newLoader
			server.TPSCommand = minecraft.GetTPSCommand(server.ModLoader)
			needsRecreation = true
		}
	}
	if msg.McVersion != "" && msg.McVersion != originalMCVersion {
		server.MCVersion = msg.McVersion
		needsRecreation = true
	}
	if msg.DockerImage != "" && msg.DockerImage != originalDockerImage {
		server.DockerImage = msg.DockerImage
		needsRecreation = true
	}
	if msg.AutoStart != nil {
		server.AutoStart = *msg.AutoStart
	}
	if msg.Detached != nil {
		server.Detached = *msg.Detached
	}
	if msg.TpsCommand != nil {
		server.TPSCommand = *msg.TpsCommand
	}
	if msg.AutoHibernate != nil {
		server.AutoHibernate = *msg.AutoHibernate
	}
	if msg.IdleTimeoutMinutes != nil {
		if int(*msg.IdleTimeoutMinutes) < 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid idle timeout %d", *msg.IdleTimeoutMinutes))
		}
		server.IdleTimeoutMinutes = int(*msg.IdleTimeoutMinutes)
	}
	if msg.AutoDeepSleep != nil {
		server.AutoDeepSleep = *msg.AutoDeepSleep
	}
	if msg.DeepSleepTimeoutMinutes != nil {
		if int(*msg.DeepSleepTimeoutMinutes) < 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid deep sleep timeout %d", *msg.DeepSleepTimeoutMinutes))
		}
		server.DeepSleepTimeoutMinutes = int(*msg.DeepSleepTimeoutMinutes)
	}

	// Handle additional ports update
	if len(msg.AdditionalPorts) > 0 {
		// Validate additional ports
		var additionalPorts []*v1.AdditionalPort
		usedPorts := make(map[string]bool)

		for _, protoPort := range msg.AdditionalPorts {
			// Validate port range
			if protoPort.ContainerPort < 1 || protoPort.ContainerPort > 65535 {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid container port %d", protoPort.ContainerPort))
			}
			if protoPort.HostPort < 1 || protoPort.HostPort > 65535 {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid host port %d", protoPort.HostPort))
			}

			// Default protocol to TCP
			protocol := protoPort.Protocol
			if protocol == "" {
				protocol = "tcp"
			} else if protocol != "tcp" && protocol != "udp" {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid protocol %s", protocol))
			}

			// Check for duplicate ports
			portKey := fmt.Sprintf("%d/%s", protoPort.HostPort, protocol)
			if usedPorts[portKey] {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("duplicate host port %d/%s", protoPort.HostPort, protocol))
			}
			usedPorts[portKey] = true

			// Check if port conflicts
			if int(protoPort.HostPort) == server.Port {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("additional port %d conflicts with main server port", protoPort.HostPort))
			}
			if s.config.Proxy.Enabled && slices.Contains(s.config.Proxy.ListenPorts, int(protoPort.HostPort)) {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port %d is already in use by the proxy server", protoPort.HostPort))
			}

			additionalPorts = append(additionalPorts, &v1.AdditionalPort{
				ContainerPort: protoPort.ContainerPort,
				HostPort:      protoPort.HostPort,
				Protocol:      protocol,
				Name:          protoPort.Name,
				ExposePublic:  protoPort.ExposePublic,
			})
		}

		server.AdditionalPorts = additionalPorts
		needsRecreation = true
	}

	// Handle docker overrides update
	if msg.DockerOverrides != nil {
		if err := s.checkDockerPrivilegedOverrides(ctx, msg.DockerOverrides); err != nil {
			return nil, err
		}
		if err := s.checkDockerOverridesConfinement(msg.DockerOverrides, server.DataPath); err != nil {
			return nil, err
		}

		// Check that labels do not start with "carbon-panel."
		for key := range msg.DockerOverrides.Labels {
			if strings.HasPrefix(key, "carbon-panel.") {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("docker label keys cannot start with 'carbon-panel.', namespace reserved for internal management"))
			}
		}

		server.DockerOverrides = msg.DockerOverrides
		needsRecreation = true
	}

	// Handle modpack version update
	if msg.ModpackId != "" {
		serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
		if err != nil {
			s.log.Error("Failed to get server config: %v", err)
			serverConfig = s.store.CreateDefaultServerConfig(server.ID)
		}

		modpack, err := s.store.GetIndexedModpack(ctx, msg.ModpackId)
		if err == nil {
			modpackURL := modpack.WebsiteURL

			switch modpack.Indexer {
			case "fuego", "manual":
				server.ModLoader = storage.ModLoaderAutoCurseForge
				needsRecreation = true

				if msg.ModpackVersionId != "" {
					versionedURL := fmt.Sprintf("%s/files/%s", modpackURL, msg.ModpackVersionId)
					serverConfig.CFPageURL = &versionedURL
				} else {
					serverConfig.CFPageURL = &modpackURL
				}
			case "modrinth":
				server.ModLoader = storage.ModLoaderModrinth
				needsRecreation = true

				configureModrinthModpack(serverConfig, modpack.IndexerID, msg.ModpackVersionId)
			}

			if err := s.store.UpdateServerConfig(ctx, serverConfig); err != nil {
				s.log.Error("Failed to update server config with modpack settings: %v", err)
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save modpack configuration: %w", err))
			}
		}
	}

	// Save server updates first
	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update server"))
	}
	if capacityLockHeld {
		s.capacityMu.Unlock()
		capacityLockHeld = false
	}

	peerIP := req.Peer().Addr
	s.auditServerLifecycle(ctx, peerIP, activity.EventServerUpdate, server.ID)
	if docker.IsPrivilegedDockerOverride(msg.DockerOverrides) {
		s.auditPrivilegedOverrides(ctx, peerIP, server.ID, msg.DockerOverrides)
	}

	// If container needs recreation
	if needsRecreation {
		// Get server config for container creation
		serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
		if err != nil {
			s.log.Error("Failed to get server config: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server configuration"))
		}

		// Recreate container
		dockerCli, err := s.getDockerClientStrict(server.NodeID)
		if err != nil {
			s.log.Error("Docker node %q unavailable for recreation: %v", server.NodeID, err)
			server.Status = storage.StatusError
			_ = s.store.UpdateServer(ctx, server)
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("docker node %q unavailable: %w", server.NodeID, err))
		}
		result, err := dockerCli.RecreateContainer(ctx, server.ContainerID, server, serverConfig)
		if err != nil {
			s.log.Error("Failed to recreate container: %v", err)
			if result != nil && result.NewContainerID != "" {
				// Keep whichever container identity remains authoritative after
				// partial recreation, including the original ID on pre-removal
				// failure and a replacement ID after create/start failure.
				server.ContainerID = result.NewContainerID
				server.Status = storage.StatusError
			} else {
				server.Status = storage.StatusError
				server.ContainerID = ""
			}
			persistErr := s.store.UpdateServer(ctx, server)
			operationErr := fmt.Errorf("server settings were saved, but container recreation failed: %w", err)
			if persistErr != nil {
				s.log.Error("Failed to persist server state after container recreation failed: %v", persistErr)
				operationErr = fmt.Errorf("%w (also failed to persist recovery state: %v)", operationErr, persistErr)
			}
			return nil, connect.NewError(connect.CodeInternal, operationErr)
		} else {
			if result == nil || result.NewContainerID == "" {
				server.ContainerID = ""
				server.Status = storage.StatusError
				persistErr := s.store.UpdateServer(ctx, server)
				operationErr := errors.New("server settings were saved, but container recreation returned no container ID")
				if persistErr != nil {
					operationErr = fmt.Errorf("%w (also failed to persist recovery state: %v)", operationErr, persistErr)
				}
				return nil, connect.NewError(connect.CodeInternal, operationErr)
			}
			server.ContainerID = result.NewContainerID
			server.Status = statusAfterContainerRecreation(server.Status, result.WasRunning)
		}

		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server after container recreation: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("container was recreated but server state could not be saved: %w", err))
		}
	}

	return connect.NewResponse(&v1.UpdateServerResponse{
		Server: dbServerToProto(server),
	}), nil
}

// DeleteServer deletes a server
func (s *ServerService) DeleteServer(ctx context.Context, req *connect.Request[v1.DeleteServerRequest]) (*connect.Response[v1.DeleteServerResponse], error) {
	requestID, reqErr := lifecycleRequestID(req.Msg.GetRequestId())
	if reqErr != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, reqErr)
	}
	transition, err := beginServerLifecycleTransition(req.Msg.Id)
	if err != nil {
		return nil, err
	}
	defer transition.Release()

	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	var dockerCli *docker.Client
	if server.ContainerID != "" {
		dockerCli, err = s.getDockerClientStrict(server.NodeID)
		if err != nil {
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("docker node %q unavailable during deletion: %w", server.NodeID, err))
		}
	}

	// Stop and remove module containers before deleting the server. A module
	// cleanup or lookup failure must keep the parent row so the remaining
	// resources stay addressable and deletion can be retried.
	if s.moduleManager != nil {
		modules, err := s.store.ListServerModules(ctx, server.ID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list server modules before deletion: %w", err))
		}
		for _, mod := range modules {
			if err := s.moduleManager.DeleteModule(ctx, mod.ID); err != nil {
				s.log.Error("Failed to delete module %s: %v", mod.ID, err)
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete module %s: %w", mod.ID, err))
			}
		}
	}

	// Stop and remove the server container before deleting its database row or
	// data directory. A failed removal must not orphan a live container.
	if server.ContainerID != "" {
		if err := s.runJournaledMutation(ctx, server, "delete", requestID, func() error {
			if _, stopErr := dockerCli.StopContainer(ctx, server.ContainerID); stopErr != nil {
				s.log.Warn("Failed to stop container %s during deletion; removal will still be attempted: %v", server.ContainerID, stopErr)
			}
			if rmErr := dockerCli.RemoveContainer(ctx, server.ContainerID); rmErr != nil && !errdefs.IsNotFound(rmErr) {
				return rmErr
			}
			return nil
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to remove server container %s: %w", server.ContainerID, err))
		}
	}

	// Remove proxy route or direct knock listener only after Docker resources are gone.
	// If earlier cleanup failed, the live server remains reachable while the caller retries.
	if s.proxy != nil {
		if server.ProxyHostname != "" {
			if err := s.proxy.RemoveServerRoute(server.ID); err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to remove server proxy route: %w", err))
			}
		} else {
			s.proxy.StopDirectKnock(server.ID)
		}
	}

	// Delete from database
	if err := s.store.DeleteServer(ctx, req.Msg.Id); err != nil {
		s.log.Error("Failed to delete server: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete server"))
	}

	peerIP := req.Peer().Addr
	s.auditServerLifecycle(ctx, peerIP, activity.EventServerDelete, server.ID)

	// Delete data directory
	if err := os.RemoveAll(server.DataPath); err != nil {
		s.log.Error("Failed to delete server data: %v", err)
	}

	return connect.NewResponse(&v1.DeleteServerResponse{}), nil
}

// StartServer starts a server
func (s *ServerService) StartServer(ctx context.Context, req *connect.Request[v1.StartServerRequest]) (out *connect.Response[v1.StartServerResponse], retErr error) {
	defer func() {
		if retErr == nil && out != nil {
			_ = staging.FinishNextRestartBatch(ctx, s.store, req.Msg.GetId())
		}
	}()
	requestID, reqErr := lifecycleRequestID(req.Msg.GetRequestId())
	if reqErr != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, reqErr)
	}
	transition, err := beginServerLifecycleTransition(req.Msg.Id)
	if err != nil {
		return nil, err
	}
	defer transition.Release()

	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	dockerCli, err := s.getDockerClientStrict(server.NodeID)
	if err != nil {
		server.Status = storage.StatusError
		_ = s.store.UpdateServer(ctx, server)
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("docker node %q is unavailable: %w", server.NodeID, err))
	}
	if err := s.checkServerStartCapacity(ctx, server); err != nil {
		return nil, err
	}

	// Apply staged configs on restart/start (STAGED-CONFIG). Content waiting
	// for a clock time or an idle window stays staged.
	undoFiles, fileErr := staging.CommitNextRestartFiles(ctx, s.store, server, s.recreateStagedModule)
	if fileErr != nil {
		s.log.Error("Failed to apply staged content on start for server %s: %v", server.ID, fileErr)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to apply staged content: %w", fileErr))
	}
	contentReady := undoFiles != nil
	if undoFiles == nil {
		undoFiles = func() {}
	}
	appliedStaged, err := scheduler.ApplyOnRestartStagedConfigRecords(ctx, s.store, server.ID)
	if err != nil {
		undoFiles()
		s.log.Error("Failed to apply staged config changes on start for server %s: %v", server.ID, err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to apply staged config changes: %w", err))
	}
	n := len(appliedStaged)
	if n > 0 {
		s.log.Info("Applied %d staged config change(s) on start for server %s", n, server.ID)
	}
	pendingRecreate, err := scheduler.HasPendingRestartStagedConfigRecreate(ctx, s.store, server.ID, server.LastStarted)
	if err != nil {
		undoFiles()
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check staged config recovery: %w", err))
	}
	needsStagedRecreate := n > 0 || pendingRecreate || contentReady
	stagedContainerStarted := false

	// If server was in deep sleep, release direct knock listener before starting
	if server.Status == storage.StatusDeepSleep && s.proxy != nil {
		s.proxy.StopDirectKnock(server.ID)
	}

	// Resume a paused container directly only when no staged changes require
	// replacement. For staged replacement, keep it frozen until config loading
	// succeeds and RecreateContainer owns the stop/remove transition.
	if server.Status == storage.StatusPaused && server.ContainerID != "" && !needsStagedRecreate {
		if err := s.runJournaledMutation(ctx, server, "start_unpause", requestID, func() error {
			return dockerCli.UnpauseContainer(ctx, server.ContainerID)
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to resume server: %w", err))
		}
		server.Status = storage.StatusRunning
		if err := s.store.UpdateServer(ctx, server); err != nil {
			resumeErr := fmt.Errorf("server resumed but status could not be saved: %w", err)
			rollbackErr := s.rollbackPausedResume(ctx, dockerCli, server)
			return nil, connect.NewError(connect.CodeInternal, errors.Join(resumeErr, rollbackErr))
		}
		if s.proxy != nil && server.ProxyHostname != "" {
			_ = s.proxy.UpdateServerRoute(server)
		}
		// Unpause lands on running inside this RPC, so it announces here.
		// Create and StartServer announce from the reconciler when it records running.
		if s.bus != nil {
			s.bus.Emit(ctx, events.Event{
				Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START,
				ServerID: server.ID,
			})
		}
		return connect.NewResponse(&v1.StartServerResponse{Status: "running"}), nil
	}

	if needsStagedRecreate && server.ContainerID != "" {
		serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server configuration: %w", err))
		}
		s.prepareServerConfig(ctx, server, serverConfig)
		result, recreateErr := dockerCli.RecreateContainer(ctx, server.ContainerID, server, serverConfig)
		if recreateErr != nil {
			if result != nil && result.NewContainerID != "" {
				server.ContainerID = result.NewContainerID
			} else {
				server.ContainerID = ""
			}
			server.Status = storage.StatusError
			_ = s.store.UpdateServer(ctx, server)
			undoFiles()
			for _, rec := range appliedStaged {
				if _, rbErr := s.store.RollbackStagedConfigChange(ctx, rec.ID); rbErr != nil {
					s.log.Error("Failed to auto-rollback staged config %s after start recreate error: %v", rec.ID, rbErr)
				} else {
					s.log.Warn("Auto-rolled back staged config %s due to start recreate failure", rec.ID)
				}
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to recreate server with staged configuration: %w", recreateErr))
		}
		if result == nil || result.NewContainerID == "" {
			undoFiles()
			for _, rec := range appliedStaged {
				_, _ = s.store.RollbackStagedConfigChange(ctx, rec.ID)
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("container recreation returned no container ID"))
		}
		server.ContainerID = result.NewContainerID
		if err := s.store.UpdateServer(ctx, server); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("container recreated but its ID could not be saved: %w", err))
		}
		if !result.WasRunning {
			if err := dockerCli.StartContainer(ctx, result.NewContainerID); err != nil {
				server.Status = storage.StatusError
				_ = s.store.UpdateServer(ctx, server)
				undoFiles()
				for _, rec := range appliedStaged {
					if _, rbErr := s.store.RollbackStagedConfigChange(ctx, rec.ID); rbErr != nil {
						s.log.Error("Failed to auto-rollback staged config %s after start error: %v", rec.ID, rbErr)
					} else {
						s.log.Warn("Auto-rolled back staged config %s due to start failure", rec.ID)
					}
				}
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start recreated server: %w", err))
			}
		}
		stagedContainerStarted = true
	}

	// If container doesn't exist (e.g. ContainerID lost/never persisted), try to recover an
	// existing-but-untracked container before creating a duplicate (MINE-103).
	if server.ContainerID == "" {
		if resolved, resolveErr := dockerCli.ResolveContainer(ctx, server.ID); resolveErr == nil && resolved != nil {
			s.log.Info("Recovered untracked container %s for server %s via label/name resolution", resolved.ID, server.ID)
			server.ContainerID = resolved.ID
			if err := s.store.UpdateServer(ctx, server); err != nil {
				s.log.Error("Failed to update server with resolved container ID: %v", err)
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update server"))
			}
		}
	}

	// Still no container - create it
	if server.ContainerID == "" {
		serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
		if err != nil {
			s.log.Error("Failed to get server config: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server configuration"))
		}

		s.prepareServerConfig(ctx, server, serverConfig)
		containerID, err := dockerCli.CreateContainer(ctx, server, serverConfig)
		if err != nil {
			s.log.Error("Failed to create container: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create server container"))
		}

		server.ContainerID = containerID
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server with container ID: %v", err)
			_ = dockerCli.RemoveContainer(ctx, containerID)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update server"))
		}
	}

	if !stagedContainerStarted {

		// Start container
		if err := s.runJournaledMutation(ctx, server, "start", requestID, func() error {
			return dockerCli.StartContainer(ctx, server.ContainerID)
		}); err != nil {
			s.log.Error("Failed to start container, attempting to recreate: %v", err)

			// Get server config for container creation
			serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
			if err != nil {
				s.log.Error("Failed to get server config: %v", err)
				server.Status = storage.StatusError
				_ = s.store.UpdateServer(ctx, server)
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server configuration"))
			}

			// Recreate container
			result, err := dockerCli.RecreateContainer(ctx, server.ContainerID, server, serverConfig)
			if err != nil {
				s.log.Error("Failed to recreate container: %v", err)
				if result != nil && result.NewContainerID != "" {
					// Container was created but failed to start
					server.ContainerID = result.NewContainerID
				} else {
					// Complete failure
					server.ContainerID = ""
				}
				server.Status = storage.StatusError
				_ = s.store.UpdateServer(ctx, server)
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start server: %w", err))
			}

			server.ContainerID = result.NewContainerID

			// RecreateContainer only starts the container if result.WasRunning was true.
			// Since StartServer was requested, start the newly recreated container.
			if !result.WasRunning {
				if startErr := dockerCli.StartContainer(ctx, result.NewContainerID); startErr != nil {
					s.log.Error("Failed to start recreated container: %v", startErr)
					server.Status = storage.StatusError
					_ = s.store.UpdateServer(ctx, server)
					return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start server: %w", startErr))
				}
			}

			// Update server with new container ID
			if err := s.store.UpdateServer(ctx, server); err != nil {
				s.log.Error("Failed to update server after container recreation: %v", err)
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update server after container recreation"))
			}
		}

	}

	// Update server status
	now := time.Now()
	server.Status = storage.StatusStarting
	server.LastStarted = &now

	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server status: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("server started but status could not be saved: %w", err))
	}

	// Update proxy route if enabled
	if s.proxy != nil && server.ProxyHostname != "" {
		if err := s.proxy.UpdateServerRoute(server); err != nil {
			s.log.Error("Failed to update proxy route: %v", err)
		}
	}

	// Clear ephemeral configuration fields
	if err := s.store.ClearEphemeralConfigFields(ctx, server.ID); err != nil {
		s.log.Error("Failed to clear ephemeral config fields: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("server started but temporary config could not be cleared: %w", err))
	}

	// SERVER_START is emitted by the reconciler when status becomes running,
	// so create, unpause, and this RPC share one announcement.

	// Audit trail (MINE-141): never break the op on audit failure.
	s.auditPower(ctx, req.Peer().Addr, activity.EventServerStart, server.ID)

	return connect.NewResponse(&v1.StartServerResponse{
		Status: "starting",
	}), nil
}

// rollbackPausedResume keeps the persisted PAUSED intent aligned with Docker
// when a resume succeeds but the database cannot record RUNNING. The rollback
// uses a detached bounded context because the RPC may already be cancelled.
func (s *ServerService) rollbackPausedResume(ctx context.Context, dockerCli *docker.Client, server *storage.Server) error {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	rollbackErr := dockerCli.PauseContainer(rollbackCtx, server.ContainerID)
	if rollbackErr == nil {
		status, statusErr := dockerCli.GetContainerStatus(rollbackCtx, server.ContainerID)
		if statusErr == nil && status == storage.StatusPaused {
			server.Status = storage.StatusPaused
			return nil
		}
		if statusErr != nil {
			rollbackErr = fmt.Errorf("failed to verify refrozen container: %w", statusErr)
		} else {
			rollbackErr = fmt.Errorf("container reported %s after refreeze", status)
		}
	}

	server.Status = storage.StatusError
	persistErr := s.store.UpdateServer(rollbackCtx, server)
	if persistErr != nil && s.log != nil {
		s.log.Error("Failed to persist error state after paused-server resume rollback failed: %v", persistErr)
	}
	return errors.Join(fmt.Errorf("failed to restore paused container state: %w", rollbackErr), persistErr)
}

// StopServer stops a server
func (s *ServerService) StopServer(ctx context.Context, req *connect.Request[v1.StopServerRequest]) (*connect.Response[v1.StopServerResponse], error) {
	requestID, reqErr := lifecycleRequestID(req.Msg.GetRequestId())
	if reqErr != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, reqErr)
	}
	transition, err := beginServerLifecycleTransition(req.Msg.Id)
	if err != nil {
		return nil, err
	}
	defer transition.Release()

	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	if server.ContainerID == "" {
		// If there's no container, server is already stopped
		server.Status = storage.StatusStopped
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server status: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("server stopped but status could not be saved: %w", err))
		}
		return connect.NewResponse(&v1.StopServerResponse{
			Status: "stopped",
		}), nil
	}

	// Stop container
	dockerCli, err := s.getDockerClientStrict(server.NodeID)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("docker node %q is unavailable: %w", server.NodeID, err))
	}

	// If server is paused, unpause it first so Docker daemon doesn't reject stop (Docker requires unpaused before stop)
	if server.Status == storage.StatusPaused && server.ContainerID != "" {
		if err := dockerCli.UnpauseContainer(ctx, server.ContainerID); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to resume server before stopping: %w", err))
		}
	}

	window := docker.DefaultStopWindow
	if cfg, cfgErr := s.store.GetServerConfig(ctx, server.ID); cfgErr == nil {
		window = docker.StopWindow(cfg)
	}

	// Persist stopping before the save window so a panel restart resumes the
	// stop instead of starting the JVM again.
	server.Status = storage.StatusStopping
	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to persist stopping status: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to record stop: %w", err))
	}

	// Drop the player route before the save wait so the proxy is not held open.
	if s.proxy != nil {
		if server.ProxyHostname != "" {
			if err := s.proxy.RemoveServerRoute(server.ID); err != nil {
				s.log.Error("Failed to remove proxy route: %v", err)
			}
		} else {
			s.proxy.StopDirectKnock(server.ID)
		}
	}

	stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), window+15*time.Second)
	defer stopCancel()
	var (
		found   bool
		stopErr error
	)
	stopErr = s.runJournaledMutation(ctx, server, "stop", requestID, func() error {
		var err error
		found, err = dockerCli.StopContainerWithin(stopCtx, server.ContainerID, window)
		return err
	})
	if stopErr != nil {
		s.log.Error("Failed to stop container: %v", stopErr)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to stop server"))
	}

	// If container wasn't found, clean up stale reference
	if !found {
		s.log.Warn("Container %s not found, cleaning up stale reference", server.ContainerID)
		server.ContainerID = ""
	}
	server.Status = storage.StatusStopped

	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server status: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("server stopped but status could not be saved: %w", err))
	}

	// Emit the server-stop event
	if s.bus != nil {
		s.bus.Emit(ctx, events.Event{
			Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_STOP,
			ServerID: server.ID,
		})
	}

	// Audit trail (MINE-141): never break the op on audit failure.
	s.auditPower(ctx, req.Peer().Addr, activity.EventServerStop, server.ID)

	return connect.NewResponse(&v1.StopServerResponse{
		Status: "stopped",
	}), nil
}

// RestartServer restarts a server
func (s *ServerService) RestartServer(ctx context.Context, req *connect.Request[v1.RestartServerRequest]) (out *connect.Response[v1.RestartServerResponse], retErr error) {
	defer func() {
		if retErr == nil && out != nil {
			_ = staging.FinishNextRestartBatch(ctx, s.store, req.Msg.GetId())
		}
	}()
	requestID, reqErr := lifecycleRequestID(req.Msg.GetRequestId())
	if reqErr != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, reqErr)
	}
	transition, err := beginServerLifecycleTransition(req.Msg.Id)
	if err != nil {
		return nil, err
	}
	defer transition.Release()

	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	dockerCli, err := s.getDockerClientStrict(server.NodeID)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("docker node %q is unavailable: %w", server.NodeID, err))
	}
	if err := s.checkServerStartCapacity(ctx, server); err != nil {
		return nil, err
	}

	// Apply any staged config changes marked for restart. A future time or idle
	// window is not published by this restart.
	undoFiles, fileErr := staging.CommitNextRestartFiles(ctx, s.store, server, s.recreateStagedModule)
	if fileErr != nil {
		s.log.Error("Failed to apply staged content on restart for server %s: %v", server.ID, fileErr)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to apply staged content: %w", fileErr))
	}
	if undoFiles == nil {
		undoFiles = func() {}
	}
	appliedStaged, err := scheduler.ApplyOnRestartStagedConfigRecords(ctx, s.store, server.ID)
	if err != nil {
		undoFiles()
		s.log.Error("Failed to apply staged config changes on restart for server %s: %v", server.ID, err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to apply staged config changes: %w", err))
	}
	stagedApplied := len(appliedStaged)
	// If server is in deep sleep, release direct knock listener before starting
	if server.Status == storage.StatusDeepSleep && s.proxy != nil {
		s.proxy.StopDirectKnock(server.ID)
	}
	// If server is paused, unpause it first so Docker can restart it
	if server.Status == storage.StatusPaused && server.ContainerID != "" {
		if err := dockerCli.UnpauseContainer(ctx, server.ContainerID); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to resume server before restarting: %w", err))
		}
	}

	pendingRecreate, err := scheduler.HasPendingRestartStagedConfigRecreate(ctx, s.store, server.ID, server.LastStarted)
	if err != nil {
		undoFiles()
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check staged config recovery: %w", err))
	}

	// If staged config changes were applied and container exists, recreate it with new config
	if (stagedApplied > 0 || pendingRecreate) && server.ContainerID != "" {
		serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server configuration: %w", err))
		}
		s.prepareServerConfig(ctx, server, serverConfig)
		result, err := dockerCli.RecreateContainer(ctx, server.ContainerID, server, serverConfig)
		if err != nil {
			// RecreateContainer can remove the old container and then fail while
			// starting the replacement. Preserve its partial result so a retry can
			// address the live replacement instead of a stale container ID.
			if result != nil && result.NewContainerID != "" {
				server.ContainerID = result.NewContainerID
			} else {
				server.ContainerID = ""
			}
			server.Status = storage.StatusError
			if persistErr := s.store.UpdateServer(ctx, server); persistErr != nil {
				s.log.Error("Failed to persist server state after staged recreation error for server %s: %v", server.ID, persistErr)
			}
			undoFiles()
			for _, rec := range appliedStaged {
				if _, rbErr := s.store.RollbackStagedConfigChange(ctx, rec.ID); rbErr != nil {
					s.log.Error("Failed to auto-rollback staged config %s after restart recreate error: %v", rec.ID, rbErr)
				} else {
					s.log.Warn("Auto-rolled back staged config %s due to restart recreate failure", rec.ID)
				}
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to recreate server with staged configuration: %w", err))
		}
		if result == nil || result.NewContainerID == "" {
			undoFiles()
			for _, rec := range appliedStaged {
				_, _ = s.store.RollbackStagedConfigChange(ctx, rec.ID)
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("container recreation returned no container ID"))
		}
		server.ContainerID = result.NewContainerID
		if err := s.store.UpdateServer(ctx, server); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("container recreated but its ID could not be saved: %w", err))
		}
		if !result.WasRunning {
			if err := s.runJournaledMutation(ctx, server, "restart_start", requestID, func() error {
				return dockerCli.StartContainer(ctx, result.NewContainerID)
			}); err != nil {
				server.Status = storage.StatusError
				_ = s.store.UpdateServer(ctx, server)
				undoFiles()
				for _, rec := range appliedStaged {
					if _, rbErr := s.store.RollbackStagedConfigChange(ctx, rec.ID); rbErr != nil {
						s.log.Error("Failed to auto-rollback staged config %s after restart start error: %v", rec.ID, rbErr)
					} else {
						s.log.Warn("Auto-rolled back staged config %s due to restart start failure", rec.ID)
					}
				}
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start recreated server: %w", err))
			}
		}
		now := time.Now()
		server.Status = storage.StatusStarting
		server.LastStarted = &now
		if err := s.store.UpdateServer(ctx, server); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("server recreated but status could not be saved: %w", err))
		}
		if err := s.store.ClearEphemeralConfigFields(ctx, server.ID); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("server restarted but temporary config could not be cleared: %w", err))
		}
		if s.bus != nil {
			s.bus.Emit(ctx, events.Event{
				Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_RESTART,
				ServerID: server.ID,
			})
		}
		s.auditPower(ctx, req.Peer().Addr, activity.EventServerRestart, server.ID)
		return connect.NewResponse(&v1.RestartServerResponse{Status: "starting"}), nil
	}
	// If container doesn't exist, create it and start it
	if server.ContainerID == "" {
		// Get server config for container creation
		serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
		if err != nil {
			s.log.Error("Failed to get server config: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server configuration"))
		}

		// Create container
		s.prepareServerConfig(ctx, server, serverConfig)
		var containerID string
		if err := s.runJournaledMutation(ctx, server, "restart_create", requestID, func() error {
			var createErr error
			containerID, createErr = dockerCli.CreateContainer(ctx, server, serverConfig)
			return createErr
		}); err != nil {
			s.log.Error("Failed to create container: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create server container"))
		}

		server.ContainerID = containerID
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server with container ID: %v", err)
			_ = dockerCli.RemoveContainer(ctx, containerID)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update server"))
		}

		// Now start the container. These kinds stay distinct from StartServer
		// so a reused request id cannot replay a completed start as this restart.
		if err := s.runJournaledMutation(ctx, server, "restart_start", requestID, func() error {
			return dockerCli.StartContainer(ctx, server.ContainerID)
		}); err != nil {
			s.log.Error("Failed to start container: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start server"))
		}

		// Update server status
		now := time.Now()
		server.Status = storage.StatusStarting
		server.LastStarted = &now

		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server status: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("server restarted but status could not be saved: %w", err))
		}

		// Clear ephemeral configuration fields
		if err := s.store.ClearEphemeralConfigFields(ctx, server.ID); err != nil {
			s.log.Error("Failed to clear ephemeral config fields: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("server restarted but temporary config could not be cleared: %w", err))
		}

		// Emit the server-restart event
		if s.bus != nil {
			s.bus.Emit(ctx, events.Event{
				Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_RESTART,
				ServerID: server.ID,
			})
		}

		// Update proxy route if enabled
		if s.proxy != nil && server.ProxyHostname != "" {
			if err := s.proxy.UpdateServerRoute(server); err != nil {
				s.log.Error("Failed to update proxy route on restart: %v", err)
			}
		}

		return connect.NewResponse(&v1.RestartServerResponse{
			Status: "starting",
		}), nil
	}

	// Restart container
	if err := s.runJournaledMutation(ctx, server, "restart", requestID, func() error {
		return dockerCli.RestartContainer(ctx, server.ContainerID, 2*time.Second)
	}); err != nil {
		s.log.Warn("Failed to restart container directly (%v), attempting recreation", err)
		serverConfig, cfgErr := s.store.GetServerConfig(ctx, server.ID)
		if cfgErr == nil {
			s.prepareServerConfig(ctx, server, serverConfig)
			recResult, recErr := dockerCli.RecreateContainer(ctx, server.ContainerID, server, serverConfig)
			if recErr == nil && recResult != nil && recResult.NewContainerID != "" {
				server.ContainerID = recResult.NewContainerID
				now := time.Now()
				server.Status = storage.StatusStarting
				server.LastStarted = &now
				_ = s.store.UpdateServer(ctx, server)
				_ = s.store.ClearEphemeralConfigFields(ctx, server.ID)
				if s.bus != nil {
					s.bus.Emit(ctx, events.Event{
						Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_RESTART,
						ServerID: server.ID,
					})
				}
				if s.proxy != nil && server.ProxyHostname != "" {
					if err := s.proxy.UpdateServerRoute(server); err != nil {
						s.log.Error("Failed to update proxy route on restart: %v", err)
					}
				}
				return connect.NewResponse(&v1.RestartServerResponse{
					Status: "starting",
				}), nil
			}
		}
		s.log.Error("Failed to restart container: %v", err)
		server.Status = storage.StatusError
		_ = s.store.UpdateServer(ctx, server)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to restart server"))
	}

	// Update server status
	now := time.Now()
	server.Status = storage.StatusStarting
	server.LastStarted = &now
	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server status: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("server restarted but status could not be saved: %w", err))
	}

	// Clear ephemeral configuration fields
	if err := s.store.ClearEphemeralConfigFields(ctx, server.ID); err != nil {
		s.log.Error("Failed to clear ephemeral config fields: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("server restarted but temporary config could not be cleared: %w", err))
	}

	// Emit the server-restart event
	if s.bus != nil {
		s.bus.Emit(ctx, events.Event{
			Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_RESTART,
			ServerID: server.ID,
		})
	}

	// Update proxy route if enabled
	if s.proxy != nil && server.ProxyHostname != "" {
		if err := s.proxy.UpdateServerRoute(server); err != nil {
			s.log.Error("Failed to update proxy route on restart: %v", err)
		}
	}

	s.auditPower(ctx, req.Peer().Addr, activity.EventServerRestart, server.ID)

	return connect.NewResponse(&v1.RestartServerResponse{
		Status: "restarting",
	}), nil
}

// Destroys and recreates a server container from scratch - brute force reset
func (s *ServerService) RecreateServer(ctx context.Context, req *connect.Request[v1.RecreateServerRequest]) (*connect.Response[v1.RecreateServerResponse], error) {
	transition, err := beginServerLifecycleTransition(req.Msg.Id)
	if err != nil {
		return nil, err
	}
	defer transition.Release()

	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Get server config for container creation
	serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
	if err != nil {
		s.log.Error("Failed to get server config: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server configuration"))
	}

	// Recreate container
	dockerCli, err := s.getDockerClientStrict(server.NodeID)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("docker node %q is unavailable: %w", server.NodeID, err))
	}
	if err := s.checkServerStartCapacity(ctx, server); err != nil {
		return nil, err
	}
	result, err := dockerCli.RecreateContainer(ctx, server.ContainerID, server, serverConfig)
	if err != nil {
		s.log.Error("Failed to recreate container: %v", err)
		server.Status = storage.StatusError
		if result != nil && result.NewContainerID != "" {
			server.ContainerID = result.NewContainerID
		} else {
			server.ContainerID = ""
		}
		if updateErr := s.store.UpdateServer(ctx, server); updateErr != nil {
			s.log.Error("Failed to update server status: %v", updateErr)
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to recreate server container: %w", err))
	}

	server.ContainerID = result.NewContainerID

	// If the container was not already running (and thus not started by RecreateContainer),
	// start it now since RecreateServer intends to start the freshly recreated container.
	if !result.WasRunning {
		if startErr := dockerCli.StartContainer(ctx, result.NewContainerID); startErr != nil {
			s.log.Error("Failed to start recreated container: %v", startErr)
			server.Status = storage.StatusError
			_ = s.store.UpdateServer(ctx, server)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start recreated container: %w", startErr))
		}
	}

	// Update server status
	now := time.Now()
	server.Status = storage.StatusStarting
	server.LastStarted = &now

	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update server"))
	}

	// Update proxy route if enabled
	if s.proxy != nil && server.ProxyHostname != "" {
		if err := s.proxy.UpdateServerRoute(server); err != nil {
			s.log.Error("Failed to update proxy route: %v", err)
		}
	}

	// Clear ephemeral configuration fields
	if err := s.store.ClearEphemeralConfigFields(ctx, server.ID); err != nil {
		s.log.Error("Failed to clear ephemeral config fields: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("server recreated but temporary config could not be cleared: %w", err))
	}

	s.log.Info("Server %s recreated successfully with new container %s", server.Name, result.NewContainerID)

	return connect.NewResponse(&v1.RecreateServerResponse{
		Status: "recreated",
	}), nil
}

// SendCommand sends a command to a server
func (s *ServerService) SendCommand(ctx context.Context, req *connect.Request[v1.SendCommandRequest]) (*connect.Response[v1.SendCommandResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)

	silent := false
	if req.Msg.Silent != nil {
		silent = *req.Msg.Silent
	}

	if err != nil {
		s.log.Error("Failed to get server: %v", err)
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Check if server is running
	if server.ContainerID == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("server container not found"))
	}

	dockerCli, err := s.getDockerClientStrict(server.NodeID)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("docker node %q is unavailable: %w", server.NodeID, err))
	}
	status, err := dockerCli.GetContainerStatus(ctx, server.ContainerID)
	if err != nil || status != storage.StatusRunning {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("server is not running"))
	}

	if req.Msg.Command == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("command is required"))
	}

	// Add command to log stream if available
	commandTime := time.Now()
	if !silent && s.logStreamer != nil {
		s.logStreamer.AddCommandEntry(server.ContainerID, req.Msg.Command, commandTime)
	}

	// Send command
	output, err := s.sender.SendCommand(ctx, server.ID, req.Msg.Command)
	success := err == nil

	// Add command output to log stream if available
	if !silent && s.logStreamer != nil && (output != "" || !success) {
		s.logStreamer.AddCommandOutput(server.ContainerID, output, success, commandTime)
	}

	if err != nil {
		s.log.Error("Failed to execute command: %v", err)
		return connect.NewResponse(&v1.SendCommandResponse{
			Success: false,
			Error:   err.Error(),
		}), nil
	}

	return connect.NewResponse(&v1.SendCommandResponse{
		Success: true,
		Output:  output,
	}), nil
}

// ListServerPlayers returns the live online roster from the metrics
// collector (RCON `list` / SLP sample), empty when offline (MINE-140).
func (s *ServerService) ListServerPlayers(ctx context.Context, req *connect.Request[v1.ListServerPlayersRequest]) (*connect.Response[v1.ListServerPlayersResponse], error) {
	if _, err := s.store.GetServer(ctx, req.Msg.ServerId); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}
	players := []string{}
	online := 0
	if s.metricsCollector != nil {
		if m := s.metricsCollector.GetMetrics(req.Msg.ServerId); m != nil {
			players = append(players, m.PlayerSample...)
			online = m.PlayersOnline
		}
	}
	return connect.NewResponse(&v1.ListServerPlayersResponse{
		Players: players, OnlineCount: int32(online),
	}), nil
}

// serverLatestLog reads logs/latest.log from the owning node's mapped root.
// A remote node without reachable storage is refused, so a controller-local
// copy is not uploaded in its place.
func serverLatestLog(ctx context.Context, store *storage.Store, server *storage.Server) ([]byte, error) {
	root, err := dataRootFor(ctx, store, server, false)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(root, "logs", "latest.log"))
}

// Reads the server's latest.log and uploads it to mclo.gs
func (s *ServerService) UploadToMCLogs(ctx context.Context, req *connect.Request[v1.UploadToMCLogsRequest]) (*connect.Response[v1.UploadToMCLogsResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	content, err := serverLatestLog(ctx, s.store, server)
	if nodestorage.IsUnavailable(err) {
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}
	if err != nil {
		s.log.Error("Failed to read server log file: %v", err)
		if os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("log file not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to read server log"))
	}

	if len(content) == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("log file is empty"))
	}

	// Truncate to 25000 lines if needed
	lines := bytes.Split(content, []byte("\n"))
	if len(lines) > 25000 {
		lines = lines[len(lines)-25000:]
		content = bytes.Join(lines, []byte("\n"))
	}

	// Build mclo.gs request
	payload, _ := json.Marshal(map[string]string{
		"content": string(content),
		"source":  fmt.Sprintf("carbon-panel-%s", server.Name),
	})

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.mclo.gs/1/log", bytes.NewReader(payload))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create request"))
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		s.log.Error("Failed to upload to mclo.gs: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to upload to mclo.gs"))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to read mclo.gs response"))
	}

	var result struct {
		Success bool   `json:"success"`
		URL     string `json:"url"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to parse mclo.gs response"))
	}

	if !result.Success {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("mclo.gs error: %s", result.Error))
	}

	return connect.NewResponse(&v1.UploadToMCLogsResponse{
		Url: result.URL,
	}), nil
}

// GetServerLogs gets server logs
func (s *ServerService) GetServerLogs(ctx context.Context, req *connect.Request[v1.GetServerLogsRequest]) (*connect.Response[v1.GetServerLogsResponse], error) {
	// Parse tail parameter
	tail := int(req.Msg.Tail)
	if tail == 0 {
		tail = 100
	}

	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// If container not created yet, return empty logs
	if server.ContainerID == "" {
		return connect.NewResponse(&v1.GetServerLogsResponse{
			Logs:  []*v1.LogEntry{},
			Total: 0,
		}), nil
	}

	// Get structured log entries from the log streamer if available
	var protoLogs []*v1.LogEntry
	if s.logStreamer != nil {
		protoLogs = s.logStreamer.GetLogs(server.ContainerID, tail)
	}

	return connect.NewResponse(&v1.GetServerLogsResponse{
		Logs:  protoLogs,
		Total: int32(len(protoLogs)),
	}), nil
}

// ClearServerLogs clears server logs
func (s *ServerService) ClearServerLogs(ctx context.Context, req *connect.Request[v1.ClearServerLogsRequest]) (*connect.Response[v1.ClearServerLogsResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	if server.ContainerID == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("server container not created"))
	}

	// Clear structured log entries if log streamer is available
	if s.logStreamer != nil {
		s.logStreamer.ClearLogs(server.ContainerID)
	}

	return connect.NewResponse(&v1.ClearServerLogsResponse{}), nil
}

// isHostPortBound checks if a port is already bound by an OS process in the local network namespace.
//
// On some platforms (notably Windows) a socket bound to a specific address (for
// example 127.0.0.1) does not conflict with a bind to the wildcard address, so
// a single bind attempt is not reliable. Probe a set of common local addresses
// and treat the port as bound if any of them cannot be acquired.
func isHostPortBound(port int) bool {
	return isHostPortBoundWith(port, net.Listen)
}

func isHostPortBoundWith(port int, listen func(string, string) (net.Listener, error)) bool {
	candidates := []string{"127.0.0.1", "0.0.0.0", "::1", "::"}

	// Also probe every address assigned to a local interface so that ports bound
	// only to a specific (non-loopback) IP are detected.
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			addrs, _ := iface.Addrs()
			for _, a := range addrs {
				var ip net.IP
				switch v := a.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if ip != nil {
					candidates = append(candidates, ip.String())
				}
			}
		}
	}

	seen := make(map[string]bool)
	for _, addr := range candidates {
		if seen[addr] {
			continue
		}
		seen[addr] = true

		ln, err := listen("tcp", net.JoinHostPort(addr, strconv.Itoa(port)))
		if err == nil {
			_ = ln.Close()
			continue
		}
		// Only treat address-in-use failures as "bound"; ignore unrelated
		// errors (e.g. disabled IPv6, permission) so they don't produce
		// false positives.
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "in use") || strings.Contains(msg, "only one usage") {
			return true
		}
	}
	return false
}

// GetNextAvailablePort gets the next available port
func (s *ServerService) GetNextAvailablePort(ctx context.Context, req *connect.Request[v1.GetNextAvailablePortRequest]) (*connect.Response[v1.GetNextAvailablePortResponse], error) {
	// Get all servers
	servers, err := s.store.ListServers(ctx)
	if err != nil {
		s.log.Error("Failed to list servers: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get available port"))
	}

	// Build a map of used ports (only for non-proxied servers)
	usedPortsMap := make(map[int32]bool)
	for _, server := range servers {
		// Only count ports for servers that don't use proxy
		if server.ProxyHostname == "" && server.Port > 0 {
			usedPortsMap[int32(server.Port)] = true
		}
	}

	// Mark proxy listening ports as used (only if proxy is enabled)
	if s.config.Proxy.Enabled {
		for _, port := range s.config.Proxy.ListenPorts {
			usedPortsMap[int32(port)] = true
		}
	}

	// Mark ports allocated on the Docker daemon as used
	dockerCli, err := s.getDockerClientStrict("")
	if err != nil || dockerCli == nil {
		if err == nil {
			err = fmt.Errorf("no default Docker client available")
		}
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("cannot check default Docker host ports for availability: %w", err))
	}
	allocated, err := dockerCli.GetAllocatedHostPorts(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("cannot check default Docker host ports for availability: %w", err))
	}
	for port := range allocated {
		usedPortsMap[int32(port)] = true
	}

	// Find the next available port starting from 25565
	var nextPort int32 = 25565
	for usedPortsMap[nextPort] || s.isHostPortBound(int(nextPort)) {
		usedPortsMap[nextPort] = true
		nextPort++
		// Safety check to avoid infinite loop
		if nextPort > 65535 {
			return nil, connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("no available ports"))
		}
	}

	// Convert map to proto UsedPort array
	usedPorts := make([]*v1.UsedPort, 0, len(usedPortsMap))
	for port, inUse := range usedPortsMap {
		usedPorts = append(usedPorts, &v1.UsedPort{
			Port:  port,
			InUse: inUse,
		})
	}

	return connect.NewResponse(&v1.GetNextAvailablePortResponse{
		Port:      nextPort,
		UsedPorts: usedPorts,
	}), nil
}

// prepareServerConfig fills in global settings and defaults for server config before container launch
func (s *ServerService) prepareServerConfig(ctx context.Context, server *storage.Server, serverConfig *storage.ServerConfig) {
	if serverConfig == nil {
		return
	}
	// Inherit global CF API Key if not set on server
	if serverConfig.CFAPIKey == nil || *serverConfig.CFAPIKey == "" {
		if globalSettings, _, err := s.store.GetGlobalSettings(ctx); err == nil && globalSettings != nil && globalSettings.CFAPIKey != nil && *globalSettings.CFAPIKey != "" {
			serverConfig.CFAPIKey = globalSettings.CFAPIKey
		}
	}
	// Inherit global Modrinth Token if not set on server
	if serverConfig.ModrinthToken == nil || *serverConfig.ModrinthToken == "" {
		if globalSettings, _, err := s.store.GetGlobalSettings(ctx); err == nil && globalSettings != nil && globalSettings.ModrinthToken != nil && *globalSettings.ModrinthToken != "" {
			serverConfig.ModrinthToken = globalSettings.ModrinthToken
		}
	}
	// Auto-assume CF slug from server name if AUTO_CURSEFORGE and no slug or page URL is set
	if server != nil && server.ModLoader == storage.ModLoaderAutoCurseForge {
		if (serverConfig.CFSlug == nil || *serverConfig.CFSlug == "") && (serverConfig.CFPageURL == nil || *serverConfig.CFPageURL == "") && server.Name != "" {
			slug := slugifyName(server.Name)
			if slug != "" {
				serverConfig.CFSlug = &slug
			}
		}
	}
}

func slugifyName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	re := regexp.MustCompile(`[^a-z0-9]+`)
	slug := re.ReplaceAllString(name, "-")
	return strings.Trim(slug, "-")
}

// MigrateServer moves a server instance from its current node to a target node
func (s *ServerService) MigrateServer(ctx context.Context, req *connect.Request[v1.MigrateServerRequest]) (*connect.Response[v1.MigrateServerResponse], error) {
	requestID, reqErr := lifecycleRequestID(req.Msg.GetRequestId())
	if reqErr != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, reqErr)
	}
	startTime := time.Now()
	serverID := req.Msg.Id
	targetNodeID := req.Msg.TargetNodeId
	force := req.Msg.GetForce()

	if serverID == "" || targetNodeID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("server ID and target node ID are required"))
	}

	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server %s not found: %w", serverID, err))
	}

	sourceNodeID := server.NodeID

	if sourceNodeID == targetNodeID {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("server is already hosted on node '%s'", targetNodeID))
	}

	// Retrieve target node & validate
	targetNode, err := s.store.GetNode(ctx, targetNodeID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("target node '%s' not found: %w", targetNodeID, err))
	}

	if !targetNode.Enabled {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("target node '%s' is disabled", targetNode.Name))
	}

	// Verify target node connectivity
	if s.pool != nil {
		online, pingErr := s.pool.PingNode(ctx, targetNodeID)
		if (!online || pingErr != nil) && !force {
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("target node daemon is unreachable: %v", pingErr))
		}
	}

	// Check capacity limits if not forced
	if !force && (targetNode.MaxMemoryMB > 0 || targetNode.MaxServers > 0) {
		allocMem, serverCount, _, err := s.store.GetNodeStats(ctx, targetNode.ID)
		if err == nil {
			if targetNode.MaxServers > 0 && serverCount >= targetNode.MaxServers {
				return nil, connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("target node has reached its maximum server capacity (%d/%d)", serverCount, targetNode.MaxServers))
			}
			if targetNode.MaxMemoryMB > 0 && (allocMem+int64(server.Memory)) > targetNode.MaxMemoryMB {
				return nil, connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("target node lacks memory headroom (allocated %dMB + required %dMB > limit %dMB)", allocMem, server.Memory, targetNode.MaxMemoryMB))
			}
		}
	}

	var sourceDockerCli, targetDockerCli *docker.Client
	if s.pool != nil || s.docker != nil {
		var err error
		sourceDockerCli, err = s.getDockerClientStrict(sourceNodeID)
		if err != nil {
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("source docker node %q is unavailable: %w", sourceNodeID, err))
		}
		targetDockerCli, err = s.getDockerClientStrict(targetNodeID)
		if err != nil {
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("target docker node %q is unavailable: %w", targetNodeID, err))
		}
	}

	// Check if server is currently running
	var isRunning bool
	if server.ContainerID != "" && sourceDockerCli != nil {
		status, err := sourceDockerCli.GetContainerStatus(ctx, server.ContainerID)
		if err == nil && (status == storage.StatusRunning || status == storage.StatusStarting) {
			isRunning = true
		}
	}

	// One durable operation per server (OSS-229): the journal's active lock
	// refuses concurrent lifecycle mutations of the same world, and the
	// transition lease serializes in-process callers (OSS-230).
	transition, terr := docker.TryBeginServerTransition(server.ID)
	if terr != nil {
		return nil, connect.NewError(connect.CodeAborted, terr)
	}
	defer transition.Release()

	// Resolve the world transfer paths before touching anything, so a world
	// that cannot be transferred aborts with the source fully intact.
	srcRoot, dstRoot, rootErr := s.migrationWorldRoots(ctx, server, targetNode)
	if rootErr != nil {
		return nil, migrationResponseError(rootErr)
	}

	oldContainerID := server.ContainerID
	var migrateResp *v1.MigrateServerResponse
	migrateErr := s.runJournaledMutation(ctx, server, "migrate", requestID, func() error {
		var mErr error
		migrateResp, mErr = s.migrateServerNow(ctx, server, targetNode, sourceDockerCli, targetDockerCli, srcRoot, dstRoot, isRunning, oldContainerID, startTime)
		return mErr
	})
	if migrateErr != nil {
		return nil, migrationResponseError(migrateErr)
	}

	if s.bus != nil {
		s.bus.Emit(ctx, events.Event{
			Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_RESTART,
			ServerID: server.ID,
		})
	}

	return connect.NewResponse(migrateResp), nil
}
