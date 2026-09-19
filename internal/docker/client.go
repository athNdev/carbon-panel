package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	models "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/minecraft"
	"github.com/athNdev/carbon-panel/pkg/utils"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

const (
	// Docker images manifest URL from itzg/docker-minecraft-server repo
	dockerImagesURL = "https://raw.githubusercontent.com/itzg/docker-minecraft-server/refs/heads/master/images.json"

	// Cache for 1 hour
	dockerImagesCacheDuration = time.Hour

	// Default Minecraft server port inside containers
	DefaultMinecraftPort = 25565

	// Default RCON port inside containers
	DefaultRCONPort = 25575

	// Offset added to game port for RCON host binding
	RCONPortOffset = 10

	// DefaultRestartPolicy is the Docker restart policy applied to every
	// Carbon Panel container (MINE-122 anti-flap). unless-stopped lets the
	// daemon re-attempt a crashed container while honouring an explicit
	// operator /stop; the reconciler treats restarting containers as
	// settling (reconciler.go) instead of flapping status. Pinned by
	// TestDefaultRestartPolicy — change deliberately, not accidentally.
	DefaultRestartPolicy = container.RestartPolicyMode("unless-stopped")
)

type ContainerStats struct {
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryUsage float64 `json:"memory_usage"` // in MB
	MemoryLimit float64 `json:"memory_limit"` // in MB
}

type DockerImageTag struct {
	Tag           string   `json:"tag"`           // Docker tag name (e.g., "latest", "java21", etc.)
	Java          string   `json:"java"`          // Java version number
	Distribution  string   `json:"distribution"`  // Linux distribution (ubuntu, alpine, oracle)
	JVM           string   `json:"jvm"`           // JVM type (hotspot, graalvm)
	Architectures []string `json:"architectures"` // Supported architectures
	Deprecated    bool     `json:"deprecated"`    // Whether this tag is deprecated
	LTS           bool     `json:"lts"`           // Whether this is an LTS version
	JDK           bool     `json:"jdk"`           // Whether this includes JDK
	Notes         string   `json:"notes"`         // Additional notes about the tag
}

// Cached docker images data
type dockerImagesCache struct {
	mu            sync.RWMutex
	images        []DockerImageTag
	lastFetchTime time.Time
}

var dockerCache = &dockerImagesCache{}

// Converts a container-internal path to a host path.
// When CARBONPANEL_HOST_DATA_PATH is not set (running on host), it returns the path unchanged.
func TranslateToHostPath(path string) string {
	hostDataPath := os.Getenv("CARBONPANEL_HOST_DATA_PATH")
	if hostDataPath == "" {
		return path
	}
	containerDataDir := os.Getenv("CARBONPANEL_DATA_DIR")
	if containerDataDir == "" {
		containerDataDir = "/app/data"
	}
	relPath, err := filepath.Rel(containerDataDir, path)
	if err != nil || strings.HasPrefix(relPath, "..") {
		// Path is not under the container data dir, return as-is
		return path
	}
	return filepath.Join(hostDataPath, relPath)
}

// Fetches the docker images manifest from itzg
func fetchDockerImages() ([]DockerImageTag, error) {
	// Check cache first
	dockerCache.mu.RLock()
	if len(dockerCache.images) > 0 && time.Since(dockerCache.lastFetchTime) < dockerImagesCacheDuration {
		images := dockerCache.images
		dockerCache.mu.RUnlock()
		return images, nil
	}
	dockerCache.mu.RUnlock()

	// Fetch new manifest
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(dockerImagesURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch docker images manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch docker images manifest: status code %d", resp.StatusCode)
	}

	var images []DockerImageTag
	if err := json.NewDecoder(resp.Body).Decode(&images); err != nil {
		return nil, fmt.Errorf("failed to decode docker images manifest: %w", err)
	}

	// Update cache
	dockerCache.mu.Lock()
	dockerCache.images = images
	dockerCache.lastFetchTime = time.Now()
	dockerCache.mu.Unlock()

	return images, nil
}

// Gets ideal docker tag for a given Minecraft version + mod loader
func GetOptimalDockerTag(mcVersion string, modLoader models.ModLoader, preferGraalVM bool) string {
	javaVersion := GetRequiredJavaVersion(mcVersion, modLoader)
	if javaVersion == "0" || javaVersion == "" {
		// Could not determine Java version, use stable
		return "stable"
	}

	// Fetch Docker images from API
	images, err := fetchDockerImages()
	if err != nil {
		// Could not fetch Docker images, use stable
		return "stable"
	}

	// Find matching tag
	for _, tag := range images {
		if tag.Java == javaVersion && !tag.Deprecated {
			if preferGraalVM && strings.Contains(tag.Tag, "graalvm") {
				return tag.Tag
			}
			// Return first matching non-special tag (not graalvm, alpine, or jdk)
			if !strings.Contains(tag.Tag, "graalvm") && !strings.Contains(tag.Tag, "alpine") && !strings.Contains(tag.Tag, "jdk") {
				return tag.Tag
			}
		}
	}

	// No matching tag found, construct one
	return fmt.Sprintf("java%s", javaVersion)
}

// Gets required Java version for a Minecraft version
func GetRequiredJavaVersion(mcVersion string, modLoader models.ModLoader) string {
	// Fetch the Java version from the Minecraft version metadata
	javaVersion, err := minecraft.GetJavaVersion(mcVersion)
	if err != nil {
		// If we can't determine the Java version, return 0 to indicate error
		return "0"
	}
	return javaVersion
}

type ClientConfig struct {
	APIVersion      string
	NetworkName     string
	RegistryURL     string
	DNS             string
	Labels          map[string]string
	EnableRateLimit bool
	RateLimitPerMin int
	RateLimitBurst  int
}

type ContainerLogStreamer interface {
	StartStreaming(containerID string) error
	StopStreaming(containerID string)
	MigrateSubscribers(oldContainerID, newContainerID string)
	RemoveContainer(containerID string)
}

type Client struct {
	docker      *client.Client
	config      ClientConfig
	logStreamer ContainerLogStreamer
	log         *logger.Logger
	firewall    *FirewallManager
}

// Auto manage streams at the client level when set
func (c *Client) SetLogStreamer(ls ContainerLogStreamer) {
	c.logStreamer = ls
}

func NewClient(host string, log *logger.Logger, config ...ClientConfig) (*Client, error) {
	opts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}

	// Apply API version if provided
	if len(config) > 0 && config[0].APIVersion != "" {
		opts = append(opts, client.WithVersion(config[0].APIVersion))
	}

	if host != "" && host != "unix:///var/run/docker.sock" {
		opts = append(opts, client.WithHost(host))
	}

	docker, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	c := &Client{docker: docker, log: log}
	if len(config) > 0 {
		c.config = config[0]
	} else {
		// Set defaults
		c.config = ClientConfig{
			NetworkName:     "carbon-panel-network",
			EnableRateLimit: true,
			RateLimitPerMin: 10,
			RateLimitBurst:  20,
		}
	}
	c.firewall = NewFirewallManager(log, c.config.EnableRateLimit, c.config.RateLimitPerMin, c.config.RateLimitBurst)

	return c, nil
}

// GetFirewallManager returns the firewall manager for DOCKER-USER rules
func (c *Client) GetFirewallManager() *FirewallManager {
	return c.firewall
}

func (c *Client) Close() error {
	if c == nil || c.docker == nil {
		return nil
	}
	return c.docker.Close()
}

// Get the docker client instance from the client object
func (c *Client) GetDockerClient() *client.Client {
	return c.docker
}

// Ping checks connectivity to the Docker daemon
func (c *Client) Ping(ctx context.Context) (types.Ping, error) {
	if c == nil || c.docker == nil {
		return types.Ping{}, fmt.Errorf("docker client is nil")
	}
	return c.docker.Ping(ctx)
}

// ApplyOverrides applies DockerOverrides to container and host configs
func ApplyOverrides(overrides *v1.DockerOverrides, config *container.Config, hostConfig *container.HostConfig) {
	if overrides == nil {
		return
	}

	// Apply environment variable overrides
	if len(overrides.GetEnvironment()) > 0 {
		for key, value := range overrides.GetEnvironment() {
			config.Env = append(config.Env, fmt.Sprintf("%s=%s", key, value))
		}
	}

	// Apply additional volume mounts
	for _, vol := range overrides.GetVolumes() {
		mountType := mount.Type(vol.GetType())
		if mountType == "" {
			mountType = mount.TypeBind
		}
		hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{
			Type:     mountType,
			Source:   vol.GetSource(),
			Target:   vol.GetTarget(),
			ReadOnly: vol.GetReadOnly(),
		})
	}

	// Apply restart policy override
	if overrides.GetRestartPolicy() != "" {
		hostConfig.RestartPolicy = container.RestartPolicy{
			Name: container.RestartPolicyMode(overrides.GetRestartPolicy()),
		}
	}

	// Apply resource limits
	if overrides.GetCpuLimit() > 0 {
		hostConfig.Resources.NanoCPUs = int64(overrides.GetCpuLimit() * 1e9)
	}
	if overrides.GetCpusetCpus() != "" {
		hostConfig.Resources.CpusetCpus = overrides.GetCpusetCpus()
	}
	if overrides.GetMemoryLimit() > 0 {
		hostConfig.Resources.Memory = overrides.GetMemoryLimit() * 1024 * 1024
		hostConfig.Resources.MemorySwap = overrides.GetMemoryLimit() * 1024 * 1024
	}

	// Apply additional labels
	if len(overrides.GetLabels()) > 0 {
		maps.Copy(config.Labels, overrides.GetLabels())
	}

	// Apply capabilities
	if len(overrides.GetCapAdd()) > 0 {
		hostConfig.CapAdd = overrides.GetCapAdd()
	}
	if len(overrides.GetCapDrop()) > 0 {
		hostConfig.CapDrop = overrides.GetCapDrop()
	}

	// Apply devices
	for _, device := range overrides.GetDevices() {
		parts := strings.Split(device, ":")
		if len(parts) >= 2 {
			hostConfig.Devices = append(hostConfig.Devices, container.DeviceMapping{
				PathOnHost:        parts[0],
				PathInContainer:   parts[1],
				CgroupPermissions: "rwm",
			})
		}
	}

	// Apply extra hosts
	if len(overrides.GetExtraHosts()) > 0 {
		hostConfig.ExtraHosts = overrides.GetExtraHosts()
	}

	// Apply security settings
	hostConfig.Privileged = overrides.GetPrivileged()
	hostConfig.ReadonlyRootfs = overrides.GetReadOnly()
	if len(overrides.GetSecurityOpt()) > 0 {
		hostConfig.SecurityOpt = overrides.GetSecurityOpt()
	}

	// Apply PIDs limit (fork bomb guard)
	if overrides.GetPidsLimit() > 0 {
		pids := overrides.GetPidsLimit()
		hostConfig.Resources.PidsLimit = &pids
	}

	// Apply SHM size
	if overrides.GetShmSize() > 0 {
		hostConfig.ShmSize = overrides.GetShmSize()
	}

	// Apply user
	if overrides.GetUser() != "" {
		config.User = overrides.GetUser()
	}

	// Apply working directory
	if overrides.GetWorkingDir() != "" {
		config.WorkingDir = overrides.GetWorkingDir()
	}

	// Apply entrypoint
	if len(overrides.GetEntrypoint()) > 0 {
		config.Entrypoint = overrides.GetEntrypoint()
	}

	// Apply command
	if len(overrides.GetCommand()) > 0 {
		config.Cmd = overrides.GetCommand()
	}

	// Apply network mode override
	if overrides.GetNetworkMode() != "" {
		hostConfig.NetworkMode = container.NetworkMode(overrides.GetNetworkMode())
	}

	// Apply DNS override
	if len(overrides.GetDns()) > 0 {
		hostConfig.DNS = overrides.GetDns()
	}
}

func (c *Client) CreateContainer(ctx context.Context, server *models.Server, serverConfig *models.ServerConfig) (string, error) {
	// Use server's DockerImage if specified, otherwise determine based on version and loader
	var imageName string
	if server.DockerImage != "" {
		imageName = "itzg/minecraft-server:" + server.DockerImage
	} else {
		imageName = getDockerImage(server.ModLoader, server.MCVersion)
	}

	// Try pulling latest
	if err := c.pullImage(ctx, imageName); err != nil {
		return "", fmt.Errorf("failed to pull image: %w", err)
	}

	// Build environment variables
	env := buildEnvFromConfig(serverConfig)

	// Determine container port - proxy servers always use default port internally
	useProxy := server.ProxyHostname != ""
	containerPort := server.Port
	if useProxy {
		containerPort = DefaultMinecraftPort
		// Override SERVER_PORT env var for proxy servers
		filtered := make([]string, 0, len(env))
		for _, e := range env {
			if !strings.HasPrefix(e, "SERVER_PORT=") {
				filtered = append(filtered, e)
			}
		}
		env = append(filtered, fmt.Sprintf("SERVER_PORT=%d", DefaultMinecraftPort))
	}

	c.log.Debug("Creating container for server %s with image %s", server.ID, imageName)

	// Build exposed ports
	exposedPorts := nat.PortSet{
		nat.Port(fmt.Sprintf("%d/tcp", containerPort)):   struct{}{},
		nat.Port(fmt.Sprintf("%d/tcp", DefaultRCONPort)): struct{}{},
	}
	for _, port := range server.AdditionalPorts {
		protocol := port.GetProtocol()
		if protocol == "" {
			protocol = "tcp"
		}
		exposedPorts[nat.Port(fmt.Sprintf("%d/%s", port.GetContainerPort(), protocol))] = struct{}{}
	}

	// Build port bindings
	portBindings := nat.PortMap{}
	if !useProxy {
		// Bind game port to public host interfaces (0.0.0.0)
		portBindings[nat.Port(fmt.Sprintf("%d/tcp", containerPort))] = []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: fmt.Sprintf("%d", server.Port)},
		}
		// Bind RCON to localhost only
		portBindings[nat.Port(fmt.Sprintf("%d/tcp", DefaultRCONPort))] = []nat.PortBinding{
			{HostIP: "127.0.0.1", HostPort: fmt.Sprintf("%d", server.Port+RCONPortOffset)},
		}

		// Apply DOCKER-USER iptables rate limiting & SYN flood protection (MINE-9)
		if c.firewall != nil && server.Port > 0 {
			_ = c.firewall.RemoveProxyPortIsolation(ctx, server.Port)
			_ = c.firewall.ApplyPortRateLimiting(ctx, server.Port, c.config.RateLimitPerMin, c.config.RateLimitBurst)
		}
	} else {
		// Automatic Proxy Port Isolation Guard (MINE-13):
		// When routed through Velocity/proxy, bind server container port strictly to loopback (127.0.0.1)
		// so external players cannot bypass Velocity by connecting directly to the server's backend port.
		if server.Port > 0 {
			portBindings[nat.Port(fmt.Sprintf("%d/tcp", containerPort))] = []nat.PortBinding{
				{HostIP: "127.0.0.1", HostPort: fmt.Sprintf("%d", server.Port)},
			}
		}
		// Bind RCON to localhost only
		portBindings[nat.Port(fmt.Sprintf("%d/tcp", DefaultRCONPort))] = []nat.PortBinding{
			{HostIP: "127.0.0.1", HostPort: fmt.Sprintf("%d", server.Port+RCONPortOffset)},
		}

		// Apply DOCKER-USER iptables proxy port isolation guard
		if c.firewall != nil && server.Port > 0 {
			_ = c.firewall.RemovePortRateLimiting(ctx, server.Port)
			_ = c.firewall.ApplyProxyPortIsolation(ctx, server.Port)
		}
	}
	// Add additional port bindings
	for _, port := range server.AdditionalPorts {
		protocol := port.GetProtocol()
		if protocol == "" {
			protocol = "tcp"
		}
		portKey := nat.Port(fmt.Sprintf("%d/%s", port.GetContainerPort(), protocol))
		portBindings[portKey] = []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: fmt.Sprintf("%d", port.GetHostPort())},
		}
		c.log.Debug("Additional port mapping: %s (%d:%d/%s)", port.GetName(), port.GetHostPort(), port.GetContainerPort(), protocol)
	}

	// Handle path translation when Carbon Panel runs in a container
	dataPath := TranslateToHostPath(server.DataPath)

	if err := os.MkdirAll(server.DataPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create server data directory: %w", err)
	}

	config := &container.Config{
		Image:        imageName,
		Env:          env,
		Tty:          true,
		AttachStdout: true,
		AttachStderr: true,
		ExposedPorts: exposedPorts,
		Labels: map[string]string{
			"carbon-panel.server.id":      server.ID,
			"carbon-panel.server.name":    server.Name,
			"carbon-panel.server.loader":  string(server.ModLoader),
			"carbon-panel.server.version": server.MCVersion,
			"carbon-panel.managed":        "true",
			"carbon-panel.node.id":        server.NodeID,
			"carbon-panel.schema-version": "2",
			// Opt out of generic third-party auto-updaters/healers so they
			// don't fight Carbon Panel's own container lifecycle management.
			"com.centurylinklabs.watchtower.enable": "false",
			"io.containrrr.watchtower.enable":       "false",
			"autoheal":                              "false",
			"willfarrell.autoheal":                  "false",
		},
	}

	// Apply dynamic memory headroom & OOM-kill (exit 137) guard (MINE-4)
	alloc := utils.CalculateMemoryAllocation(server.Memory)
	containerLimitBytes := alloc.ContainerLimitBytes
	if serverConfig.MaxMemory != nil && *serverConfig.MaxMemory != "" {
		if customMaxMB, err := utils.ParseMemoryMB(*serverConfig.MaxMemory); err == nil && customMaxMB > 0 {
			_, containerLimitBytes = utils.EnsureMemoryHeadroom(customMaxMB, alloc.ContainerLimitMB)
		}
	}

	pidsLimit := int64(512)
	hostConfig := &container.HostConfig{
		PortBindings: portBindings,
		Mounts: []mount.Mount{
			{Type: mount.TypeBind, Source: dataPath, Target: "/data", BindOptions: &mount.BindOptions{CreateMountpoint: true}},
		},
		RestartPolicy: container.RestartPolicy{Name: DefaultRestartPolicy},
		CapDrop:       []string{"ALL"},
		SecurityOpt:   []string{"no-new-privileges:true"},
		Resources: container.Resources{
			Memory:     containerLimitBytes,
			MemorySwap: containerLimitBytes,
			PidsLimit:  &pidsLimit,
		},
		LogConfig: container.LogConfig{
			Type:   "json-file",
			Config: map[string]string{"max-size": "10m", "max-file": "3"},
		},
	}

	// Apply global DNS from config
	if c.config.DNS != "" {
		hostConfig.DNS = []string{c.config.DNS}
	}

	// Apply global labels from config
	if c.config.Labels != nil {
		maps.Copy(config.Labels, c.config.Labels)
	}

	// Apply docker overrides
	ApplyOverrides(server.DockerOverrides, config, hostConfig)

	// Network configuration
	networkConfig := &network.NetworkingConfig{}
	if c.config.NetworkName != "" && hostConfig.NetworkMode == "" {
		networkConfig.EndpointsConfig = map[string]*network.EndpointSettings{
			c.config.NetworkName: {},
		}
	}

	containerName := fmt.Sprintf("carbon-panel-server-%s", server.ID)

	// Idempotent create (Wings pattern, MINE-104): before attempting creation, check whether
	// a container already claims this server's identity (deterministic name / label). If it's
	// alive, adopt it instead of creating a duplicate. If it's a stale leftover, remove it so
	// the name is free for a fresh container.
	if adoptedID, ok, err := c.preflightResolveForCreate(ctx, "server", server.ID, c.ResolveContainer); err != nil {
		return "", err
	} else if ok {
		return adoptedID, nil
	}

	containerID, err := c.createContainerWithConflictRetry(ctx, config, hostConfig, networkConfig, containerName, func() (*container.Summary, error) {
		return c.ResolveContainer(ctx, server.ID)
	})
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	return containerID, nil
}

// isConflictError reports whether err represents a Docker API 409 Conflict, e.g. attempting to
// create a container whose name is already in use. Prefers the containerd errdefs helper (same
// convention already used for errdefs.IsNotFound elsewhere in this file) over string matching.
func isConflictError(err error) bool {
	return errdefs.IsConflict(err)
}

// preflightResolveForCreate checks, before attempting Docker container creation, whether a
// container already exists for the given identity (server or module ID) via resolve.
//
//   - If nothing is found (ErrContainerNotResolved), returns ("", false, nil) so the caller
//     proceeds to create normally.
//   - If a container is found and looks alive (running/restarting/paused), it is adopted:
//     returns (containerID, true, nil) so the caller returns that ID instead of creating a new one.
//   - If a container is found but looks stale (created/exited/dead/removing/anything else), it is
//     force-removed so its name is free, then returns ("", false, nil) so the caller proceeds to create.
//   - Any transient resolve error (not ErrContainerNotResolved) is logged and treated as "nothing
//     found" so a resolve hiccup never blocks container creation outright.
func (c *Client) preflightResolveForCreate(ctx context.Context, kind, id string, resolve func(ctx context.Context, id string) (*container.Summary, error)) (adoptedID string, adopted bool, err error) {
	existing, resolveErr := resolve(ctx, id)
	if resolveErr != nil {
		if errors.Is(resolveErr, ErrContainerNotResolved) {
			return "", false, nil
		}
		c.log.Warn("Preflight resolve for %s %s failed, proceeding with creation attempt: %v", kind, id, resolveErr)
		return "", false, nil
	}

	if isAliveContainerState(existing.State) {
		c.log.Info("Adopting existing %s container %s (name=%v, state=%s) instead of creating a duplicate", kind, existing.ID, existing.Names, existing.State)
		return existing.ID, true, nil
	}

	c.log.Warn("Found stale %s container %s (name=%v, state=%s), removing before creating a fresh one", kind, existing.ID, existing.Names, existing.State)
	if rmErr := c.docker.ContainerRemove(ctx, existing.ID, container.RemoveOptions{Force: true}); rmErr != nil && !errdefs.IsNotFound(rmErr) {
		return "", false, fmt.Errorf("failed to remove stale %s container %s: %w", kind, existing.ID, rmErr)
	}
	return "", false, nil
}

// isAliveContainerState reports whether a container in this state should be adopted rather than
// treated as a stale leftover to remove.
func isAliveContainerState(state container.ContainerState) bool {
	switch state {
	case container.StateRunning, container.StateRestarting, container.StatePaused:
		return true
	default:
		return false
	}
}

// createContainerWithConflictRetry calls ContainerCreate and, on a 409 name-conflict error
// (belt-and-suspenders against a race between the preflight check and this call - e.g. another
// process created the same deterministically-named container in between), re-resolves the
// conflicting container via resolve, force-removes it if found, and retries creation exactly
// once. If the retry also fails, the error is returned as-is.
func (c *Client) createContainerWithConflictRetry(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkConfig *network.NetworkingConfig, containerName string, resolve func() (*container.Summary, error)) (string, error) {
	resp, err := c.docker.ContainerCreate(ctx, config, hostConfig, networkConfig, nil, containerName)
	if err == nil {
		return resp.ID, nil
	}
	if !isConflictError(err) {
		return "", err
	}

	c.log.Warn("Container name %q conflicted on create (%v), resolving and removing stale container before retrying once", containerName, err)

	if existing, resolveErr := resolve(); resolveErr == nil {
		if rmErr := c.docker.ContainerRemove(ctx, existing.ID, container.RemoveOptions{Force: true}); rmErr != nil && !errdefs.IsNotFound(rmErr) {
			return "", fmt.Errorf("container name %q already in use and stale container %s could not be removed: %w (original conflict: %v)", containerName, existing.ID, rmErr, err)
		}
	} else if !errors.Is(resolveErr, ErrContainerNotResolved) {
		c.log.Warn("Failed to resolve conflicting container %q after 409: %v", containerName, resolveErr)
	}

	resp, retryErr := c.docker.ContainerCreate(ctx, config, hostConfig, networkConfig, nil, containerName)
	if retryErr != nil {
		return "", retryErr
	}
	return resp.ID, nil
}

// ErrContainerNotResolved is returned by Resolve* methods when a container cannot be
// found by label or by the deterministic name convention. It signals "genuinely gone"
// (as opposed to a transient Docker API error) so callers can safely fall through to
// creating a new container.
var ErrContainerNotResolved = fmt.Errorf("no matching docker container found")

// ResolveContainer attempts to deterministically re-find the Docker container belonging
// to the given server ID, independent of any (possibly empty or stale) stored container ID.
//
// Resolution order:
//  1. ContainerList filtered by the carbon-panel.server.id label (includes stopped containers).
//  2. Fallback: ContainerInspect on the canonical deterministic name "carbon-panel-server-<id>",
//     in case the container exists but was created before labels were consistently applied,
//     or the label was somehow stripped/lost.
//
// Returns ErrContainerNotResolved (wrapped) if neither strategy finds a container. Any other
// error is a transient/unexpected Docker API failure and should NOT be treated as "not found".
func (c *Client) ResolveContainer(ctx context.Context, serverID string) (*container.Summary, error) {
	if serverID == "" {
		return nil, fmt.Errorf("resolve container: server id is empty")
	}

	// Strategy 1: label match
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", fmt.Sprintf("carbon-panel.server.id=%s", serverID))

	containers, err := c.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve container: label lookup failed: %w", err)
	}
	if len(containers) > 0 {
		return &containers[0], nil
	}

	// Strategy 2: deterministic name fallback
	name := fmt.Sprintf("carbon-panel-server-%s", serverID)
	inspect, err := c.docker.ContainerInspect(ctx, name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, fmt.Errorf("%w: server %s", ErrContainerNotResolved, serverID)
		}
		return nil, fmt.Errorf("resolve container: name lookup failed: %w", err)
	}

	return inspectToSummary(inspect), nil
}

// ResolveModuleContainer is the module-scoped analogue of ResolveContainer. It re-finds the
// Docker container belonging to a module ID via the carbon-panel.module.id label, falling back
// to the deterministic name "carbon-panel-module-<id>".
func (c *Client) ResolveModuleContainer(ctx context.Context, moduleID string) (*container.Summary, error) {
	if moduleID == "" {
		return nil, fmt.Errorf("resolve module container: module id is empty")
	}

	// Strategy 1: label match
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", fmt.Sprintf("carbon-panel.module.id=%s", moduleID))

	containers, err := c.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve module container: label lookup failed: %w", err)
	}
	if len(containers) > 0 {
		return &containers[0], nil
	}

	// Strategy 2: deterministic name fallback
	name := fmt.Sprintf("carbon-panel-module-%s", moduleID)
	inspect, err := c.docker.ContainerInspect(ctx, name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, fmt.Errorf("%w: module %s", ErrContainerNotResolved, moduleID)
		}
		return nil, fmt.Errorf("resolve module container: name lookup failed: %w", err)
	}

	return inspectToSummary(inspect), nil
}

// inspectToSummary adapts a ContainerInspect result into the same container.Summary shape
// returned by ContainerList, so callers of Resolve* don't need to handle two different types.
func inspectToSummary(inspect container.InspectResponse) *container.Summary {
	summary := &container.Summary{
		ID:      inspect.ID,
		Image:   inspect.Image,
		Command: "",
		Labels:  map[string]string{},
	}
	if inspect.Name != "" {
		summary.Names = []string{inspect.Name}
	}
	if inspect.Config != nil {
		summary.Image = inspect.Config.Image
		if inspect.Config.Labels != nil {
			summary.Labels = inspect.Config.Labels
		}
	}
	if inspect.State != nil {
		summary.State = container.ContainerState(inspect.State.Status)
		summary.Status = inspect.State.Status
	}
	// Carry the per-network endpoint state across too. Callers that need a
	// container's address (e.g. the reconciler's route-drift check) get the same
	// view whether the container was found by label or by name fallback.
	if inspect.NetworkSettings != nil {
		summary.NetworkSettings = &container.NetworkSettingsSummary{
			Networks: inspect.NetworkSettings.Networks,
		}
	}
	return summary
}

func (c *Client) StartContainer(ctx context.Context, containerID string) error {
	if err := c.docker.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return err
	}

	// Start log streaming if configured
	if c.logStreamer != nil {
		if err := c.logStreamer.StartStreaming(containerID); err != nil {
			c.log.Warn("Failed to start log streaming for container %s: %v", containerID, err)
		}
	}

	return nil
}

// StopContainer stops a container. Returns (containerFound, error).
// If container doesn't exist, returns (false, nil) so caller can clean up stale references.
func (c *Client) StopContainer(ctx context.Context, containerID string) (bool, error) {
	// Stop log streaming before stopping container
	if c.logStreamer != nil {
		c.logStreamer.StopStreaming(containerID)
	}

	// First try graceful stop with a short timeout
	timeout := 5 // seconds
	err := c.docker.ContainerStop(ctx, containerID, container.StopOptions{
		Timeout: &timeout,
	})

	if err != nil {
		// If container non-existent on stop
		if errdefs.IsNotFound(err) {
			c.log.Debug("Container %s not found, treating as already stopped", containerID)
			return false, nil
		}
		// If graceful stop fails, force kill the container
		c.log.Warn("Graceful stop failed for container %s: %v, attempting force kill", containerID, err)
		killErr := c.docker.ContainerKill(ctx, containerID, "KILL")
		if killErr != nil {
			// If container non-existent on kill
			if errdefs.IsNotFound(killErr) {
				return false, nil
			}
			return false, fmt.Errorf("failed to stop container: graceful stop error: %v, force kill error: %v", err, killErr)
		}
	}

	return true, nil
}

func (c *Client) RemoveContainer(ctx context.Context, containerID string) error {
	if c.logStreamer != nil {
		c.logStreamer.RemoveContainer(containerID)
	}
	return c.docker.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force: true,
	})
}

// PauseContainer pauses all processes in the container using cgroup freezer (MINE-18)
func (c *Client) PauseContainer(ctx context.Context, containerID string) error {
	if err := c.docker.ContainerPause(ctx, containerID); err != nil {
		return fmt.Errorf("failed to pause container %s: %w", containerID, err)
	}
	c.log.Info("Paused container %s via cgroup freezer", containerID)
	return nil
}

// UnpauseContainer resumes all processes in the container from cgroup freezer (MINE-18)
func (c *Client) UnpauseContainer(ctx context.Context, containerID string) error {
	if err := c.docker.ContainerUnpause(ctx, containerID); err != nil {
		return fmt.Errorf("failed to unpause container %s: %w", containerID, err)
	}
	c.log.Info("Unpaused container %s from cgroup freezer", containerID)
	return nil
}

// Stops and starts a container with an optional delay between operations
func (c *Client) RestartContainer(ctx context.Context, containerID string, delay time.Duration) error {
	if _, err := c.StopContainer(ctx, containerID); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	if delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	if err := c.StartContainer(ctx, containerID); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	return nil
}

// Result of a container recreation operation
type RecreateContainerResult struct {
	NewContainerID string
	WasRunning     bool
}

// Stops, removes, and creates a new container - Returns new container ID and whether it was running before
func (c *Client) RecreateContainer(ctx context.Context, oldContainerID string, server *models.Server, serverConfig *models.ServerConfig) (*RecreateContainerResult, error) {
	result := &RecreateContainerResult{}

	// Check if container was running before we stop it
	if oldContainerID != "" {
		status, err := c.GetContainerStatus(ctx, oldContainerID)
		if err != nil {
			// Container may not exist, that's ok - continue with creation
			c.log.Debug("Container %s not found during recreation: %v", oldContainerID, err)
		} else if status == models.StatusRunning || status == models.StatusUnhealthy {
			result.WasRunning = true
			if _, err := c.StopContainer(ctx, oldContainerID); err != nil {
				return nil, fmt.Errorf("failed to stop container: %w", err)
			}
		}

		// Remove old container directly from Docker to preserve log subscribers for migration
		if err := c.docker.ContainerRemove(ctx, oldContainerID, container.RemoveOptions{Force: true}); err != nil {
			// Log but continue - container may already be removed
			c.log.Debug("Could not remove old container (may not exist): %v", err)
		}
	}

	// Create new container
	newContainerID, err := c.CreateContainer(ctx, server, serverConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}
	result.NewContainerID = newContainerID

	// Migrate log subscribers from old to new container
	if c.logStreamer != nil && oldContainerID != "" {
		c.logStreamer.MigrateSubscribers(oldContainerID, newContainerID)
	}

	// Start if it was running before
	if result.WasRunning {
		if err := c.StartContainer(ctx, newContainerID); err != nil {
			return result, fmt.Errorf("failed to start new container: %w", err)
		}
	}

	return result, nil
}

// ContainerState is the consolidated, reconciler-relevant view of a container's
// runtime state. It captures the mapped ServerStatus plus the exit code and OOM
// flag that are only available from a full ContainerInspect, in a single Docker
// round-trip. MINE-107's level-triggered reconciler uses this to converge DB
// state with observed reality without inspecting twice.
type ContainerState struct {
	ContainerID string
	Status      models.ServerStatus
	Running     bool
	// ExitCode is the container process's last exit code. Only meaningful when
	// HasExited is true.
	ExitCode int
	// HasExited reports whether the container is not currently running, i.e.
	// ExitCode reflects a completed process rather than a live one.
	HasExited bool
	// OOMKilled reports whether Docker recorded the last exit as an OOM kill.
	OOMKilled bool
}

// mapContainerState translates a Docker container.State into the panel's
// ServerStatus vocabulary. It is the single source of truth shared by
// GetContainerStatus and ObserveContainer so the two can never drift apart.
func mapContainerState(state *container.State) models.ServerStatus {
	if state == nil {
		return models.StatusError
	}

	switch state.Status {
	case "running":
		// Check health status if available
		if state.Health != nil {
			switch state.Health.Status {
			case "healthy":
				return models.StatusRunning
			case "starting":
				return models.StatusStarting
			case "unhealthy":
				// Server process isn't responding
				return models.StatusUnhealthy
			default:
				// No health status or unknown, assume running
				return models.StatusRunning
			}
		}
		return models.StatusRunning
	case "restarting":
		return models.StatusStarting
	case "exited", "dead":
		return models.StatusStopped
	case "created", "removing":
		return models.StatusStopped
	case "paused":
		return models.StatusPaused
	default:
		return models.StatusError
	}
}

func (c *Client) GetContainerStatus(ctx context.Context, containerID string) (models.ServerStatus, error) {
	inspect, err := c.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return models.StatusError, err
	}

	return mapContainerState(inspect.State), nil
}

// ObserveContainer inspects a container once and returns its consolidated
// runtime state. A container that does not exist is reported by the returned
// error (errdefs.IsNotFound); callers expecting it to exist should treat that
// as "container missing" rather than a transient failure.
func (c *Client) ObserveContainer(ctx context.Context, containerID string) (*ContainerState, error) {
	inspect, err := c.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, err
	}

	st := &ContainerState{
		ContainerID: inspect.ID,
		Status:      mapContainerState(inspect.State),
	}
	if st.ContainerID == "" {
		st.ContainerID = containerID
	}
	if inspect.State != nil {
		st.Running = inspect.State.Running
		st.HasExited = !inspect.State.Running
		st.ExitCode = inspect.State.ExitCode
		st.OOMKilled = inspect.State.OOMKilled
	}

	return st, nil
}

func (c *Client) GetContainerStats(ctx context.Context, containerID string) (*ContainerStats, error) {
	// Get real-time stats
	statsResponse, err := c.docker.ContainerStats(ctx, containerID, false)
	if err != nil {
		return nil, err
	}
	defer statsResponse.Body.Close()

	var stats container.StatsResponse
	if err := json.NewDecoder(statsResponse.Body).Decode(&stats); err != nil {
		return nil, err
	}

	// Calculate CPU percentage (ns)
	cpuPercent := 0.0
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)

	// Number of CPU cores
	cpuCount := float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
	if cpuCount == 0 {
		cpuCount = float64(stats.CPUStats.OnlineCPUs)
	}
	if cpuCount == 0 {
		cpuCount = 1.0
	}

	if systemDelta > 0.0 && cpuDelta > 0.0 {
		cpuPercent = (cpuDelta / systemDelta) * cpuCount * 100.0
	}

	// Get memory usage in MB (excluding cache)
	memoryUsage := float64(stats.MemoryStats.Usage-stats.MemoryStats.Stats["cache"]) / 1024 / 1024
	memoryLimit := float64(stats.MemoryStats.Limit) / 1024 / 1024

	return &ContainerStats{
		CPUPercent:  cpuPercent,
		MemoryUsage: memoryUsage,
		MemoryLimit: memoryLimit,
	}, nil
}

// Runs shell command, script, or executable inside the container and returns the output
func (c *Client) Exec(ctx context.Context, containerID string, execCmd []string) (string, error) {
	// Create exec configuration
	execConfig := container.ExecOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
		Cmd:          execCmd, //[]string{"rcon-cli", command},
	}

	// Create exec instance
	execResp, err := c.docker.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create exec: %w", err)
	}

	// Attach to exec instance
	attachResp, err := c.docker.ContainerExecAttach(ctx, execResp.ID, container.ExecStartOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attachResp.Close()

	// Read output using stdcopy to demultiplex the stream
	var outputBuf bytes.Buffer
	_, err = stdcopy.StdCopy(&outputBuf, &outputBuf, attachResp.Reader)
	if err != nil {
		return "", fmt.Errorf("failed to read exec output: %w", err)
	}

	// Check exec exit code
	inspectResp, err := c.docker.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect exec: %w", err)
	}

	if inspectResp.ExitCode != 0 {
		return "", fmt.Errorf("command failed with exit code %d: %s", inspectResp.ExitCode, outputBuf.String())
	}

	return outputBuf.String(), nil
}

// ExecCommand executes a command inside the container and returns the output
func (c *Client) ExecCommand(ctx context.Context, containerID string, command string) (string, error) {
	return c.Exec(ctx, containerID, []string{"rcon-cli", command})
}

func (c *Client) pullImage(ctx context.Context, imageName string) error {
	reader, err := c.docker.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w", imageName, err)
	}
	defer reader.Close()

	// Read the output to ensure the pull completes
	_, err = io.Copy(io.Discard, reader)
	if err != nil {
		return fmt.Errorf("failed to complete image pull for %s: %w", imageName, err)
	}

	return nil
}

func (c *Client) GetDockerImages() []DockerImageTag {
	images, err := fetchDockerImages()
	if err != nil {
		c.log.Error("Failed to fetch docker images: %v", err)
		return []DockerImageTag{}
	}

	// Filter out deprecated and dedup
	seen := make(map[string]bool)
	var activeImages []DockerImageTag
	for _, img := range images {
		if !img.Deprecated && !seen[img.Tag] {
			seen[img.Tag] = true
			activeImages = append(activeImages, img)
		}
	}
	return activeImages
}

func getDockerImage(loader models.ModLoader, mcVersion string) string {
	_ = loader
	// itzg/minecraft-server supports all mod loaders through environment variables
	// We use Java version specific tags for better compatibility
	return "itzg/minecraft-server:" + GetOptimalDockerTag(mcVersion, loader, false)
}

// Creates the Docker network if it doesn't exist - attaches itself to that network when applicable
func (c *Client) EnsureNetwork() error {
	ctx := context.Background()

	// List existing networks
	networks, err := c.docker.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list networks: %w", err)
	}

	// Check if network already exists
	exists := false
	for _, net := range networks {
		if net.Name == c.config.NetworkName {
			exists = true
			break
		}
	}

	if !exists {
		// Create network - let Docker allocate subnet from its configured default-address-pools
		createOpts := network.CreateOptions{
			Driver: "bridge",
			Labels: map[string]string{
				"carbon-panel.managed": "true",
			},
		}

		if _, err = c.docker.NetworkCreate(ctx, c.config.NetworkName, createOpts); err != nil {
			return fmt.Errorf("failed to create network: %w", err)
		}
	}

	c.attachSelfToNetwork(ctx)
	return nil
}

// Connects Carbon Panel to its own bridge network if running as container
// NOTE: Only really needed for bridge mode though
func (c *Client) attachSelfToNetwork(ctx context.Context) {
	if _, err := os.Stat("/.dockerenv"); err != nil {
		return
	}

	hostname, err := os.Hostname()
	if err != nil {
		return
	}

	// Docker sets the container hostname to its short ID by default
	info, err := c.docker.ContainerInspect(ctx, hostname)
	if err != nil {
		c.log.Debug("Could not inspect own container %s: %v", hostname, err)
		return
	}

	if info.HostConfig != nil && info.HostConfig.NetworkMode.IsHost() {
		return
	}

	if _, ok := info.NetworkSettings.Networks[c.config.NetworkName]; ok {
		return
	}

	if err := c.docker.NetworkConnect(ctx, c.config.NetworkName, info.ID, nil); err != nil {
		c.log.Error("Failed to attach Carbon Panel container to network %s: %v", c.config.NetworkName, err)
		return
	}

	c.log.Info("Attached Carbon Panel container to network %s", c.config.NetworkName)
}

var (
	cfSlugRegex = regexp.MustCompile(`/(?:minecraft/(?:modpacks|mc-mods|customization|worlds|texture-packs)|projects)/([a-zA-Z0-9_\-]+)`)
	cfFileRegex = regexp.MustCompile(`/files/(\d+)`)
)

func extractCurseForgeSlug(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	matches := cfSlugRegex.FindStringSubmatch(raw)
	if len(matches) > 1 {
		return matches[1]
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") && !strings.Contains(raw, "/") {
		return raw
	}
	return ""
}

func extractCurseForgeFileID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	matches := cfFileRegex.FindStringSubmatch(raw)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// Builds Docker environment variables from ServerConfig struct
func buildEnvFromConfig(config *models.ServerConfig) []string {
	// Auto-assume CurseForge slug and file ID if CFPageURL is present
	if config.CFPageURL != nil && *config.CFPageURL != "" {
		if config.CFSlug == nil || *config.CFSlug == "" {
			if slug := extractCurseForgeSlug(*config.CFPageURL); slug != "" {
				config.CFSlug = &slug
			}
		}
		if config.CFFileID == nil || *config.CFFileID == "" {
			if fileID := extractCurseForgeFileID(*config.CFPageURL); fileID != "" {
				config.CFFileID = &fileID
			}
		}
	}
	// Sensible default for CFParallelDownloads if not explicitly set
	if config.CFParallelDownloads == nil {
		p := 4
		config.CFParallelDownloads = &p
	}

	env := []string{
		"DUMP_SERVER_PROPERTIES=true",
	}

	configValue := reflect.ValueOf(config).Elem()
	configType := configValue.Type()

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		envTag := field.Tag.Get("env")

		// Skip fields without env tags
		if envTag == "" || envTag == "-" {
			continue
		}

		fieldValue := configValue.Field(i)

		// Handle pointer types
		if fieldValue.Kind() == reflect.Pointer {
			// Skip if nil
			if fieldValue.IsNil() {
				continue
			}
			// Dereference the pointer
			fieldValue = fieldValue.Elem()
		}

		// Handle different field types
		switch fieldValue.Kind() {
		case reflect.String:
			if str := fieldValue.String(); str != "" {
				env = append(env, fmt.Sprintf("%s=%s", envTag, str))
			}
		case reflect.Int, reflect.Int32, reflect.Int64:
			// Always include int values (even 0) when the field is explicitly set
			env = append(env, fmt.Sprintf("%s=%d", envTag, fieldValue.Int()))
		case reflect.Bool:
			// Always include bool values when the field is explicitly set
			env = append(env, fmt.Sprintf("%s=%v", envTag, fieldValue.Bool()))
		}
	}

	// MINE-3: Generational ZGC (Java 21+) handling vs Aikar G1GC flags
	if config.UseGenerationalZgc != nil && *config.UseGenerationalZgc {
		// Override USE_AIKAR_FLAGS to false to prevent conflicting G1GC flags
		for i, e := range env {
			if strings.HasPrefix(e, "USE_AIKAR_FLAGS=") {
				env[i] = "USE_AIKAR_FLAGS=false"
			}
		}
		// Inject Generational ZGC flags into JVM_XX_OPTS
		zgcFlags := "-XX:+UseZGC -XX:+ZGenerational"
		hasXxOpts := false
		for i, e := range env {
			if strings.HasPrefix(e, "JVM_XX_OPTS=") {
				val := strings.TrimPrefix(e, "JVM_XX_OPTS=")
				if !strings.Contains(val, "UseZGC") {
					if val != "" {
						env[i] = fmt.Sprintf("JVM_XX_OPTS=%s %s", val, zgcFlags)
					} else {
						env[i] = fmt.Sprintf("JVM_XX_OPTS=%s", zgcFlags)
					}
				}
				hasXxOpts = true
				break
			}
		}
		if !hasXxOpts {
			env = append(env, fmt.Sprintf("JVM_XX_OPTS=%s", zgcFlags))
		}
	}

	return env
}

// DetectContainerJavaVersion attempts to auto-detect the Java runtime version from a running container
func (c *Client) DetectContainerJavaVersion(ctx context.Context, containerID string) (int, error) {
	inspect, err := c.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return 0, err
	}
	for _, e := range inspect.Config.Env {
		if strings.HasPrefix(e, "JAVA_VERSION=") {
			vStr := strings.TrimPrefix(e, "JAVA_VERSION=")
			parts := strings.Split(vStr, ".")
			if len(parts) > 0 {
				if v, err := strconv.Atoi(parts[0]); err == nil {
					return v, nil
				}
			}
		}
	}
	// Fall back to inspecting image tag
	image := inspect.Config.Image
	if strings.Contains(image, "java21") {
		return 21, nil
	} else if strings.Contains(image, "java17") {
		return 17, nil
	} else if strings.Contains(image, "java8") {
		return 8, nil
	}
	return 21, nil
}
