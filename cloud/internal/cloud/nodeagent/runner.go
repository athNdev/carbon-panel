package nodeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/athNdev/carbon-panel/internal/minecraft"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"net/netip"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Runner manages the execution of containerized Minecraft workloads.
type Runner interface {
	Assign(ctx context.Context, w *v1.Workload, start bool) (containerID string, assignedPort int32, err error)
	Stop(ctx context.Context, workloadID string, timeoutSec int) error
	Delete(ctx context.Context, workloadID string, deleteData bool) error
	RunCommand(ctx context.Context, workloadID, command string) (string, error)
	Logs(ctx context.Context, workloadID string, tail int, follow bool) (io.ReadCloser, error)
	GetMetrics(ctx context.Context, workloadID string) (*v1.WorkloadMetrics, error)
	ListActiveWorkloadIDs(ctx context.Context) ([]string, error)
	Hibernate(ctx context.Context, workloadID, mode string) error
	Wake(ctx context.Context, workloadID string) error
}

// DockerRunner interacts directly with Docker Engine on the local node.
type diskCacheEntry struct {
	size      int64
	updatedAt time.Time
}

// DockerRunner interacts directly with Docker Engine on the local node.
type DockerRunner struct {
	cli       *client.Client
	dataDir   string
	logger    *slog.Logger
	diskMu    sync.RWMutex
	diskCache map[string]diskCacheEntry
}

// NewDockerRunner creates a new Docker runner using local environment connection.
func NewDockerRunner(dataDir string, logger *slog.Logger) (*DockerRunner, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &DockerRunner{
		cli:     cli,
		dataDir: dataDir,
		logger:  logger,
	}, nil
}

// Assign creates or adopts a Minecraft container and starts it if requested.
func (r *DockerRunner) Assign(ctx context.Context, w *v1.Workload, start bool) (string, int32, error) {
	if w == nil || w.Id == "" {
		return "", 0, fmt.Errorf("invalid workload: nil or empty ID")
	}

	containerName := "carbon-workload-" + w.Id

	// Check if container already exists
	mcPort := network.MustParsePort("25565/tcp")
	inspectRes, err := r.cli.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	if err == nil {
		inspect := inspectRes.Container
		status := ""
		running := false
		if inspect.State != nil {
			status = string(inspect.State.Status)
			running = inspect.State.Running
		}
		r.logger.Info("workload container already exists", "workload_id", w.Id, "container_id", inspect.ID, "status", status)
		if start && !running {
			if _, startErr := r.cli.ContainerStart(ctx, inspect.ID, client.ContainerStartOptions{}); startErr != nil {
				return inspect.ID, 0, fmt.Errorf("start existing container: %w", startErr)
			}
		}
		var port int32
		if inspect.NetworkSettings != nil && inspect.NetworkSettings.Ports != nil {
			if bindings, ok := inspect.NetworkSettings.Ports[mcPort]; ok && len(bindings) > 0 {
				var p int
				if _, err := fmt.Sscanf(bindings[0].HostPort, "%d", &p); err == nil {
					port = int32(p)
				}
			}
		}
		return inspect.ID, port, nil
	}

	// Prepare data directory on host
	hostDataDir := filepath.Join(r.dataDir, "workloads", w.Id, "data")
	if err := os.MkdirAll(hostDataDir, 0755); err != nil {
		return "", 0, fmt.Errorf("create workload data dir: %w", err)
	}

	loader := "PAPER"
	version := "1.21.4"
	var memoryMb int64 = 2048
	var cpuMillicores int64 = 1000

	if w.Spec != nil {
		if l := strings.ToUpper(strings.TrimSpace(w.Spec.Loader)); l != "" {
			loader = l
		}
		if v := strings.TrimSpace(w.Spec.MinecraftVersion); v != "" {
			version = v
		}
		if w.Spec.MemoryMb > 0 {
			memoryMb = w.Spec.MemoryMb
		}
		if w.Spec.CpuMillicores > 0 {
			cpuMillicores = w.Spec.CpuMillicores
		}
	}

	env := []string{
		"EULA=TRUE",
		fmt.Sprintf("TYPE=%s", loader),
		fmt.Sprintf("VERSION=%s", version),
		fmt.Sprintf("MEMORY=%dM", memoryMb),
	}
	if w.Spec != nil {
		for k, v := range w.Spec.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		if len(w.Spec.JvmFlags) > 0 {
			env = append(env, fmt.Sprintf("JVM_OPTS=%s", strings.Join(w.Spec.JvmFlags, " ")))
		}
	}

	hostPortStr := "25565"
	if w.HostPort > 0 {
		hostPortStr = fmt.Sprintf("%d", w.HostPort)
	} else if !isPortAvailable(25565) {
		hostPortStr = "0" // allocate dynamic ephemeral port if 25565 is occupied
	}

	portBindings := network.PortMap{
		mcPort: []network.PortBinding{
			{
				HostIP:   netip.MustParseAddr("0.0.0.0"),
				HostPort: hostPortStr,
			},
		},
	}

	imageName := "itzg/minecraft-server:latest"
	if w.Spec != nil && w.Spec.Env != nil && w.Spec.Env["DOCKER_IMAGE"] != "" {
		imageName = w.Spec.Env["DOCKER_IMAGE"]
	}

	cfg := &container.Config{
		Image: imageName,
		Env:   env,
		Labels: map[string]string{
			"carbon.workload.id":   w.Id,
			"carbon.workload.name": w.Name,
			"carbon.org.id":        w.OrgId,
		},
		ExposedPorts: network.PortSet{
			mcPort: struct{}{},
		},
	}

	hostCfg := &container.HostConfig{
		Binds:        []string{fmt.Sprintf("%s:/data", hostDataDir)},
		PortBindings: portBindings,
		RestartPolicy: container.RestartPolicy{
			Name: "unless-stopped",
		},
		Resources: container.Resources{
			Memory:   memoryMb * 1024 * 1024,
			NanoCPUs: cpuMillicores * 1_000_000,
		},
	}

	resp, err := r.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     cfg,
		HostConfig: hostCfg,
		Name:       containerName,
	})
	if err != nil {
		return "", 0, fmt.Errorf("create container: %w", err)
	}

	r.logger.Info("created workload container", "workload_id", w.Id, "container_id", resp.ID)

	if start {
		if _, err := r.cli.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
			return resp.ID, 0, fmt.Errorf("start container: %w", err)
		}
		r.logger.Info("started workload container", "workload_id", w.Id, "container_id", resp.ID)
	}

	var assignedPort int32
	if inspRes, err := r.cli.ContainerInspect(ctx, resp.ID, client.ContainerInspectOptions{}); err == nil {
		insp := inspRes.Container
		if insp.NetworkSettings != nil && insp.NetworkSettings.Ports != nil {
			if bindings, ok := insp.NetworkSettings.Ports[mcPort]; ok && len(bindings) > 0 {
				var p int
				if _, err := fmt.Sscanf(bindings[0].HostPort, "%d", &p); err == nil {
					assignedPort = int32(p)
				}
			}
		}
	}

	return resp.ID, assignedPort, nil
}

// Stop halts the workload's container gracefully.
func (r *DockerRunner) Stop(ctx context.Context, workloadID string, timeoutSec int) error {
	containerName := "carbon-workload-" + workloadID
	if timeoutSec <= 0 {
		timeoutSec = 15
	}
	_, err := r.cli.ContainerStop(ctx, containerName, client.ContainerStopOptions{Timeout: &timeoutSec})
	return err
}

// Delete removes the container and optionally scrubs the on-disk directory.
func (r *DockerRunner) Delete(ctx context.Context, workloadID string, deleteData bool) error {
	containerName := "carbon-workload-" + workloadID
	_, _ = r.cli.ContainerRemove(ctx, containerName, client.ContainerRemoveOptions{Force: true})
	if deleteData {
		hostDataDir := filepath.Join(r.dataDir, "workloads", workloadID)
		_ = os.RemoveAll(hostDataDir)
	}
	return nil
}

// Hibernate pauses or deep-sleeps the workload container.
func (r *DockerRunner) Hibernate(ctx context.Context, workloadID, mode string) error {
	containerName := "carbon-workload-" + workloadID
	inspectRes, err := r.cli.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspect container for hibernate: %w", err)
	}
	inspect := inspectRes.Container

	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "deep_sleep" || mode == "stop" {
		if inspect.State != nil && inspect.State.Running {
			timeout := 15
			_, err := r.cli.ContainerStop(ctx, containerName, client.ContainerStopOptions{Timeout: &timeout})
			return err
		}
		return nil
	}

	// Default: pause (cgroup freeze)
	if inspect.State != nil && inspect.State.Paused {
		return nil
	}
	if inspect.State == nil || !inspect.State.Running {
		status := ""
		if inspect.State != nil {
			status = string(inspect.State.Status)
		}
		return fmt.Errorf("container is not running (status: %s)", status)
	}
	_, err = r.cli.ContainerPause(ctx, containerName, client.ContainerPauseOptions{})
	return err
}

// Wake unpauses or starts a hibernated workload container.
func (r *DockerRunner) Wake(ctx context.Context, workloadID string) error {
	containerName := "carbon-workload-" + workloadID
	inspectRes, err := r.cli.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspect container for wake: %w", err)
	}
	inspect := inspectRes.Container

	if inspect.State != nil && inspect.State.Paused {
		_, err := r.cli.ContainerUnpause(ctx, containerName, client.ContainerUnpauseOptions{})
		return err
	}
	if inspect.State != nil && !inspect.State.Running {
		_, err := r.cli.ContainerStart(ctx, inspect.ID, client.ContainerStartOptions{})
		return err
	}
	return nil
}

// RunCommand executes a command via rcon-cli inside the workload container.
func (r *DockerRunner) RunCommand(ctx context.Context, workloadID, command string) (string, error) {
	containerName := "carbon-workload-" + workloadID
	execResp, err := r.cli.ExecCreate(ctx, containerName, client.ExecCreateOptions{
		Cmd:          []string{"rcon-cli", command},
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return "", fmt.Errorf("create exec: %w", err)
	}

	attachResp, err := r.cli.ExecAttach(ctx, execResp.ID, client.ExecAttachOptions{})
	if err != nil {
		return "", fmt.Errorf("attach exec: %w", err)
	}
	defer attachResp.Close()

	var stdout, stderr bytes.Buffer
	_, err = stdcopy.StdCopy(&stdout, &stderr, attachResp.Reader)
	if err != nil {
		raw, _ := io.ReadAll(attachResp.Reader)
		stdout.Write(raw)
	}

	insp, err := r.cli.ExecInspect(ctx, execResp.ID, client.ExecInspectOptions{})
	out := strings.TrimSpace(stdout.String())
	errOut := strings.TrimSpace(stderr.String())
	if err == nil && insp.ExitCode != 0 {
		if errOut != "" {
			return out, fmt.Errorf("exit code %d: %s", insp.ExitCode, errOut)
		}
		return out, fmt.Errorf("exit code %d", insp.ExitCode)
	}

	if out == "" && errOut != "" {
		out = errOut
	}
	return out, nil
}

// Logs returns an io.ReadCloser streaming container logs multiplexed via Docker.
func (r *DockerRunner) Logs(ctx context.Context, workloadID string, tail int, follow bool) (io.ReadCloser, error) {
	containerName := "carbon-workload-" + workloadID
	tailStr := "100"
	if tail > 0 {
		tailStr = fmt.Sprintf("%d", tail)
	}
	opts := client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tailStr,
	}
	res, err := r.cli.ContainerLogs(ctx, containerName, opts)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ListActiveWorkloadIDs returns all workload IDs that currently exist as Docker containers on this node.
func (r *DockerRunner) ListActiveWorkloadIDs(ctx context.Context) ([]string, error) {
	res, err := r.cli.ContainerList(ctx, client.ContainerListOptions{
		Filters: client.Filters{}.Add("label", "carbon.workload.id"),
	})
	if err != nil {
		return nil, fmt.Errorf("list workload containers: %w", err)
	}
	var ids []string
	for _, c := range res.Items {
		if id, ok := c.Labels["carbon.workload.id"]; ok && id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (r *DockerRunner) getWorkloadDiskUsage(workloadID string) int64 {
	r.diskMu.RLock()
	if r.diskCache != nil {
		if entry, ok := r.diskCache[workloadID]; ok && time.Since(entry.updatedAt) < 60*time.Second {
			r.diskMu.RUnlock()
			return entry.size
		}
	}
	r.diskMu.RUnlock()

	dataDir := filepath.Join(r.dataDir, "workloads", workloadID, "data")
	var total int64
	_ = filepath.Walk(dataDir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})

	r.diskMu.Lock()
	if r.diskCache == nil {
		r.diskCache = make(map[string]diskCacheEntry)
	}
	r.diskCache[workloadID] = diskCacheEntry{
		size:      total,
		updatedAt: time.Now(),
	}
	r.diskMu.Unlock()
	return total
}

// GetMetrics samples real-time container metrics (CPU, RAM, network, disk, players, TPS).
func (r *DockerRunner) GetMetrics(ctx context.Context, workloadID string) (*v1.WorkloadMetrics, error) {
	containerName := "carbon-workload-" + workloadID

	statsResp, err := r.cli.ContainerStats(ctx, containerName, client.ContainerStatsOptions{Stream: false})
	if err != nil {
		return nil, fmt.Errorf("container stats: %w", err)
	}
	defer statsResp.Body.Close()

	var stats container.StatsResponse
	if err := json.NewDecoder(statsResp.Body).Decode(&stats); err != nil {
		return nil, fmt.Errorf("decode container stats: %w", err)
	}

	// Calculate CPU percentage
	cpuPercent := 0.0
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)
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

	// Calculate Memory in MB (deduct cache/inactive file)
	cache := stats.MemoryStats.Stats["cache"]
	if cache == 0 {
		cache = stats.MemoryStats.Stats["inactive_file"]
	}
	usedBytes := stats.MemoryStats.Usage
	if usedBytes > cache {
		usedBytes -= cache
	}
	memUsedMb := float64(usedBytes) / 1024.0 / 1024.0
	memLimitMb := float64(stats.MemoryStats.Limit) / 1024.0 / 1024.0

	// Network I/O
	var rxBytes, txBytes int64
	for _, netStats := range stats.Networks {
		rxBytes += int64(netStats.RxBytes)
		txBytes += int64(netStats.TxBytes)
	}

	// Disk usage
	diskUsed := r.getWorkloadDiskUsage(workloadID)

	// Minecraft RCON queries (best-effort, non-blocking)
	var playersOnline int32
	var maxPlayers int32 = 20
	var playerSample []string
	var tps float64 = 20.0

	rconCtx, rconCancel := context.WithTimeout(ctx, 2*time.Second)
	defer rconCancel()

	if listOut, err := r.RunCommand(rconCtx, workloadID, "list"); err == nil && listOut != "" {
		count, sample := minecraft.ParsePlayerListFromOutput(listOut)
		playersOnline = int32(count)
		playerSample = sample
	}

	if tpsOut, err := r.RunCommand(rconCtx, workloadID, "tps"); err == nil && tpsOut != "" {
		if parsedTPS := minecraft.ParseTPSFromOutput(tpsOut); parsedTPS > 0 {
			tps = parsedTPS
		}
	}

	return &v1.WorkloadMetrics{
		WorkloadId:     workloadID,
		CpuPercent:     cpuPercent,
		MemoryUsedMb:   memUsedMb,
		MemoryLimitMb:  memLimitMb,
		DiskUsedBytes:  diskUsed,
		NetworkRxBytes: rxBytes,
		NetworkTxBytes: txBytes,
		PlayersOnline:  playersOnline,
		MaxPlayers:     maxPlayers,
		Tps:            tps,
		PlayerSample:   playerSample,
		UpdatedAt:      timestamppb.Now(),
	}, nil
}

// MockRunner is an in-memory test runner that simulates container lifecycle.
type MockRunner struct {
	Containers map[string]string
	LogsOutput string
}

func NewMockRunner() *MockRunner {
	return &MockRunner{Containers: make(map[string]string)}
}

func (m *MockRunner) Assign(ctx context.Context, w *v1.Workload, start bool) (string, int32, error) {
	cid := "mock-container-" + w.Id
	m.Containers[w.Id] = cid
	return cid, 25565, nil
}

func (m *MockRunner) Stop(ctx context.Context, workloadID string, timeoutSec int) error {
	delete(m.Containers, workloadID)
	return nil
}

func (m *MockRunner) Delete(ctx context.Context, workloadID string, deleteData bool) error {
	delete(m.Containers, workloadID)
	return nil
}

func (m *MockRunner) Hibernate(ctx context.Context, workloadID, mode string) error {
	if _, ok := m.Containers[workloadID]; !ok {
		return fmt.Errorf("workload %s not running", workloadID)
	}
	m.Containers[workloadID] = "mock-hibernated"
	return nil
}

func (m *MockRunner) Wake(ctx context.Context, workloadID string) error {
	m.Containers[workloadID] = "mock-container-" + workloadID
	return nil
}

func (m *MockRunner) RunCommand(ctx context.Context, workloadID, command string) (string, error) {
	if _, ok := m.Containers[workloadID]; !ok {
		return "", fmt.Errorf("workload %s not running", workloadID)
	}
	return "mock response for " + command, nil
}

func (m *MockRunner) Logs(ctx context.Context, workloadID string, tail int, follow bool) (io.ReadCloser, error) {
	out := m.LogsOutput
	if out == "" {
		out = "mock server log line\n"
	}
	return io.NopCloser(strings.NewReader(out)), nil
}

func (m *MockRunner) ListActiveWorkloadIDs(ctx context.Context) ([]string, error) {
	var ids []string
	for id := range m.Containers {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *MockRunner) GetMetrics(ctx context.Context, workloadID string) (*v1.WorkloadMetrics, error) {
	return &v1.WorkloadMetrics{
		WorkloadId:     workloadID,
		CpuPercent:     12.5,
		MemoryUsedMb:   512.0,
		MemoryLimitMb:  2048.0,
		DiskUsedBytes:  1048576,
		NetworkRxBytes: 2048,
		NetworkTxBytes: 4096,
		PlayersOnline:  1,
		MaxPlayers:     20,
		Tps:            20.0,
		PlayerSample:   []string{"Steve"},
		UpdatedAt:      timestamppb.Now(),
	}, nil
}

func isPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}
