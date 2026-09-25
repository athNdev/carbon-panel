// Command cloudnoded is the Carbon Cloud node agent binary.
//
// It runs on each managed node, handles registration via join tokens, maintains
// the bidirectional control stream with cloudcontrold, sends periodic heartbeats,
// and serves local health/telemetry endpoints.
//
// Subcommand:
//
//	cloudnoded healthcheck [url]   probe /healthz, exit 0/1 (compose healthcheck)
package main

import (
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/nodeagent"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"


	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/config"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/obs"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/athNdev/carbon-panel/pkg/proto/cloud/v1/cloudv1connect"
)

// Build metadata, injected via ldflags.
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

// PersistedIdentity stores node credentials saved after a successful join.
type PersistedIdentity struct {
	NodeID                   string `json:"node_id"`
	OrgID                    string `json:"org_id"`
	ControlPlaneEndpoint     string `json:"control_plane_endpoint"`
	HeartbeatIntervalSeconds int32  `json:"heartbeat_interval_seconds"`
}

type AgentState struct {
	mu        sync.RWMutex
	identity  *PersistedIdentity
	connected bool
	started   time.Time
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "healthcheck" {
		url := healthcheckURL()
		if len(args) > 1 && strings.TrimSpace(args[1]) != "" {
			url = strings.TrimSpace(args[1])
		}
		if err := probeHealth(url); err != nil {
			_, _ = fmt.Fprintf(stderr, "healthcheck: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprintln(stdout, "ok")
		return 0
	}

	fs := flag.NewFlagSet("cloudnoded", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "path to YAML config file (optional)")
	controlPlaneFlag := fs.String("control-plane-url", "", "control plane endpoint URL")
	joinTokenFlag := fs.String("join-token", "", "single-use node join token")
	dataDirFlag := fs.String("data-dir", "", "data directory for persisting identity and state")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if err := startAgent(*configPath, *controlPlaneFlag, *joinTokenFlag, *dataDirFlag, stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "cloudnoded: %v\n", err)
		return 1
	}
	return 0
}

func healthcheckURL() string {
	addr := strings.TrimSpace(os.Getenv("CARBONCLOUD_SERVER_ADDR"))
	if addr == "" {
		addr = ":8090"
	}
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return "http://127.0.0.1:8090/healthz"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz"
}

func probeHealth(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return err
	}
	if !strings.Contains(string(body), `"ok"`) {
		return fmt.Errorf("GET %s: unhealthy body", url)
	}
	return nil
}

func startAgent(configPath, controlPlaneOverride, joinTokenOverride, dataDirOverride string, stderr io.Writer) error {
	var cfg *config.Config
	var err error
	if configPath != "" {
		cfg, err = config.Load(configPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
	} else {
		cfg = config.Default()
		cfg.Server.Addr = ":8090"
	}

	// Environment variable overrides
	controlPlaneURL := strings.TrimSpace(controlPlaneOverride)
	if controlPlaneURL == "" {
		controlPlaneURL = strings.TrimSpace(os.Getenv("CLOUD_CONTROL_PLANE_URL"))
	}
	if controlPlaneURL == "" {
		controlPlaneURL = strings.TrimSpace(os.Getenv("CARBONCLOUD_CONTROL_PLANE_URL"))
	}
	if controlPlaneURL == "" {
		controlPlaneURL = cfg.Server.PublicURL
	}

	joinToken := strings.TrimSpace(joinTokenOverride)
	if joinToken == "" {
		joinToken = strings.TrimSpace(os.Getenv("CLOUD_JOIN_TOKEN"))
	}
	if joinToken == "" {
		joinToken = strings.TrimSpace(os.Getenv("CARBONCLOUD_JOIN_TOKEN"))
	}

	dataDir := strings.TrimSpace(dataDirOverride)
	if dataDir == "" {
		dataDir = strings.TrimSpace(os.Getenv("CARBONCLOUD_DATA_DIR"))
	}
	if dataDir == "" {
		dataDir = "./data"
	}

	logger := obs.NewLogger(cfg.Telemetry)
	logger.Info("starting cloudnoded", "version", version, "commit", commit, "build_time", buildTime, "addr", cfg.Server.Addr)

	state := &AgentState{
		started: time.Now().UTC(),
	}

	// Attempt to load persisted identity
	identityFile := filepath.Join(dataDir, "identity.json")
	if loaded, err := loadIdentity(identityFile); err == nil && loaded != nil {
		state.identity = loaded
		logger.Info("loaded existing node identity", "node_id", loaded.NodeID, "org_id", loaded.OrgID)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		uptime := time.Since(state.started).Truncate(time.Second).String()
		nodeID := ""
		if state.identity != nil {
			nodeID = state.identity.NodeID
		}
		state.mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"version": version,
			"uptime":  uptime,
			"node_id": nodeID,
		})
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		ready := state.identity != nil && state.connected
		state.mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		if ready {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ready"}`))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"not-ready"}`))
		}
	})

	httpSrv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var runner nodeagent.Runner
	dockerRunner, err := nodeagent.NewDockerRunner(dataDir, logger)
	if err != nil {
		logger.Warn("docker runner unavailable, falling back to mock runner", "err", err)
		runner = nodeagent.NewMockRunner()
	} else {
		runner = dockerRunner
	}

	// Run agent lifecycle loop in background
	go runAgentLoop(ctx, logger, state, runner, controlPlaneURL, joinToken, identityFile)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpSrv.ListenAndServe()
	}()

	select {
	case sig := <-stop:
		logger.Info("shutdown on signal", "signal", sig.String())
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	}

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	return httpSrv.Shutdown(shutCtx)
}

func loadIdentity(path string) (*PersistedIdentity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var id PersistedIdentity
	if err := json.Unmarshal(data, &id); err != nil {
		return nil, err
	}
	return &id, nil
}

func saveIdentity(path string, id *PersistedIdentity) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func detectDockerVersion() string {
	if out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output(); err == nil {
		v := strings.TrimSpace(string(out))
		if v != "" {
			return v
		}
	}
	if out, err := exec.Command("docker", "-v").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return "unknown"
}

func runAgentLoop(ctx context.Context, logger *slog.Logger, state *AgentState, runner nodeagent.Runner, controlPlaneURL, joinToken, identityPath string) {
	if controlPlaneURL == "" {
		logger.Warn("control plane URL is empty; agent loop inactive")
		return
	}

	client := cloudv1connect.NewAgentServiceClient(newAgentHTTPClient(), controlPlaneURL)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		state.mu.RLock()
		id := state.identity
		state.mu.RUnlock()

		// Join if not yet joined
		if id == nil {
			if joinToken == "" {
				logger.Warn("no node identity and no join token provided; waiting for configuration")
				time.Sleep(5 * time.Second)
				continue
			}

			hostname, _ := os.Hostname()
			joinReq := &v1.JoinNodeRequest{
				Token:        joinToken,
				AgentVersion: version,
				Hostname:     hostname,
				Capacity: &v1.NodeCapacity{
					Vcpu:   2,
					RamMb:  4096,
					DiskGb: 40,
				},
			}

			resp, err := client.JoinNode(ctx, connect.NewRequest(joinReq))
			if err != nil {
				logger.Error("failed to join control plane", "err", err, "endpoint", controlPlaneURL)
				time.Sleep(5 * time.Second)
				continue
			}

			id = &PersistedIdentity{
				NodeID:                   resp.Msg.Identity.NodeId,
				OrgID:                    resp.Msg.Identity.OrgId,
				ControlPlaneEndpoint:     resp.Msg.Identity.ControlPlaneEndpoint,
				HeartbeatIntervalSeconds: resp.Msg.Identity.HeartbeatIntervalSeconds,
			}
			if err := saveIdentity(identityPath, id); err != nil {
				logger.Warn("failed to persist identity file", "err", err)
			}

			state.mu.Lock()
			state.identity = id
			state.mu.Unlock()
			logger.Info("node successfully joined", "node_id", id.NodeID, "org_id", id.OrgID)
		}

		// Connect bidirectional stream
		stream := client.Connect(ctx)
		stream.RequestHeader().Set("Authorization", "Bearer ccn_"+id.NodeID)
		stream.RequestHeader().Set("X-Carbon-Node-ID", id.NodeID)
		stream.RequestHeader().Set("X-Carbon-Org-ID", id.OrgID)

		dockerVer := detectDockerVersion()
		hostname, _ := os.Hostname()
		hello := &v1.AgentMessage{
			Payload: &v1.AgentMessage_Hello{
				Hello: &v1.AgentHello{
					NodeId:        id.NodeID,
					AgentVersion:  version,
					DockerVersion: dockerVer,
					Hostname:      hostname,
				},
			},
		}
		if err := stream.Send(hello); err != nil {
			logger.Error("stream send hello failed", "err", err)
			time.Sleep(3 * time.Second)
			continue
		}

		// Read welcome
		welcome, err := stream.Receive()
		if err != nil {
			logger.Error("stream receive welcome failed", "err", err)
			time.Sleep(3 * time.Second)
			continue
		}
		logger.Info("stream welcome received", "payload", welcome.Payload)

		// Send initial heartbeat immediately
		_ = stream.Send(&v1.AgentMessage{
			Payload: &v1.AgentMessage_Heartbeat{
				Heartbeat: &v1.AgentHeartbeat{
					NodeId: id.NodeID,
				},
			},
		})

		state.mu.Lock()
		state.connected = true
		state.mu.Unlock()

		interval := time.Duration(id.HeartbeatIntervalSeconds) * time.Second
		if interval <= 0 || interval > 10*time.Second {
			interval = 10 * time.Second
		}
		ticker := time.NewTicker(interval)

		sendMsgCh := make(chan *v1.AgentMessage, 64)

		streamDone := make(chan struct{})
		go func() {
			defer close(streamDone)
			for {
				msg, err := stream.Receive()
				if err != nil {
					logger.Warn("stream receive closed/error", "err", err)
					return
				}
				logger.Debug("received control envelope", "msg", msg)
				switch p := msg.Payload.(type) {
				case *v1.ControlMessage_AssignWorkload:
					assign := p.AssignWorkload
					if assign == nil || assign.Workload == nil {
						continue
					}
					w := assign.Workload
					logger.Info("executing workload assignment", "workload_id", w.Id, "name", w.Name, "start", assign.Start)
					cid, port, err := runner.Assign(ctx, w, assign.Start)
					if err != nil {
						logger.Error("failed assigning workload", "workload_id", w.Id, "err", err)
						sendMsgCh <- &v1.AgentMessage{
							Payload: &v1.AgentMessage_WorkloadStatus{
								WorkloadStatus: &v1.AgentWorkloadStatus{
									NodeId:     id.NodeID,
									WorkloadId: w.Id,
									Status:     v1.WorkloadStatus_WORKLOAD_STATUS_ERROR,
									Detail:     err.Error(),
								},
							},
						}
					} else {
						st := v1.WorkloadStatus_WORKLOAD_STATUS_STOPPED
						detail := "container created"
						if assign.Start {
							st = v1.WorkloadStatus_WORKLOAD_STATUS_RUNNING
							detail = fmt.Sprintf("container running on port %d", port)
						}
						sendMsgCh <- &v1.AgentMessage{
							Payload: &v1.AgentMessage_WorkloadStatus{
								WorkloadStatus: &v1.AgentWorkloadStatus{
									NodeId:      id.NodeID,
									WorkloadId:  w.Id,
									Status:      st,
									ContainerId: cid,
									Detail:      detail,
								},
							},
						}
					}
				case *v1.ControlMessage_StopWorkload:
					stop := p.StopWorkload
					if stop == nil || stop.WorkloadId == "" {
						continue
					}
					logger.Info("executing stop workload", "workload_id", stop.WorkloadId)
					err := runner.Stop(ctx, stop.WorkloadId, int(stop.TimeoutSeconds))
					if err != nil {
						logger.Error("failed stopping workload", "workload_id", stop.WorkloadId, "err", err)
						sendMsgCh <- &v1.AgentMessage{
							Payload: &v1.AgentMessage_WorkloadStatus{
								WorkloadStatus: &v1.AgentWorkloadStatus{
									NodeId:     id.NodeID,
									WorkloadId: stop.WorkloadId,
									Status:     v1.WorkloadStatus_WORKLOAD_STATUS_ERROR,
									Detail:     err.Error(),
								},
							},
						}
					} else {
						sendMsgCh <- &v1.AgentMessage{
							Payload: &v1.AgentMessage_WorkloadStatus{
								WorkloadStatus: &v1.AgentWorkloadStatus{
									NodeId:     id.NodeID,
									WorkloadId: stop.WorkloadId,
									Status:     v1.WorkloadStatus_WORKLOAD_STATUS_STOPPED,
									Detail:     "container stopped",
								},
							},
						}
					}
				case *v1.ControlMessage_DeleteWorkload:
					del := p.DeleteWorkload
					if del == nil || del.WorkloadId == "" {
						continue
					}
					logger.Info("executing delete workload", "workload_id", del.WorkloadId)
					_ = runner.Delete(ctx, del.WorkloadId, del.DeleteData)
					sendMsgCh <- &v1.AgentMessage{
						Payload: &v1.AgentMessage_WorkloadStatus{
							WorkloadStatus: &v1.AgentWorkloadStatus{
								NodeId:     id.NodeID,
								WorkloadId: del.WorkloadId,
								Status:     v1.WorkloadStatus_WORKLOAD_STATUS_STOPPED,
								Detail:     "workload removed",
							},
						},
					}
				}
			}
		}()

	streamLoop:
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				_ = stream.CloseRequest()
				return
			case <-streamDone:
				ticker.Stop()
				break streamLoop
			case outMsg := <-sendMsgCh:
				if err := stream.Send(outMsg); err != nil {
					logger.Warn("stream send error", "err", err)
					ticker.Stop()
					break streamLoop
				}
			case <-ticker.C:
				hb := &v1.AgentMessage{
					Payload: &v1.AgentMessage_Heartbeat{
						Heartbeat: &v1.AgentHeartbeat{
							NodeId: id.NodeID,
						},
					},
				}
				if err := stream.Send(hb); err != nil {
					logger.Warn("heartbeat send failed", "err", err)
					ticker.Stop()
					break streamLoop
				}
				logger.Info("heartbeat sent", "node_id", id.NodeID)
			}
		}

		state.mu.Lock()
		state.connected = false
		state.mu.Unlock()

		logger.Warn("stream disconnected, reconnecting in 3s")
		time.Sleep(3 * time.Second)
	}
}

func newAgentHTTPClient() *http.Client {
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	return &http.Client{
		Transport: &http.Transport{
			Protocols: protocols,
		},
	}
}
