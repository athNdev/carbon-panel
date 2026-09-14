package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/athNdev/mineserver/internal/command"
	"github.com/athNdev/mineserver/internal/config"
	storage "github.com/athNdev/mineserver/internal/db"
	"github.com/athNdev/mineserver/internal/docker"
	"github.com/athNdev/mineserver/internal/events"
	"github.com/athNdev/mineserver/internal/metrics"
	"github.com/athNdev/mineserver/internal/minecraft"
	"github.com/athNdev/mineserver/pkg/utils"
	"github.com/athNdev/mineserver/internal/module"
	"github.com/athNdev/mineserver/internal/proxy"
	"github.com/athNdev/mineserver/pkg/files"
	"github.com/athNdev/mineserver/pkg/logger"
	v1 "github.com/athNdev/mineserver/pkg/proto/mineserver/v1"
	"github.com/athNdev/mineserver/pkg/proto/mineserver/v1/mineserverv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Compile-time check that ServerService implements the interface
var _ mineserverv1connect.ServerServiceHandler = (*ServerService)(nil)

// ServerService implements the Server service
type ServerService struct {
	store            *storage.Store
	docker           *docker.Client
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
}

func (s *ServerService) getDockerClient(nodeID string) *docker.Client {
	if s.pool != nil {
		if cli, err := s.pool.GetClient(nodeID); err == nil && cli != nil {
			return cli
		}
	}
	return s.docker
}

// SetClientPool sets the client pool on the service
func (s *ServerService) SetClientPool(pool *docker.ClientPool) {
	s.pool = pool
}

// SetPlacementEngine sets the placement engine on the service
func (s *ServerService) SetPlacementEngine(engine *docker.PlacementEngine) {
	s.placementEngine = engine
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
	}
}

// dbServerToProto converts a database server model to proto server
func dbServerToProto(server *storage.Server) *v1.Server {
	if server == nil {
		return nil
	}

	// Convert JavaVersion string to int32
	javaVersion, _ := strconv.ParseInt(server.JavaVersion, 10, 32)

	protoServer := &v1.Server{
		Id:              server.ID,
		Name:            server.Name,
		Description:     server.Description,
		McVersion:       server.MCVersion,
		Port:            int32(server.Port),
		ProxyHostname:   server.ProxyHostname,
		ProxyListenerId: server.ProxyListenerID,
		ProxyPort:       int32(server.ProxyPort),
		MaxPlayers:      int32(server.MaxPlayers),
		Memory:          int32(server.Memory),
		DataPath:        server.DataPath,
		ContainerId:     server.ContainerID,
		JavaVersion:     int32(javaVersion),
		DockerImage:     server.DockerImage,
		AutoStart:       server.AutoStart,
		Detached:        server.Detached,
		TpsCommand:      server.TPSCommand,
		MemoryUsage:     int64(server.MemoryUsage),
		CpuPercent:      server.CPUPercent,
		DiskUsage:       server.DiskUsage,
		DiskTotal:       server.DiskTotal,
		WorldSize:       server.WorldSize,
		PlayersOnline:   int32(server.PlayersOnline),
		Tps:             server.TPS,
		AdditionalPorts: server.AdditionalPorts,
		CreatedAt:       timestamppb.New(server.CreatedAt),
		UpdatedAt:       timestamppb.New(server.UpdatedAt),

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

	// Update status from Docker and apply cached metrics
	for _, server := range servers {
		// If server uses proxy, ensure ProxyPort is populated from the listener
		if server.ProxyHostname != "" && server.ProxyListenerID != "" && listeners != nil {
			if listener, ok := listeners[server.ProxyListenerID]; ok {
				server.ProxyPort = listener.Port
			}
		}

		if server.ContainerID != "" {
			dockerCli := s.getDockerClient(server.NodeID)
			status, err := dockerCli.GetContainerStatus(ctx, server.ContainerID)
			if err == nil {
				server.Status = status
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

	// Update status from Docker
	if server.ContainerID != "" {
		dockerCli := s.getDockerClient(server.NodeID)
		status, err := dockerCli.GetContainerStatus(ctx, server.ContainerID)
		if err == nil {
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
		AdditionalPorts: additionalPorts,
		DockerOverrides: msg.DockerOverrides,
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

	// When using proxy, set the ports correctly
	if server.ProxyHostname != "" && proxyListenerID != "" {
		listener, err := s.store.GetProxyListener(ctx, proxyListenerID)
		if err == nil && listener != nil {
			server.ProxyPort = listener.Port
			server.Port = 25565 // Internal container port for proxied servers
		}
	}

	// Determine node placement
	nodeID := msg.NodeId
	if nodeID == "" && msg.PlacementStrategy != "" && s.placementEngine != nil {
		strategy := docker.PlacementStrategy(strings.ToLower(msg.PlacementStrategy))
		selectedNode, err := s.placementEngine.SelectNode(ctx, strategy, int64(server.Memory))
		if err != nil {
			s.log.Error("Failed to select node using strategy %s: %v", msg.PlacementStrategy, err)
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
		nodeID = "default"
	}
	server.NodeID = nodeID

	// Create data directory
	if err := os.MkdirAll(server.DataPath, 0755); err != nil {
		s.log.Error("Failed to create data directory: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create server directory"))
	}

	// Save to database
	if err := s.store.CreateServer(ctx, server); err != nil {
		s.log.Error("Failed to create server: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create server"))
	}

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
				destPath := filepath.Join(server.DataPath, "modpack.zip")

				// Copy the modpack file
				if sourceFile, err := os.Open(sourcePath); err == nil {
					defer sourceFile.Close()
					if destFile, err := os.Create(destPath); err == nil {
						defer destFile.Close()
						io.Copy(destFile, sourceFile)

						// Set CF_MODPACK_ZIP for manual modpack
						cfModpackZip := "/data/modpack.zip"
						serverConfig.CFModpackZip = &cfModpackZip

						// Set a dummy slug
						cfSlug := "manual-" + modpack.ID
						serverConfig.CFSlug = &cfSlug
					}
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
			var projectSpec string
			if msg.ModpackVersionId != "" && msg.ModpackVersionId != "latest" {
				projectSpec = fmt.Sprintf("%s:%s", modpack.IndexerID, msg.ModpackVersionId)
				s.log.Info("Using specific Modrinth version: %s", projectSpec)
			} else {
				projectSpec = modpack.IndexerID
				s.log.Info("Using latest Modrinth version for project: %s", projectSpec)
			}
			serverConfig.ModrinthModpack = &projectSpec
			downloadDeps := "required"
			serverConfig.ModrinthDownloadDependencies = &downloadDeps

			// Only set version type when using latest
			if msg.ModpackVersionId == "" || msg.ModpackVersionId == "latest" {
				versionType := "release"
				serverConfig.ModrinthModpackVersionType = &versionType
			}
		}

		// Update config with modpack settings
		if err := s.store.UpdateServerConfig(ctx, serverConfig); err != nil {
			s.log.Error("Failed to update server config with modpack settings: %v", err)
		}
	}

	// Create Docker container asynchronously
	go func() {
		bgCtx := context.Background()
		s.log.Info("Starting async Docker container creation for server %s", server.ID)

		s.prepareServerConfig(bgCtx, server, serverConfig)
		dockerCli := s.getDockerClient(server.NodeID)
		containerID, err := dockerCli.CreateContainer(bgCtx, server, serverConfig)
		if err != nil {
			s.log.Error("Failed to create container: %v", err)
			server.Status = storage.StatusError
			if updateErr := s.store.UpdateServer(bgCtx, server); updateErr != nil {
				s.log.Error("Failed to update server status to error: %v", updateErr)
			}
			return
		}

		server.ContainerID = containerID
		s.log.Info("Container created successfully for server %s: %s", server.ID, containerID)

		// Update server with container ID
		if err := s.store.UpdateServer(bgCtx, server); err != nil {
			s.log.Error("Failed to update server with container ID: %v", err)
			return
		}

		// Start the container immediately if requested
		if msg.StartImmediately {
			if err := dockerCli.StartContainer(bgCtx, containerID); err != nil {
				s.log.Error("Failed to start container: %v", err)
				server.Status = storage.StatusError
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
			// Update status in database
			if err := s.store.UpdateServer(bgCtx, server); err != nil {
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
			if err := s.store.UpdateServer(bgCtx, server); err != nil {
				s.log.Error("Failed to update server status: %v", err)
			}
			s.log.Info("Server %s created but not started immediately", server.ID)
		}
	}()

	// Return immediately with the server in "creating" state
	return connect.NewResponse(&v1.CreateServerResponse{
		Server: dbServerToProto(server),
	}), nil
}

// UpdateServer updates a server
func (s *ServerService) UpdateServer(ctx context.Context, req *connect.Request[v1.UpdateServerRequest]) (*connect.Response[v1.UpdateServerResponse], error) {
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

		server.Port = newPort
		needsRecreation = true
	}
	if msg.MaxPlayers > 0 {
		server.MaxPlayers = int(msg.MaxPlayers)
		needsRecreation = true
	}
	if msg.Memory > 0 && int(msg.Memory) != originalMemory {
		server.Memory = int(msg.Memory)
		needsRecreation = true
		if err := s.store.UpdateServerConfigMemory(ctx, server.ID, int(msg.Memory)); err != nil {
			s.log.Error("Failed to update server config memory: %v", err)
		}
	}
	if msg.ModLoader != "" && storage.ModLoader(msg.ModLoader) != originalModLoader {
		server.ModLoader = storage.ModLoader(msg.ModLoader)
		server.TPSCommand = minecraft.GetTPSCommand(server.ModLoader)
		needsRecreation = true
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
			})
		}

		server.AdditionalPorts = additionalPorts
		needsRecreation = true
	}

	// Handle docker overrides update
	if msg.DockerOverrides != nil {
		// Check that labels do not start with "MINESERVER."
		for key := range msg.DockerOverrides.Labels {
			if strings.HasPrefix(key, "MINESERVER.") {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("docker label keys cannot start with 'MINESERVER.', namespace reserved for internal management"))
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

				var projectSpec string
				if msg.ModpackVersionId != "" && msg.ModpackVersionId != "latest" {
					projectSpec = fmt.Sprintf("%s:%s", modpack.IndexerID, msg.ModpackVersionId)
				} else {
					projectSpec = modpack.IndexerID
				}
				serverConfig.ModrinthModpack = &projectSpec

				downloadDeps := "required"
				serverConfig.ModrinthDownloadDependencies = &downloadDeps

				if msg.ModpackVersionId == "" || msg.ModpackVersionId == "latest" {
					versionType := "release"
					serverConfig.ModrinthModpackVersionType = &versionType
				}
			}

			if err := s.store.UpdateServerConfig(ctx, serverConfig); err != nil {
				s.log.Error("Failed to update server config with modpack settings: %v", err)
			}
		}
	}

	// Save server updates first
	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update server"))
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
		dockerCli := s.getDockerClient(server.NodeID)
		result, err := dockerCli.RecreateContainer(ctx, server.ContainerID, server, serverConfig)
		if err != nil {
			s.log.Error("Failed to recreate container: %v", err)
			if result != nil && result.NewContainerID != "" {
				// Container was created but failed to start
				server.ContainerID = result.NewContainerID
				server.Status = storage.StatusError
			} else {
				// Complete failure
				server.Status = storage.StatusError
				server.ContainerID = ""
			}
		} else {
			server.ContainerID = result.NewContainerID
			if result.WasRunning {
				server.Status = storage.StatusRunning
			} else {
				server.Status = storage.StatusStopped
			}
		}

		// Update server with new container ID and status
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server after container recreation: %v", err)
		}
	}

	return connect.NewResponse(&v1.UpdateServerResponse{
		Server: dbServerToProto(server),
	}), nil
}

// DeleteServer deletes a server
func (s *ServerService) DeleteServer(ctx context.Context, req *connect.Request[v1.DeleteServerRequest]) (*connect.Response[v1.DeleteServerResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Remove proxy route if configured
	if s.proxy != nil && server.ProxyHostname != "" {
		if err := s.proxy.RemoveServerRoute(server.ID); err != nil {
			s.log.Error("Failed to remove proxy route: %v", err)
		}
	}

	// Stop and remove module containers before deleting the server
	// (database cascade will delete module records, but containers need cleanup)
	if s.moduleManager != nil {
		modules, err := s.store.ListServerModules(ctx, server.ID)
		if err == nil {
			for _, mod := range modules {
				if mod.ContainerID != "" {
					if err := s.moduleManager.StopModule(ctx, mod.ID); err != nil {
						s.log.Error("Failed to stop module %s: %v", mod.ID, err)
					}
					if err := s.moduleManager.DeleteModule(ctx, mod.ID); err != nil {
						s.log.Error("Failed to delete module %s: %v", mod.ID, err)
					}
				}
			}
		}
	}

	// Stop and remove container
	if server.ContainerID != "" {
		dockerCli := s.getDockerClient(server.NodeID)
		if _, err := dockerCli.StopContainer(ctx, server.ContainerID); err != nil {
			s.log.Error("Failed to stop container: %v", err)
		}
		if err := dockerCli.RemoveContainer(ctx, server.ContainerID); err != nil {
			s.log.Error("Failed to remove container: %v", err)
		}
	}

	// Delete from database
	if err := s.store.DeleteServer(ctx, req.Msg.Id); err != nil {
		s.log.Error("Failed to delete server: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete server"))
	}

	// Delete data directory
	if err := os.RemoveAll(server.DataPath); err != nil {
		s.log.Error("Failed to delete server data: %v", err)
	}

	return connect.NewResponse(&v1.DeleteServerResponse{}), nil
}

// StartServer starts a server
func (s *ServerService) StartServer(ctx context.Context, req *connect.Request[v1.StartServerRequest]) (*connect.Response[v1.StartServerResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	dockerCli := s.getDockerClient(server.NodeID)

	// If container doesn't exist, create it first
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
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update server"))
		}
	}

	// Start container
	if err := dockerCli.StartContainer(ctx, server.ContainerID); err != nil {
		s.log.Error("Failed to start container, attempting to recreate: %v", err)

		// Get server config for container creation
		serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
		if err != nil {
			s.log.Error("Failed to get server config: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server configuration"))
		}

		// Recreate container
		result, err := dockerCli.RecreateContainer(ctx, server.ContainerID, server, serverConfig)
		if err != nil {
			s.log.Error("Failed to recreate container: %v", err)
			if result != nil && result.NewContainerID != "" {
				// Container was created but failed to start
				server.ContainerID = result.NewContainerID
				server.Status = storage.StatusError
			} else {
				// Complete failure
				server.Status = storage.StatusError
				server.ContainerID = ""
			}
		} else {
			server.ContainerID = result.NewContainerID
			if result.WasRunning {
				server.Status = storage.StatusRunning
			} else {
				server.Status = storage.StatusStopped
			}
		}

		// Update server with new container ID and status
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server after container recreation: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update server after container recreation"))
		}
	}

	// Update server status
	now := time.Now()
	server.Status = storage.StatusStarting
	server.LastStarted = &now

	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server status: %v", err)
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
	}

	// Emit the server-start event
	if s.bus != nil {
		s.bus.Emit(ctx, events.Event{
			Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START,
			ServerID: server.ID,
		})
	}

	return connect.NewResponse(&v1.StartServerResponse{
		Status: "starting",
	}), nil
}

// StopServer stops a server
func (s *ServerService) StopServer(ctx context.Context, req *connect.Request[v1.StopServerRequest]) (*connect.Response[v1.StopServerResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	if server.ContainerID == "" {
		// If there's no container, server is already stopped
		server.Status = storage.StatusStopped
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server status: %v", err)
		}
		return connect.NewResponse(&v1.StopServerResponse{
			Status: "stopped",
		}), nil
	}

	// Stop container
	dockerCli := s.getDockerClient(server.NodeID)
	found, err := dockerCli.StopContainer(ctx, server.ContainerID)
	if err != nil {
		s.log.Error("Failed to stop container: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to stop server"))
	}

	// If container wasn't found, clean up stale reference
	if !found {
		s.log.Warn("Container %s not found, cleaning up stale reference", server.ContainerID)
		server.ContainerID = ""
		server.Status = storage.StatusStopped
	} else {
		server.Status = storage.StatusStopping
	}

	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server status: %v", err)
	}

	// Remove proxy route if enabled
	if s.proxy != nil && server.ProxyHostname != "" {
		if err := s.proxy.RemoveServerRoute(server.ID); err != nil {
			s.log.Error("Failed to remove proxy route: %v", err)
		}
	}

	// Emit the server-stop event
	if s.bus != nil {
		s.bus.Emit(ctx, events.Event{
			Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_STOP,
			ServerID: server.ID,
		})
	}

	status := "stopping"
	if !found {
		status = "stopped"
	}
	return connect.NewResponse(&v1.StopServerResponse{
		Status: status,
	}), nil
}

// RestartServer restarts a server
func (s *ServerService) RestartServer(ctx context.Context, req *connect.Request[v1.RestartServerRequest]) (*connect.Response[v1.RestartServerResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	dockerCli := s.getDockerClient(server.NodeID)

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
		containerID, err := dockerCli.CreateContainer(ctx, server, serverConfig)
		if err != nil {
			s.log.Error("Failed to create container: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create server container"))
		}

		server.ContainerID = containerID
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server with container ID: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update server"))
		}

		// Now start the container
		if err := dockerCli.StartContainer(ctx, server.ContainerID); err != nil {
			s.log.Error("Failed to start container: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start server"))
		}

		// Update server status
		now := time.Now()
		server.Status = storage.StatusStarting
		server.LastStarted = &now

		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server status: %v", err)
		}

		// Clear ephemeral configuration fields
		if err := s.store.ClearEphemeralConfigFields(ctx, server.ID); err != nil {
			s.log.Error("Failed to clear ephemeral config fields: %v", err)
		}

		// Emit the server-restart event
		if s.bus != nil {
			s.bus.Emit(ctx, events.Event{
				Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_RESTART,
				ServerID: server.ID,
			})
		}

		return connect.NewResponse(&v1.RestartServerResponse{
			Status: "starting",
		}), nil
	}

	// Restart container
	if err := dockerCli.RestartContainer(ctx, server.ContainerID, 2*time.Second); err != nil {
		s.log.Error("Failed to restart container: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to restart server"))
	}

	// Update server status
	now := time.Now()
	server.Status = storage.StatusStarting
	server.LastStarted = &now
	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server status: %v", err)
	}

	// Clear ephemeral configuration fields
	if err := s.store.ClearEphemeralConfigFields(ctx, server.ID); err != nil {
		s.log.Error("Failed to clear ephemeral config fields: %v", err)
	}

	// Emit the server-restart event
	if s.bus != nil {
		s.bus.Emit(ctx, events.Event{
			Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_RESTART,
			ServerID: server.ID,
		})
	}

	return connect.NewResponse(&v1.RestartServerResponse{
		Status: "restarting",
	}), nil
}

// Destroys and recreates a server container from scratch - brute force reset
func (s *ServerService) RecreateServer(ctx context.Context, req *connect.Request[v1.RecreateServerRequest]) (*connect.Response[v1.RecreateServerResponse], error) {
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
	dockerCli := s.getDockerClient(server.NodeID)
	result, err := dockerCli.RecreateContainer(ctx, server.ContainerID, server, serverConfig)
	if err != nil {
		s.log.Error("Failed to recreate container: %v", err)
		server.Status = storage.StatusError
		if updateErr := s.store.UpdateServer(ctx, server); updateErr != nil {
			s.log.Error("Failed to update server status: %v", updateErr)
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to recreate server container"))
	}

	server.ContainerID = result.NewContainerID

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

	dockerCli := s.getDockerClient(server.NodeID)
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

// Reads the server's latest.log and uploads it to mclo.gs
func (s *ServerService) UploadToMCLogs(ctx context.Context, req *connect.Request[v1.UploadToMCLogsRequest]) (*connect.Response[v1.UploadToMCLogsResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	logPath := filepath.Join(server.DataPath, "logs", "latest.log")
	content, err := os.ReadFile(logPath)
	if err != nil {
		s.log.Error("Failed to read server log file: %v", err)
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("log file not found"))
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
		"source":  fmt.Sprintf("MINESERVER-%s", server.Name),
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

	// Find the next available port starting from 25565
	var nextPort int32 = 25565
	for usedPortsMap[nextPort] {
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
	if sourceNodeID == "" {
		sourceNodeID = "default"
	}

	if sourceNodeID == targetNodeID {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("server is already hosted on node '%s'", targetNodeID))
	}

	// Retrieve target node & validate
	var targetNode *storage.Node
	if targetNodeID == "default" {
		targetNode = &storage.Node{
			ID:      "default",
			Name:    "Local Controller Daemon",
			Status:  storage.NodeStatusOnline,
			Enabled: true,
			IsLocal: true,
		}
	} else {
		targetNode, err = s.store.GetNode(ctx, targetNodeID)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("target node '%s' not found: %w", targetNodeID, err))
		}
	}

	if !targetNode.Enabled {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("target node '%s' is disabled", targetNode.Name))
	}

	// Verify target node connectivity
	if s.pool != nil && targetNodeID != "default" {
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

	sourceDockerCli := s.getDockerClient(sourceNodeID)
	targetDockerCli := s.getDockerClient(targetNodeID)

	// Check if server is currently running
	var isRunning bool
	if server.ContainerID != "" && sourceDockerCli != nil {
		status, err := sourceDockerCli.GetContainerStatus(ctx, server.ContainerID)
		if err == nil && (status == storage.StatusRunning || status == storage.StatusStarting) {
			isRunning = true
		}
	}

	serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server configuration: %w", err))
	}

	oldContainerID := server.ContainerID

	if isRunning {
		s.log.Info("Initiating live migration for server '%s' (%s) from node '%s' to node '%s'", server.Name, server.ID, sourceNodeID, targetNodeID)

		// 1. Flush world chunks to disk
		if s.sender != nil && oldContainerID != "" {
			_, _ = s.sender.SendCommand(ctx, server.ID, "save-all flush")
		}

		// 2. Stop old container cleanly to avoid level.dat lock contention
		if oldContainerID != "" && sourceDockerCli != nil {
			_, _ = sourceDockerCli.StopContainer(ctx, oldContainerID)
		}

		// 3. Prepare config and create new container on target node
		s.prepareServerConfig(ctx, server, serverConfig)
		var newContainerID string
		if targetDockerCli != nil {
			newContainerID, err = targetDockerCli.CreateContainer(ctx, server, serverConfig)
			if err != nil {
				s.log.Error("Migration target container creation failed: %v", err)
				// Rollback: try restarting old container on source node
				if oldContainerID != "" && sourceDockerCli != nil {
					_ = sourceDockerCli.StartContainer(ctx, oldContainerID)
				}
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create container on target node: %w", err))
			}

			// 4. Start container on target node
			if err := targetDockerCli.StartContainer(ctx, newContainerID); err != nil {
				s.log.Error("Migration target container start failed: %v", err)
				_ = targetDockerCli.RemoveContainer(ctx, newContainerID)
				// Rollback: restart old container
				if oldContainerID != "" && sourceDockerCli != nil {
					_ = sourceDockerCli.StartContainer(ctx, oldContainerID)
				}
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start container on target node: %w", err))
			}
		}

		// 5. Update DB record
		server.NodeID = targetNodeID
		server.ContainerID = newContainerID
		server.Status = storage.StatusRunning
		now := time.Now()
		server.LastStarted = &now
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server DB record during live migration: %v", err)
		}

		// 6. Fast proxy route cutover
		if s.proxy != nil && server.ProxyHostname != "" {
			if err := s.proxy.UpdateServerRoute(server); err != nil {
				s.log.Error("Failed to update proxy route during migration: %v", err)
			}
		}

		// 7. Cleanup old container on source node asynchronously
		if oldContainerID != "" && sourceDockerCli != nil {
			go func(cID string, sCli *docker.Client) {
				time.Sleep(1 * time.Second)
				_ = sCli.RemoveContainer(context.Background(), cID)
			}(oldContainerID, sourceDockerCli)
		}

	} else {
		s.log.Info("Initiating offline migration for server '%s' (%s) from node '%s' to node '%s'", server.Name, server.ID, sourceNodeID, targetNodeID)

		// Remove old container if it exists on source node
		if oldContainerID != "" && sourceDockerCli != nil {
			_, _ = sourceDockerCli.StopContainer(ctx, oldContainerID)
			_ = sourceDockerCli.RemoveContainer(ctx, oldContainerID)
		}

		// Update database record
		server.NodeID = targetNodeID
		server.ContainerID = "" // Reset container ID so next start creates it fresh on target node
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server record: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update server: %w", err))
		}
	}

	durationMs := time.Since(startTime).Milliseconds()
	msg := fmt.Sprintf("Successfully migrated '%s' to node '%s' (%dms, live=%v)", server.Name, targetNodeID, durationMs, isRunning)
	s.log.Info("%s", msg)

	if s.bus != nil {
		s.bus.Emit(ctx, events.Event{
			Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_RESTART,
			ServerID: server.ID,
		})
	}

	return connect.NewResponse(&v1.MigrateServerResponse{
		Success:      true,
		Message:      msg,
		SourceNodeId: sourceNodeID,
		TargetNodeId: targetNodeID,
		DurationMs:   durationMs,
		Server:       dbServerToProto(server),
	}), nil
}

