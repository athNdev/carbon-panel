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
	"bufio"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/nodeagent"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/obs"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/athNdev/carbon-panel/pkg/proto/cloud/v1/cloudv1connect"
	"github.com/docker/docker/pkg/stdcopy"
	"google.golang.org/protobuf/types/known/timestamppb"
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
		connected := state.connected
		state.mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "ok",
			"version":   version,
			"node_id":   nodeID,
			"connected": connected,
			"uptime":    uptime,
		})
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		joined := state.identity != nil
		connected := state.connected
		state.mu.RUnlock()

		if !joined || !connected {
			http.Error(w, `{"status":"not_ready"}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
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

	fileManager := nodeagent.NewFileManager(dataDir)
	backupManager := nodeagent.NewBackupManager(dataDir)

	// Run agent lifecycle loop in background
	go runAgentLoop(ctx, logger, state, runner, fileManager, backupManager, controlPlaneURL, joinToken, identityFile)

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
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("local http server failed", "err", err)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	return nil
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
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func detectDockerVersion() string {
	out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	return "unknown"
}

func runAgentLoop(ctx context.Context, logger *slog.Logger, state *AgentState, runner nodeagent.Runner, files *nodeagent.FileManager, backupManager *nodeagent.BackupManager, controlPlaneURL, joinToken, identityPath string) {
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
		var activeStreams sync.Map
		var assignedWorkloads sync.Map
		var idleTrackers sync.Map

		type idleState struct {
			mu         sync.Mutex
			lastActive time.Time
			hibernated bool
		}

		streamDone := make(chan struct{})

		// Background idle watchdog for auto-hibernation
		go func() {
			watchdogTicker := time.NewTicker(30 * time.Second)
			defer watchdogTicker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-streamDone:
					return
				case now := <-watchdogTicker.C:
					assignedWorkloads.Range(func(key, value any) bool {
						wID, ok := key.(string)
						if !ok {
							return true
						}
						w, ok := value.(*v1.Workload)
						if !ok || w == nil || w.Spec == nil {
							return true
						}
						idleMinutes := int(w.Spec.IdleTimeoutMinutes)
						if idleMinutes <= 0 {
							return true
						}

						rawTracker, _ := idleTrackers.LoadOrStore(wID, &idleState{lastActive: now})
						ist := rawTracker.(*idleState)
						ist.mu.Lock()
						hibernated := ist.hibernated
						lastActive := ist.lastActive
						ist.mu.Unlock()

						if hibernated {
							return true
						}

						metrics, err := runner.GetMetrics(ctx, wID)
						if err != nil {
							return true
						}

						if metrics.PlayersOnline > 0 {
							ist.mu.Lock()
							ist.lastActive = now
							ist.mu.Unlock()
							return true
						}

						if now.Sub(lastActive) >= time.Duration(idleMinutes)*time.Minute {
							logger.Info("idle watchdog triggered hibernation", "workload_id", wID, "idle_minutes", idleMinutes)
							mode := w.Spec.HibernationMode
							if mode == "" {
								mode = "pause"
							}
							if err := runner.Hibernate(ctx, wID, mode); err == nil {
								ist.mu.Lock()
								ist.hibernated = true
								ist.mu.Unlock()
								sendMsgCh <- &v1.AgentMessage{
									Payload: &v1.AgentMessage_WorkloadStatus{
										WorkloadStatus: &v1.AgentWorkloadStatus{
											NodeId:     id.NodeID,
											WorkloadId: wID,
											Status:     v1.WorkloadStatus_WORKLOAD_STATUS_HIBERNATED,
											Detail:     fmt.Sprintf("auto-hibernated after %d min idle", idleMinutes),
										},
									},
								}
							}
						}
						return true
					})
				}
			}
		}()
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
					assignedWorkloads.Store(w.Id, w)
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
							if _, loaded := activeStreams.LoadOrStore(w.Id, true); !loaded {
								go streamContainerLogs(ctx, runner, id.NodeID, w.Id, sendMsgCh, &activeStreams, logger)
							}
						}
						sendMsgCh <- &v1.AgentMessage{
							Payload: &v1.AgentMessage_WorkloadStatus{
								WorkloadStatus: &v1.AgentWorkloadStatus{
									NodeId:      id.NodeID,
									WorkloadId:  w.Id,
									Status:      st,
									ContainerId: cid,
									Detail:      detail,
									HostPort:    int32(port),
								},
							},
						}
					}
				case *v1.ControlMessage_StopWorkload:
					stop := p.StopWorkload
					if stop == nil || stop.WorkloadId == "" {
						continue
					}
					activeStreams.Delete(stop.WorkloadId)
					assignedWorkloads.Delete(stop.WorkloadId)
					idleTrackers.Delete(stop.WorkloadId)
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
					activeStreams.Delete(del.WorkloadId)
					assignedWorkloads.Delete(del.WorkloadId)
					idleTrackers.Delete(del.WorkloadId)
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

				case *v1.ControlMessage_RunCommand:
					cmd := p.RunCommand
					if cmd == nil || cmd.WorkloadId == "" {
						continue
					}
					logger.Info("executing command on workload", "workload_id", cmd.WorkloadId, "command", cmd.Command, "command_id", cmd.CommandId)
					out, err := runner.RunCommand(ctx, cmd.WorkloadId, cmd.Command)
					errMsg := ""
					success := err == nil
					if err != nil {
						errMsg = err.Error()
					}
					sendMsgCh <- &v1.AgentMessage{
						Payload: &v1.AgentMessage_CommandResult{
							CommandResult: &v1.AgentCommandResult{
								CommandId: cmd.CommandId,
								Success:   success,
								Output:    out,
								Error:     errMsg,
							},
						},
					}

				case *v1.ControlMessage_FileList:
					req := p.FileList
					if req == nil || req.WorkloadId == "" {
						continue
					}
					flist, err := files.ListFiles(req.WorkloadId, req.Path)
					errMsg := ""
					success := err == nil
					if err != nil {
						errMsg = err.Error()
					}
					sendMsgCh <- &v1.AgentMessage{
						Payload: &v1.AgentMessage_FileListResult{
							FileListResult: &v1.AgentFileListResult{
								CommandId: req.CommandId,
								Success:   success,
								Error:     errMsg,
								Files:     flist,
							},
						},
					}

				case *v1.ControlMessage_FileRead:
					req := p.FileRead
					if req == nil || req.WorkloadId == "" {
						continue
					}
					go func(cmdID, wID, relPath string) {
						rErr := files.ReadFile(wID, relPath, func(chunk []byte, isLast bool, totalSize int64) error {
							sendMsgCh <- &v1.AgentMessage{
								Payload: &v1.AgentMessage_FileReadChunk{
									FileReadChunk: &v1.AgentReadFileChunk{
										CommandId: cmdID,
										Success:   true,
										Chunk:     chunk,
										IsLast:    isLast,
										TotalSize: totalSize,
									},
								},
							}
							return nil
						})
						if rErr != nil {
							sendMsgCh <- &v1.AgentMessage{
								Payload: &v1.AgentMessage_FileReadChunk{
									FileReadChunk: &v1.AgentReadFileChunk{
										CommandId: cmdID,
										Success:   false,
										Error:     rErr.Error(),
										IsLast:    true,
									},
								},
							}
						}
					}(req.CommandId, req.WorkloadId, req.Path)

				case *v1.ControlMessage_FileWrite:
					req := p.FileWrite
					if req == nil || req.WorkloadId == "" {
						continue
					}
					written, err := files.WriteChunk(req.CommandId, req.WorkloadId, req.Path, req.Chunk, req.IsLast, req.Mode)
					errMsg := ""
					success := err == nil
					if err != nil {
						errMsg = err.Error()
					}
					if req.IsLast || !success {
						sendMsgCh <- &v1.AgentMessage{
							Payload: &v1.AgentMessage_FileWriteResult{
								FileWriteResult: &v1.AgentWriteFileResult{
									CommandId:    req.CommandId,
									Success:      success,
									Error:        errMsg,
									BytesWritten: written,
								},
							},
						}
					}

				case *v1.ControlMessage_FileDelete:
					req := p.FileDelete
					if req == nil || req.WorkloadId == "" {
						continue
					}
					err := files.DeleteFile(req.WorkloadId, req.Path, req.Recursive)
					errMsg := ""
					success := err == nil
					if err != nil {
						errMsg = err.Error()
					}
					sendMsgCh <- &v1.AgentMessage{
						Payload: &v1.AgentMessage_FileDeleteResult{
							FileDeleteResult: &v1.AgentDeleteFileResult{
								CommandId: req.CommandId,
								Success:   success,
								Error:     errMsg,
							},
						},
					}

				case *v1.ControlMessage_DirCreate:
					req := p.DirCreate
					if req == nil || req.WorkloadId == "" {
						continue
					}
					err := files.CreateDirectory(req.WorkloadId, req.Path)
					errMsg := ""
					success := err == nil
					if err != nil {
						errMsg = err.Error()
					}
					sendMsgCh <- &v1.AgentMessage{
						Payload: &v1.AgentMessage_DirCreateResult{
							DirCreateResult: &v1.AgentCreateDirectoryResult{
								CommandId: req.CommandId,
								Success:   success,
								Error:     errMsg,
							},
						},
					}

				case *v1.ControlMessage_FileStat:
					req := p.FileStat
					if req == nil || req.WorkloadId == "" {
						continue
					}
					stat, err := files.Stat(req.WorkloadId, req.Path)
					errMsg := ""
					success := err == nil
					if err != nil {
						errMsg = err.Error()
					}
					sendMsgCh <- &v1.AgentMessage{
						Payload: &v1.AgentMessage_FileStatResult{
							FileStatResult: &v1.AgentStatFileResult{
								CommandId: req.CommandId,
								Success:   success,
								Error:     errMsg,
								Info:      stat,
							},
						},
					}

				case *v1.ControlMessage_FileRename:
					req := p.FileRename
					if req == nil || req.WorkloadId == "" {
						continue
					}
					err := files.RenameFile(req.WorkloadId, req.OldPath, req.NewPath)
					errMsg := ""
					success := err == nil
					if err != nil {
						errMsg = err.Error()
					}
					sendMsgCh <- &v1.AgentMessage{
						Payload: &v1.AgentMessage_FileRenameResult{
							FileRenameResult: &v1.AgentFileRenameResult{
								CommandId: req.CommandId,
								Success:   success,
								Error:     errMsg,
							},
						},
					}

				case *v1.ControlMessage_CreateBackup:
					req := p.CreateBackup
					if req == nil || req.WorkloadId == "" || req.BackupId == "" {
						continue
					}
					if runner != nil {
						_, _ = runner.RunCommand(ctx, req.WorkloadId, "save-off")
						_, _ = runner.RunCommand(ctx, req.WorkloadId, "save-all flush")
					}
					size, sha, err := backupManager.CreateBackup(req.WorkloadId, req.BackupId)
					if runner != nil {
						_, _ = runner.RunCommand(ctx, req.WorkloadId, "save-on")
					}
					errMsg := ""
					success := err == nil
					if err != nil {
						errMsg = err.Error()
					}
					sendMsgCh <- &v1.AgentMessage{
						Payload: &v1.AgentMessage_BackupCreateResult{
							BackupCreateResult: &v1.AgentCreateBackupResult{
								CommandId:  req.CommandId,
								WorkloadId: req.WorkloadId,
								BackupId:   req.BackupId,
								Success:    success,
								Error:      errMsg,
								SizeBytes:  size,
								Sha256:     sha,
							},
						},
					}

				case *v1.ControlMessage_RestoreBackup:
					req := p.RestoreBackup
					if req == nil || req.WorkloadId == "" || req.BackupId == "" {
						continue
					}
					err := backupManager.RestoreBackup(req.WorkloadId, req.BackupId)
					errMsg := ""
					success := err == nil
					if err != nil {
						errMsg = err.Error()
					}
					sendMsgCh <- &v1.AgentMessage{
						Payload: &v1.AgentMessage_BackupRestoreResult{
							BackupRestoreResult: &v1.AgentRestoreBackupResult{
								CommandId:  req.CommandId,
								WorkloadId: req.WorkloadId,
								BackupId:   req.BackupId,
								Success:    success,
								Error:      errMsg,
							},
						},
					}

				case *v1.ControlMessage_DeleteBackup:
					req := p.DeleteBackup
					if req == nil || req.WorkloadId == "" || req.BackupId == "" {
						continue
					}
					err := backupManager.DeleteBackup(req.WorkloadId, req.BackupId)
					errMsg := ""
					success := err == nil
					if err != nil {
						errMsg = err.Error()
					}
					sendMsgCh <- &v1.AgentMessage{
						Payload: &v1.AgentMessage_BackupDeleteResult{
							BackupDeleteResult: &v1.AgentDeleteBackupResult{
								CommandId:  req.CommandId,
								WorkloadId: req.WorkloadId,
								BackupId:   req.BackupId,
								Success:    success,
								Error:      errMsg,
							},
						},
					}

				case *v1.ControlMessage_GetWorkloadMetrics:
					req := p.GetWorkloadMetrics
					if req == nil || req.WorkloadId == "" {
						continue
					}
					metrics, err := runner.GetMetrics(ctx, req.WorkloadId)
					errMsg := ""
					success := err == nil
					if err != nil {
						errMsg = err.Error()
					}
					sendMsgCh <- &v1.AgentMessage{
						Payload: &v1.AgentMessage_MetricsResult{
							MetricsResult: &v1.AgentGetWorkloadMetricsResult{
								CommandId:  req.CommandId,
								WorkloadId: req.WorkloadId,
								Success:    success,
								Error:      errMsg,
								Metrics:    metrics,
							},
						},
					}

				case *v1.ControlMessage_HibernateWorkload:
					req := p.HibernateWorkload
					if req == nil || req.WorkloadId == "" {
						continue
					}
					logger.Info("executing hibernate workload", "workload_id", req.WorkloadId, "mode", req.Mode, "command_id", req.CommandId)
					err := runner.Hibernate(ctx, req.WorkloadId, req.Mode)
					errMsg := ""
					success := err == nil
					if err != nil {
						errMsg = err.Error()
					}
					sendMsgCh <- &v1.AgentMessage{
						Payload: &v1.AgentMessage_HibernateResult{
							HibernateResult: &v1.AgentHibernateResult{
								CommandId:  req.CommandId,
								WorkloadId: req.WorkloadId,
								Success:    success,
								Error:      errMsg,
							},
						},
					}
					if success {
						if val, ok := idleTrackers.Load(req.WorkloadId); ok {
							ist := val.(*idleState)
							ist.mu.Lock()
							ist.hibernated = true
							ist.mu.Unlock()
						}
						sendMsgCh <- &v1.AgentMessage{
							Payload: &v1.AgentMessage_WorkloadStatus{
								WorkloadStatus: &v1.AgentWorkloadStatus{
									NodeId:     id.NodeID,
									WorkloadId: req.WorkloadId,
									Status:     v1.WorkloadStatus_WORKLOAD_STATUS_HIBERNATED,
									Detail:     "workload hibernated",
								},
							},
						}
					}

				case *v1.ControlMessage_WakeWorkload:
					req := p.WakeWorkload
					if req == nil || req.WorkloadId == "" {
						continue
					}
					logger.Info("executing wake workload", "workload_id", req.WorkloadId, "command_id", req.CommandId)
					err := runner.Wake(ctx, req.WorkloadId)
					errMsg := ""
					success := err == nil
					if err != nil {
						errMsg = err.Error()
					}
					sendMsgCh <- &v1.AgentMessage{
						Payload: &v1.AgentMessage_WakeResult{
							WakeResult: &v1.AgentWakeResult{
								CommandId:  req.CommandId,
								WorkloadId: req.WorkloadId,
								Success:    success,
								Error:      errMsg,
							},
						},
					}
					if success {
						if val, ok := idleTrackers.Load(req.WorkloadId); ok {
							ist := val.(*idleState)
							ist.mu.Lock()
							ist.hibernated = false
							ist.lastActive = time.Now()
							ist.mu.Unlock()
						}
						sendMsgCh <- &v1.AgentMessage{
							Payload: &v1.AgentMessage_WorkloadStatus{
								WorkloadStatus: &v1.AgentWorkloadStatus{
									NodeId:     id.NodeID,
									WorkloadId: req.WorkloadId,
									Status:     v1.WorkloadStatus_WORKLOAD_STATUS_RUNNING,
									Detail:     "workload resumed from hibernation",
								},
							},
						}
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
				var wlMetrics []*v1.WorkloadMetrics
				if wIDs, err := runner.ListActiveWorkloadIDs(ctx); err == nil {
					for _, wid := range wIDs {
						if m, err := runner.GetMetrics(ctx, wid); err == nil && m != nil {
							wlMetrics = append(wlMetrics, m)
						}
					}
				}
				hb := &v1.AgentMessage{
					Payload: &v1.AgentMessage_Heartbeat{
						Heartbeat: &v1.AgentHeartbeat{
							NodeId:          id.NodeID,
							WorkloadMetrics: wlMetrics,
						},
					},
				}
				if err := stream.Send(hb); err != nil {
					logger.Warn("heartbeat send failed", "err", err)
					ticker.Stop()
					break streamLoop
				}
				logger.Info("heartbeat sent", "node_id", id.NodeID, "workloads_sampled", len(wlMetrics))
			}
		}

		state.mu.Lock()
		state.connected = false
		state.mu.Unlock()

		logger.Warn("stream disconnected, reconnecting in 3s")
		time.Sleep(3 * time.Second)
	}
}

func streamContainerLogs(
	ctx context.Context,
	runner nodeagent.Runner,
	nodeID, workloadID string,
	sendMsgCh chan<- *v1.AgentMessage,
	activeStreams *sync.Map,
	logger *slog.Logger,
) {
	defer activeStreams.Delete(workloadID)

	reader, err := runner.Logs(ctx, workloadID, 100, true)
	if err != nil {
		logger.Warn("failed opening log stream for workload", "workload_id", workloadID, "err", err)
		return
	}
	defer func() { _ = reader.Close() }()

	prOut, pwOut := io.Pipe()
	prErr, pwErr := io.Pipe()

	go func() {
		defer func() { _ = pwOut.Close() }()
		defer func() { _ = pwErr.Close() }()
		_, _ = stdcopy.StdCopy(pwOut, pwErr, reader)
	}()

	scanLines := func(r io.Reader, isStderr bool) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			text := scanner.Text()
			line := &v1.WorkloadLogLine{
				Source:    "console",
				Line:      text,
				Stderr:    isStderr,
				Timestamp: timestamppb.Now(),
			}
			select {
			case <-ctx.Done():
				return
			case sendMsgCh <- &v1.AgentMessage{
				Payload: &v1.AgentMessage_Logs{
					Logs: &v1.AgentLogChunk{
						NodeId:     nodeID,
						WorkloadId: workloadID,
						Lines:      []*v1.WorkloadLogLine{line},
					},
				},
			}:
			}
		}
	}

	go scanLines(prErr, true)
	scanLines(prOut, false)
}

func newAgentHTTPClient() *http.Client {
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	return &http.Client{
		Transport: &http.Transport{
			Protocols:           protocols,
			ForceAttemptHTTP2:   true,
			DisableCompression: true,
		},
	}
}
