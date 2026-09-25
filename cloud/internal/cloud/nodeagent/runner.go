package nodeagent

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

// Runner manages the execution of containerized Minecraft workloads.
type Runner interface {
	Assign(ctx context.Context, w *v1.Workload, start bool) (containerID string, assignedPort int32, err error)
	Stop(ctx context.Context, workloadID string, timeoutSec int) error
	Delete(ctx context.Context, workloadID string, deleteData bool) error
}

// DockerRunner interacts directly with Docker Engine on the local node.
type DockerRunner struct {
	cli     *client.Client
	dataDir string
	logger  *slog.Logger
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
	inspect, err := r.cli.ContainerInspect(ctx, containerName)
	if err == nil {
		r.logger.Info("workload container already exists", "workload_id", w.Id, "container_id", inspect.ID, "status", inspect.State.Status)
		if start && !inspect.State.Running {
			if startErr := r.cli.ContainerStart(ctx, inspect.ID, container.StartOptions{}); startErr != nil {
				return inspect.ID, 0, fmt.Errorf("start existing container: %w", startErr)
			}
		}
		var port int32
		if bindings, ok := inspect.NetworkSettings.Ports["25565/tcp"]; ok && len(bindings) > 0 {
			var p int
			if _, err := fmt.Sscanf(bindings[0].HostPort, "%d", &p); err == nil {
				port = int32(p)
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

	portBindings := nat.PortMap{
		"25565/tcp": []nat.PortBinding{
			{
				HostIP:   "0.0.0.0",
				HostPort: hostPortStr,
			},
		},
	}

	cfg := &container.Config{
		Image: "itzg/minecraft-server:latest",
		Env:   env,
		Labels: map[string]string{
			"carbon.workload.id":   w.Id,
			"carbon.workload.name": w.Name,
			"carbon.org.id":        w.OrgId,
		},
		ExposedPorts: nat.PortSet{
			"25565/tcp": struct{}{},
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

	resp, err := r.cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, containerName)
	if err != nil {
		return "", 0, fmt.Errorf("create container: %w", err)
	}

	r.logger.Info("created workload container", "workload_id", w.Id, "container_id", resp.ID)

	if start {
		if err := r.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
			return resp.ID, 0, fmt.Errorf("start container: %w", err)
		}
		r.logger.Info("started workload container", "workload_id", w.Id, "container_id", resp.ID)
	}

	var assignedPort int32
	if insp, err := r.cli.ContainerInspect(ctx, resp.ID); err == nil {
		if bindings, ok := insp.NetworkSettings.Ports["25565/tcp"]; ok && len(bindings) > 0 {
			var p int
			if _, err := fmt.Sscanf(bindings[0].HostPort, "%d", &p); err == nil {
				assignedPort = int32(p)
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
	return r.cli.ContainerStop(ctx, containerName, container.StopOptions{Timeout: &timeoutSec})
}

// Delete removes the container and optionally scrubs the on-disk directory.
func (r *DockerRunner) Delete(ctx context.Context, workloadID string, deleteData bool) error {
	containerName := "carbon-workload-" + workloadID
	_ = r.cli.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})
	if deleteData {
		hostDataDir := filepath.Join(r.dataDir, "workloads", workloadID)
		_ = os.RemoveAll(hostDataDir)
	}
	return nil
}

// MockRunner is an in-memory test runner that simulates container lifecycle.
type MockRunner struct {
	Containers map[string]string
}

func NewMockRunner() *MockRunner {
	return &MockRunner{Containers: make(map[string]string)}
}

func (m *MockRunner) Assign(ctx context.Context, w *v1.Workload, start bool) (string, int32, error) {
	if w == nil || w.Id == "" {
		return "", 0, fmt.Errorf("invalid workload")
	}
	cid := "mock-container-" + w.Id
	m.Containers[w.Id] = cid
	port := int32(25565)
	if w.HostPort > 0 {
		port = w.HostPort
	}
	return cid, port, nil
}

func (m *MockRunner) Stop(ctx context.Context, workloadID string, timeoutSec int) error {
	delete(m.Containers, workloadID)
	return nil
}

func (m *MockRunner) Delete(ctx context.Context, workloadID string, deleteData bool) error {
	delete(m.Containers, workloadID)
	return nil
}

func isPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}
