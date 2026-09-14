package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"github.com/athNdev/mineserver/internal/command"
	appconfig "github.com/athNdev/mineserver/internal/config"
	storage "github.com/athNdev/mineserver/internal/db"
	"github.com/athNdev/mineserver/internal/docker"
	"github.com/athNdev/mineserver/internal/events"
	"github.com/athNdev/mineserver/internal/metrics"
	"github.com/athNdev/mineserver/internal/snapshot"
	"github.com/athNdev/mineserver/internal/webhook"
	"github.com/athNdev/mineserver/pkg/logger"
	v1 "github.com/athNdev/mineserver/pkg/proto/mineserver/v1"
)

// Scheduler manages scheduled tasks for all servers
type Scheduler struct {
	store         *storage.Store
	docker        *docker.Client
	sender        *command.Sender
	appConfig     *appconfig.Config
	metrics       *metrics.Collector
	log           *logger.Logger
	checkInterval time.Duration

	// State management
	running  bool
	mu       sync.RWMutex
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Execution tracking
	runningExecutions map[string]context.CancelFunc // executionID -> cancel func
	executionMu       sync.RWMutex

	// In-flight task tracking to prevent double-dispatching
	runningTasks   map[string]bool // taskID -> bool
	runningTasksMu sync.Mutex

	// Cron parser
	cronParser cron.Parser

	// Stats
	lastCheck time.Time
	nextCheck time.Time

	// Snapshot engine for pre-update snapshots and rollback (MINE-23)
	snapshotEngine *snapshot.Engine
}

// Config holds scheduler configuration
type Config struct {
	CheckInterval time.Duration // How often to check for due tasks
}

// DefaultConfig returns default scheduler configuration
func DefaultConfig() Config {
	return Config{
		CheckInterval: 10 * time.Second,
	}
}

// NewScheduler creates a new task scheduler
func NewScheduler(store *storage.Store, docker *docker.Client, sender *command.Sender, appCfg *appconfig.Config, metricsCollector *metrics.Collector, log *logger.Logger, config ...Config) *Scheduler {
	cfg := DefaultConfig()
	if len(config) > 0 {
		cfg = config[0]
	}

	var snapEngine *snapshot.Engine
	if appCfg != nil {
		snapEngine = snapshot.NewEngine(store, docker, sender, log, snapshot.Config{
			BackupDir: appCfg.Storage.BackupDir,
		})
	}

	return &Scheduler{
		store:             store,
		docker:            docker,
		sender:            sender,
		appConfig:         appCfg,
		metrics:           metricsCollector,
		log:               log,
		checkInterval:     cfg.CheckInterval,
		stopChan:          make(chan struct{}),
		runningExecutions: make(map[string]context.CancelFunc),
		runningTasks:      make(map[string]bool),
		cronParser:        cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
		snapshotEngine:    snapEngine,
	}
}

// SetSnapshotEngine configures custom snapshot engine
func (s *Scheduler) SetSnapshotEngine(e *snapshot.Engine) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshotEngine = e
}

// Start begins the scheduler loop
func (s *Scheduler) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("scheduler already running")
	}

	s.running = true
	s.stopChan = make(chan struct{})

	s.wg.Add(1)
	go s.runLoop()

	s.log.Info("Task scheduler started (check interval: %v)", s.checkInterval)
	return nil
}

// Stop gracefully stops the scheduler
func (s *Scheduler) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	close(s.stopChan)
	s.mu.Unlock()

	// Wait for scheduler loop to finish
	s.wg.Wait()

	// Cancel all running executions
	s.executionMu.Lock()
	for _, cancel := range s.runningExecutions {
		cancel()
	}
	s.runningExecutions = make(map[string]context.CancelFunc)
	s.executionMu.Unlock()

	s.log.Info("Task scheduler stopped")
	return nil
}

// IsRunning returns whether the scheduler is running
func (s *Scheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// GetStatus returns current scheduler status
func (s *Scheduler) GetStatus() SchedulerStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.executionMu.RLock()
	runningCount := len(s.runningExecutions)
	s.executionMu.RUnlock()

	// Count active tasks
	ctx := context.Background()
	tasks, _ := s.store.ListAllScheduledTasks(ctx)
	activeCount := 0
	for _, task := range tasks {
		if task.Status == storage.TaskStatusEnabled {
			activeCount++
		}
	}

	return SchedulerStatus{
		Running:           s.running,
		ActiveTasks:       activeCount,
		RunningExecutions: runningCount,
		LastCheck:         s.lastCheck,
		NextCheck:         s.nextCheck,
	}
}

// SchedulerStatus represents the current state of the scheduler
type SchedulerStatus struct {
	Running           bool
	ActiveTasks       int
	RunningExecutions int
	LastCheck         time.Time
	NextCheck         time.Time
}

// runLoop is the main scheduler loop
func (s *Scheduler) runLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	// Run initial check
	s.checkAndRunDueTasks()

	for {
		select {
		case <-ticker.C:
			s.checkAndRunDueTasks()
		case <-s.stopChan:
			return
		}
	}
}

// checkAndRunDueTasks checks for due tasks and executes them
func (s *Scheduler) checkAndRunDueTasks() {
	s.mu.Lock()
	s.lastCheck = time.Now()
	s.nextCheck = s.lastCheck.Add(s.checkInterval)
	s.mu.Unlock()

	ctx := context.Background()

	// Get all due tasks
	tasks, err := s.store.ListDueScheduledTasks(ctx, time.Now())
	if err != nil {
		s.log.Error("Failed to list due tasks: %v", err)
		return
	}

	for _, task := range tasks {
		s.runningTasksMu.Lock()
		if s.runningTasks[task.ID] {
			s.runningTasksMu.Unlock()
			s.log.Debug("Task %s is already running, skipping duplicate dispatch", task.Name)
			continue
		}
		s.runningTasks[task.ID] = true
		s.runningTasksMu.Unlock()

		// Update next run time immediately to prevent re-querying from DB if check runs again before task finishes
		s.updateNextRun(task)

		// Execute task asynchronously
		s.wg.Add(1)
		go func(t *storage.ScheduledTask) {
			defer s.wg.Done()
			defer func() {
				s.runningTasksMu.Lock()
				delete(s.runningTasks, t.ID)
				s.runningTasksMu.Unlock()
			}()
			s.executeTask(t, "scheduled", v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_UNSPECIFIED, nil)
		}(task)
	}
}

// TriggerTask manually triggers a task execution
func (s *Scheduler) TriggerTask(ctx context.Context, taskID string) (*storage.TaskExecution, error) {
	task, err := s.store.GetScheduledTask(ctx, taskID)
	if err != nil {
		return nil, err
	}

	execution, err := s.executeTask(task, "manual", v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_UNSPECIFIED, nil)
	return execution, err
}

// IsTaskRunning checks if a task is currently executing
func (s *Scheduler) IsTaskRunning(taskID string) bool {
	s.runningTasksMu.Lock()
	defer s.runningTasksMu.Unlock()
	return s.runningTasks[taskID]
}

// Schedulers subscription to the central event bus
func (s *Scheduler) HandleServerEvent(ctx context.Context, event events.Event) {
	tasks, err := s.store.ListEventTriggeredTasks(ctx, event.ServerID, event.Type)
	if err != nil {
		s.log.Error("Failed to list event-triggered tasks for %s: %v", event.Type, err)
		return
	}
	for _, task := range tasks {
		s.wg.Add(1)
		go func(t *storage.ScheduledTask) {
			defer s.wg.Done()
			s.executeTaskForEvent(t, event.Type, event.Data)
		}(task)
	}
}

// executeTaskForEvent runs a task as a result of an event firing. The event
// type is threaded through to webhook executors so the rendered payload
// reflects which event triggered the delivery.
func (s *Scheduler) executeTaskForEvent(task *storage.ScheduledTask, eventType v1.TriggeredEventType, eventData map[string]any) {
	s.executeTask(task, "event", eventType, eventData)
}

// executeTask runs a single task. eventTrigger names the event that drove an
// event-triggered run (empty for scheduled/manual runs).
func (s *Scheduler) executeTask(task *storage.ScheduledTask, trigger string, eventType v1.TriggeredEventType, eventData map[string]any) (*storage.TaskExecution, error) {
	ctx := context.Background()

	// Check if server exists
	server, err := s.store.GetServer(ctx, task.ServerID)
	if err != nil {
		s.log.Error("Task %s: server not found: %v", task.Name, err)
		return nil, err
	}

	// Check if server is online (if required). Webhook tasks always fire â€”
	// they notify, they don't operate on the server, and most useful events
	// (server_stop, server_restart) happen while the server is not running.
	if task.RequireOnline && task.TaskType != storage.TaskTypeWebhook && server.Status != storage.StatusRunning {
		s.log.Debug("Task %s: skipped (server offline)", task.Name)

		// Create skipped execution record
		execution := &storage.TaskExecution{
			ID:        uuid.New().String(),
			TaskID:    task.ID,
			ServerID:  task.ServerID,
			Status:    storage.ExecutionStatusSkipped,
			StartedAt: time.Now(),
			Trigger:   trigger,
			Error:     "server offline",
		}
		now := time.Now()
		execution.EndedAt = &now
		s.store.CreateTaskExecution(ctx, execution)

		// Update next run time if not already updated at dispatch
		if trigger != "scheduled" {
			s.updateNextRun(task)
		}
		return execution, nil
	}

	// Create execution record
	execution := &storage.TaskExecution{
		ID:        uuid.New().String(),
		TaskID:    task.ID,
		ServerID:  task.ServerID,
		Status:    storage.ExecutionStatusRunning,
		StartedAt: time.Now(),
		Trigger:   trigger,
	}
	if err := s.store.CreateTaskExecution(ctx, execution); err != nil {
		s.log.Error("Task %s: failed to create execution record: %v", task.Name, err)
		return nil, err
	}

	// Create cancellable context with timeout
	timeout := time.Duration(task.Timeout) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute // Default timeout
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)

	// Track running execution
	s.executionMu.Lock()
	s.runningExecutions[execution.ID] = cancel
	s.executionMu.Unlock()

	defer func() {
		cancel()
		s.executionMu.Lock()
		delete(s.runningExecutions, execution.ID)
		s.executionMu.Unlock()
	}()

	s.log.Info("Task %s: executing on server %s (trigger: %s)", task.Name, server.Name, trigger)

	// Execute the task based on type, retrying on failure if configured
	var output string
	var execErr error

	for attempt := 0; ; attempt++ {
		output, execErr = s.runTaskType(execCtx, server, task, eventType, eventData)
		if execErr == nil || attempt >= task.RetryCount || execCtx.Err() != nil {
			break
		}

		retryDelay := time.Duration(task.RetryDelay) * time.Second
		if retryDelay <= 0 {
			retryDelay = time.Minute
		}
		s.log.Warn("Task %s: attempt %d failed, retrying in %v: %v", task.Name, attempt+1, retryDelay, execErr)

		select {
		case <-execCtx.Done():
		case <-time.After(retryDelay):
		}
		if execCtx.Err() != nil {
			break
		}
		execution.RetryNum = attempt + 1
	}

	// Update execution record
	endTime := time.Now()
	execution.EndedAt = &endTime
	execution.Duration = endTime.Sub(execution.StartedAt).Milliseconds()
	execution.Output = output

	if execErr != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			execution.Status = storage.ExecutionStatusTimeout
			execution.Error = "execution timed out"
		} else if execCtx.Err() == context.Canceled {
			execution.Status = storage.ExecutionStatusCancelled
			execution.Error = "execution cancelled"
		} else {
			execution.Status = storage.ExecutionStatusFailed
			execution.Error = execErr.Error()
		}
		s.log.Error("Task %s: failed: %v", task.Name, execErr)
	} else {
		execution.Status = storage.ExecutionStatusCompleted
		s.log.Info("Task %s: completed successfully", task.Name)
	}

	s.store.UpdateTaskExecution(ctx, execution)

	// Update next run time if not already updated at dispatch
	if trigger != "scheduled" {
		s.updateNextRun(task)
	}

	return execution, execErr
}

// runTaskType dispatches a single execution attempt to the type-specific executor
func (s *Scheduler) runTaskType(ctx context.Context, server *storage.Server, task *storage.ScheduledTask, eventType v1.TriggeredEventType, eventData map[string]any) (string, error) {
	switch task.TaskType {
	case storage.TaskTypeCommand:
		return s.executeCommandTask(ctx, server, task)
	case storage.TaskTypeRestart:
		return s.executeRestartTask(ctx, server, task)
	case storage.TaskTypeStart:
		return s.executeStartTask(ctx, server, task)
	case storage.TaskTypeStop:
		return s.executeStopTask(ctx, server, task)
	case storage.TaskTypeBackup:
		return s.executeBackupTask(ctx, server, task)
	case storage.TaskTypeScript:
		return s.executeScriptTask(ctx, server, task)
	case storage.TaskTypeWebhook:
		return s.executeWebhookTask(ctx, server, task, eventType, eventData)
	case storage.TaskTypeModpackUpdate:
		return s.executeModpackUpdateTask(ctx, server, task)
	case storage.TaskTypeChunkyPregen:
		return s.executeChunkyPregen(ctx, server, task)
	default:
		return "", fmt.Errorf("unknown task type: %s", task.TaskType)
	}
}

// CancelExecution cancels a running execution
func (s *Scheduler) CancelExecution(executionID string) error {
	s.executionMu.RLock()
	cancel, exists := s.runningExecutions[executionID]
	s.executionMu.RUnlock()

	if !exists {
		return fmt.Errorf("execution not found or already finished")
	}

	cancel()
	return nil
}

// updateNextRun calculates and updates the next run time for a task
func (s *Scheduler) updateNextRun(task *storage.ScheduledTask) {
	ctx := context.Background()
	now := time.Now()
	var nextRun *time.Time

	switch task.Schedule {
	case storage.ScheduleTypeCron:
		if task.CronExpr != "" {
			schedule, err := s.cronParser.Parse(task.CronExpr)
			if err == nil {
				next := schedule.Next(now)
				nextRun = &next
			}
		}
	case storage.ScheduleTypeInterval:
		if task.IntervalSecs > 0 {
			next := now.Add(time.Duration(task.IntervalSecs) * time.Second)
			nextRun = &next
		}
	case storage.ScheduleTypeOnce:
		// Once tasks don't repeat, disable after execution
		task.Status = storage.TaskStatusDisabled
		nextRun = nil
	case storage.ScheduleTypeEvent:
		// Event-triggered tasks have no time-based next run.
		nextRun = nil
	}

	s.store.UpdateTaskNextRun(ctx, task.ID, nextRun, &now)
}

// Task type executors

// CommandTaskConfig represents configuration for command tasks
type CommandTaskConfig struct {
	Command string `json:"command"`
}

func (s *Scheduler) executeCommandTask(ctx context.Context, server *storage.Server, task *storage.ScheduledTask) (string, error) {
	var config CommandTaskConfig
	if task.Config != "" {
		if err := json.Unmarshal([]byte(task.Config), &config); err != nil {
			return "", fmt.Errorf("invalid command config: %w", err)
		}
	}

	if config.Command == "" {
		return "", fmt.Errorf("no command specified")
	}

	if server.ContainerID == "" {
		return "", fmt.Errorf("server has no container")
	}

	output, err := s.sender.SendCommand(ctx, server.ID, config.Command)
	return output, err
}

func (s *Scheduler) executeRestartTask(ctx context.Context, server *storage.Server, _ *storage.ScheduledTask) (string, error) {
	if server.ContainerID == "" {
		return "", fmt.Errorf("server has no container")
	}

	// Stop container
	found, err := s.docker.StopContainer(ctx, server.ContainerID)
	if err != nil {
		return "", fmt.Errorf("failed to stop: %w", err)
	}
	if !found {
		server.ContainerID = ""
		server.Status = storage.StatusStopped
		s.store.UpdateServer(ctx, server)
		return "container not found, marked as stopped", nil
	}

	// Wait a moment
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(2 * time.Second):
	}

	// Start container
	if err := s.docker.StartContainer(ctx, server.ContainerID); err != nil {
		return "", fmt.Errorf("failed to start: %w", err)
	}

	// Update server status
	server.Status = storage.StatusStarting
	now := time.Now()
	server.LastStarted = &now
	s.store.UpdateServer(ctx, server)

	return "server restarted successfully", nil
}

func (s *Scheduler) executeStartTask(ctx context.Context, server *storage.Server, _ *storage.ScheduledTask) (string, error) {
	if server.ContainerID == "" {
		return "", fmt.Errorf("server has no container")
	}

	if err := s.docker.StartContainer(ctx, server.ContainerID); err != nil {
		return "", fmt.Errorf("failed to start: %w", err)
	}

	server.Status = storage.StatusStarting
	now := time.Now()
	server.LastStarted = &now
	s.store.UpdateServer(ctx, server)

	return "server started successfully", nil
}

func (s *Scheduler) executeStopTask(ctx context.Context, server *storage.Server, _ *storage.ScheduledTask) (string, error) {
	if server.ContainerID == "" {
		return "", fmt.Errorf("server has no container")
	}

	found, err := s.docker.StopContainer(ctx, server.ContainerID)
	if err != nil {
		return "", fmt.Errorf("failed to stop: %w", err)
	}
	if !found {
		server.ContainerID = ""
		server.Status = storage.StatusStopped
		s.store.UpdateServer(ctx, server)
		return "container not found, marked as stopped", nil
	}

	server.Status = storage.StatusStopping
	s.store.UpdateServer(ctx, server)

	return "server stopped successfully", nil
}

// ScriptTaskConfig represents configuration for script tasks
type ScriptTaskConfig struct {
	ScriptPath string   `json:"script_path"`
	Args       []string `json:"args"`
}

func (s *Scheduler) executeScriptTask(ctx context.Context, server *storage.Server, task *storage.ScheduledTask) (string, error) {
	// Script tasks execute inside the container
	var config ScriptTaskConfig
	if task.Config != "" {
		if err := json.Unmarshal([]byte(task.Config), &config); err != nil {
			return "", fmt.Errorf("invalid config: %w", err)
		}
	}

	if config.ScriptPath == "" {
		return "", fmt.Errorf("no script/executable specified")
	}

	execCmd := []string{config.ScriptPath}
	return s.docker.Exec(ctx, server.ContainerID, append(execCmd, config.Args...))
}

// CalculateNextRun calculates the next run time for a task based on its schedule
func (s *Scheduler) CalculateNextRun(task *storage.ScheduledTask) (*time.Time, error) {
	now := time.Now()

	switch task.Schedule {
	case storage.ScheduleTypeCron:
		if task.CronExpr == "" {
			return nil, fmt.Errorf("cron expression required")
		}
		schedule, err := s.cronParser.Parse(task.CronExpr)
		if err != nil {
			return nil, fmt.Errorf("invalid cron expression: %w", err)
		}
		next := schedule.Next(now)
		return &next, nil

	case storage.ScheduleTypeInterval:
		if task.IntervalSecs <= 0 {
			return nil, fmt.Errorf("interval must be positive")
		}
		next := now.Add(time.Duration(task.IntervalSecs) * time.Second)
		return &next, nil

	case storage.ScheduleTypeOnce:
		if task.RunAt == nil {
			return nil, fmt.Errorf("run_at time required for once schedule")
		}
		if task.RunAt.Before(now) {
			return nil, nil // Already passed
		}
		return task.RunAt, nil

	case storage.ScheduleTypeEvent:
		// No scheduled time; execution is triggered via OnEvent.
		return nil, nil

	default:
		return nil, fmt.Errorf("unknown schedule type: %s", task.Schedule)
	}
}

// ValidateCronExpr validates a cron expression
func (s *Scheduler) ValidateCronExpr(expr string) error {
	_, err := s.cronParser.Parse(expr)
	return err
}

func (s *Scheduler) executeWebhookTask(ctx context.Context, server *storage.Server, task *storage.ScheduledTask, eventType v1.TriggeredEventType, eventData map[string]any) (string, error) {
	var cfg webhook.Config
	if task.Config != "" {
		if err := json.Unmarshal([]byte(task.Config), &cfg); err != nil {
			return "", fmt.Errorf("invalid webhook config: %w", err)
		}
	}
	if cfg.URL == "" {
		return "", fmt.Errorf("webhook URL is required")
	}

	// Determine which event drove this run. Event-triggered runs pass the
	// firing event directly - otherwise fall back to the first subscribed event or "manual"
	var event string
	switch {
	case eventType != v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_UNSPECIFIED:
		event = webhookEventName(eventType)
	case len(task.EventTriggers) > 0:
		event = webhookEventName(task.EventTriggers[0])
	default:
		event = "manual"
	}

	// Pull live count from metrics so payloads report players accurately
	if s.metrics != nil {
		if m := s.metrics.GetMetrics(server.ID); m != nil {
			server.PlayersOnline = m.PlayersOnline
		}
	}

	payload := webhook.BuildPayload(event, server, eventData)

	result := webhook.Deliver(ctx, cfg, payload)
	output := fmt.Sprintf("HTTP %d in %dms (attempt %d)", result.ResponseCode, result.DurationMs, result.Attempts)
	if result.ResponseBody != "" {
		output += "\n" + result.ResponseBody
	}
	if result.Success {
		return output, nil
	}
	return output, fmt.Errorf("%s", result.ErrorMessage)
}

// Maps a server event type to the lowercase event name used in webhook payloads
func webhookEventName(t v1.TriggeredEventType) string {
	switch t {
	case v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START:
		return "server_start"
	case v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_STOP:
		return "server_stop"
	case v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_RESTART:
		return "server_restart"
	case v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_HEALTHY:
		return "server_healthy"
	case v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_PLAYER_JOIN:
		return "player_join"
	case v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_PLAYER_LEAVE:
		return "player_leave"
	default:
		return "manual"
	}
}

// ModpackUpdateTaskConfig matches the configuration for periodic modpack updates
type ModpackUpdateTaskConfig struct {
	GitURL                   string `json:"git_url"`
	Branch                   string `json:"branch"`
	TargetSubfolder          string `json:"target_subfolder"`
	RestartImmediately       bool   `json:"restart_immediately"`
	GracefulRestart          bool   `json:"graceful_restart"`
	GracefulCountdownSeconds int    `json:"graceful_countdown_seconds"`
	MaintenanceWindowCron    string `json:"maintenance_window_cron"`
	StageConfigUpdates       bool   `json:"stage_config_updates"`
	PreUpdateSnapshot        bool   `json:"pre_update_snapshot"`        // Create volume snapshot before applying updates (MINE-23)
	AutoRollbackOnFailure    bool   `json:"auto_rollback_on_failure"`    // Automatically rollback snapshot if restart/update fails
	AuthToken                string `json:"auth_token"`
}

// executeModpackUpdateTask handles cloning or fetching updates from Git/source, syncing into the server directory, and restarting if updates are applied
func (s *Scheduler) executeModpackUpdateTask(ctx context.Context, server *storage.Server, task *storage.ScheduledTask) (string, error) {
	var cfg ModpackUpdateTaskConfig
	if task.Config != "" {
		if err := json.Unmarshal([]byte(task.Config), &cfg); err != nil {
			return "", fmt.Errorf("invalid modpack update config: %w", err)
		}
	}

	if cfg.GitURL == "" {
		return "", fmt.Errorf("git_url is required for modpack update task")
	}

	branch := cfg.Branch
	if branch == "" {
		branch = "main"
	}

	// Prepare git repository clone directory inside server directory or app cache
	cacheDir := filepath.Join(server.DataPath, ".mineserver_modpack_git")
	gitURL := cfg.GitURL
	if cfg.AuthToken != "" && strings.HasPrefix(gitURL, "https://") {
		// Embed auth token into clone URL
		gitURL = strings.Replace(gitURL, "https://", fmt.Sprintf("https://oauth2:%s@", cfg.AuthToken), 1)
	}

	var hasUpdates bool
	var updateSummary string
	var currentHash string
	var remoteHash string
	var changedFiles []string

	if _, err := os.Stat(filepath.Join(cacheDir, ".git")); os.IsNotExist(err) {
		// Initial clone
		_ = os.MkdirAll(cacheDir, 0755)
		s.log.Info("ModpackTask %s: Performing initial clone from %s (branch: %s)", task.Name, cfg.GitURL, branch)
		cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "-b", branch, gitURL, cacheDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			return string(out), fmt.Errorf("initial git clone failed: %w: %s", err, string(out))
		}
		cmdHead := exec.CommandContext(ctx, "git", "-C", cacheDir, "rev-parse", "HEAD")
		if headOut, err := cmdHead.Output(); err == nil {
			remoteHash = strings.TrimSpace(string(headOut))
		}
		hasUpdates = true
		updateSummary = fmt.Sprintf("Initial modpack clone successful from %s on branch %s", cfg.GitURL, branch)
	} else {
		// Fetch and check if updates are available
		cmdFetch := exec.CommandContext(ctx, "git", "-C", cacheDir, "fetch", "origin", branch)
		if out, err := cmdFetch.CombinedOutput(); err != nil {
			return string(out), fmt.Errorf("git fetch failed: %w: %s", err, string(out))
		}

		cmdHead := exec.CommandContext(ctx, "git", "-C", cacheDir, "rev-parse", "HEAD")
		headOut, _ := cmdHead.Output()
		currentHash = strings.TrimSpace(string(headOut))

		cmdRemote := exec.CommandContext(ctx, "git", "-C", cacheDir, "rev-parse", "FETCH_HEAD")
		remoteOut, _ := cmdRemote.Output()
		remoteHash = strings.TrimSpace(string(remoteOut))

		if currentHash == remoteHash && currentHash != "" {
			return fmt.Sprintf("Modpack is up to date at commit %s (no updates)", currentHash[:min(8, len(currentHash))]), nil
		}

		// Inspect changed files between current commit and new FETCH_HEAD
		if currentHash != "" && remoteHash != "" {
			cmdDiff := exec.CommandContext(ctx, "git", "-C", cacheDir, "diff", "--name-only", currentHash, remoteHash)
			if diffOut, err := cmdDiff.Output(); err == nil {
				for _, f := range strings.Split(string(diffOut), "\n") {
					f = strings.TrimSpace(f)
					if f != "" {
						changedFiles = append(changedFiles, f)
					}
				}
			}
		}

		// Pull latest changes
		cmdReset := exec.CommandContext(ctx, "git", "-C", cacheDir, "reset", "--hard", "FETCH_HEAD")
		if out, err := cmdReset.CombinedOutput(); err != nil {
			return string(out), fmt.Errorf("git reset to FETCH_HEAD failed: %w: %s", err, string(out))
		}

		hasUpdates = true
		updateSummary = fmt.Sprintf("Updated modpack from commit %s to %s",
			currentHash[:min(8, len(currentHash))],
			remoteHash[:min(8, len(remoteHash))])
	}

	if hasUpdates {
		var createdSnapshot *storage.ServerSnapshot

		// One-click pre-update volume snapshotting (MINE-23)
		if s.snapshotEngine != nil {
			snap, snapErr := s.snapshotEngine.CreatePreUpdateSnapshot(ctx, server, fmt.Sprintf("pre-update: %s", updateSummary))
			if snapErr != nil {
				s.log.Warn("ModpackTask %s: Failed to create pre-update volume snapshot: %v", task.Name, snapErr)
			} else {
				createdSnapshot = snap
				s.log.Info("ModpackTask %s: Created pre-update snapshot %s (%s)", task.Name, snap.ID, snap.Name)
			}
		}

		// Stage configuration updates if requested
		if cfg.StageConfigUpdates {
			stagedDir := filepath.Join(server.DataPath, ".mineserver_modpack_staged")
			_ = os.MkdirAll(stagedDir, 0755)
			manifest := map[string]any{
				"from_commit":   currentHash,
				"to_commit":     remoteHash,
				"changed_files": changedFiles,
				"staged_at":     time.Now().Format(time.RFC3339),
				"status":        "staged",
			}
			if manifestData, err := json.MarshalIndent(manifest, "", "  "); err == nil {
				_ = os.WriteFile(filepath.Join(stagedDir, "staged_manifest.json"), manifestData, 0644)
			}
			s.log.Info("ModpackTask %s: Staged %d changed config/pack files in %s", task.Name, len(changedFiles), stagedDir)
		}

		// Source directory to copy from
		srcDir := cacheDir
		if cfg.TargetSubfolder != "" {
			srcDir = filepath.Join(cacheDir, cfg.TargetSubfolder)
		}

		// Sync files into server.DataPath (excluding internal directories)
		s.log.Info("ModpackTask %s: Syncing updated modpack files from %s to %s", task.Name, srcDir, server.DataPath)
		cmdRsync := exec.CommandContext(ctx, "rsync", "-avc",
			"--exclude=.git",
			"--exclude=.mineserver_modpack_git",
			"--exclude=.mineserver_modpack_staged",
			"--exclude=.mineserver_snapshots",
			srcDir+"/", server.DataPath+"/")
		if out, err := cmdRsync.CombinedOutput(); err != nil {
			// Fallback to cp -rf if rsync is not installed
			cmdCp := exec.CommandContext(ctx, "cp", "-rf", srcDir+"/.", server.DataPath+"/")
			if cpOut, cpErr := cmdCp.CombinedOutput(); cpErr != nil {
				// Pure Go fallback if neither rsync nor cp are available (e.g. on Windows)
				excludes := []string{".git", ".mineserver_modpack_git", ".mineserver_modpack_staged", ".mineserver_snapshots"}
				if goErr := copyDirExcluding(srcDir, server.DataPath, excludes); goErr != nil {
					// If copying failed and we took a snapshot, rollback if enabled
					if cfg.AutoRollbackOnFailure && createdSnapshot != nil && s.snapshotEngine != nil {
						_ = s.snapshotEngine.Rollback(ctx, server, createdSnapshot.ID)
					}
					return string(out) + "\n" + string(cpOut), fmt.Errorf("failed to copy modpack files: %w", goErr)
				}
			}
		}

		// Check if maintenance window or immediate restart is triggered
		shouldRestartNow := cfg.RestartImmediately
		if !shouldRestartNow && cfg.MaintenanceWindowCron != "" {
			if s.isWithinMaintenanceWindow(cfg.MaintenanceWindowCron, time.Now()) {
				s.log.Info("ModpackTask %s: Current time is within maintenance window (%s), proceeding with restart",
					task.Name, cfg.MaintenanceWindowCron)
				shouldRestartNow = true
			} else {
				s.log.Info("ModpackTask %s: Outside maintenance window (%s), restart deferred",
					task.Name, cfg.MaintenanceWindowCron)
			}
		}

		if shouldRestartNow {
			countdown := cfg.GracefulCountdownSeconds
			if countdown <= 0 && cfg.GracefulRestart {
				countdown = 10
			}

			if cfg.GracefulRestart || countdown > 0 {
				s.log.Info("ModpackTask %s: Executing graceful restart (countdown: %ds) for server %s", task.Name, countdown, server.Name)
				restartOut, err := s.executeGracefulRestart(ctx, server, task, countdown)
				if err != nil {
					if cfg.AutoRollbackOnFailure && createdSnapshot != nil && s.snapshotEngine != nil {
						s.log.Warn("ModpackTask %s: Restart failed, rolling back to pre-update snapshot %s", task.Name, createdSnapshot.ID)
						_ = s.snapshotEngine.Rollback(ctx, server, createdSnapshot.ID)
					}
					return fmt.Sprintf("%s\nModpack synced, but graceful restart failed: %v", updateSummary, err), err
				}
				return fmt.Sprintf("%s\n%s", updateSummary, restartOut), nil
			}

			s.log.Info("ModpackTask %s: Restarting server %s immediately", task.Name, server.Name)
			restartOut, err := s.executeRestartTask(ctx, server, task)
			if err != nil {
				if cfg.AutoRollbackOnFailure && createdSnapshot != nil && s.snapshotEngine != nil {
					s.log.Warn("ModpackTask %s: Restart failed, rolling back to pre-update snapshot %s", task.Name, createdSnapshot.ID)
					_ = s.snapshotEngine.Rollback(ctx, server, createdSnapshot.ID)
				}
				return fmt.Sprintf("%s\nModpack synced, but server restart failed: %v", updateSummary, err), err
			}
			return fmt.Sprintf("%s\n%s", updateSummary, restartOut), nil
		} else {
			return fmt.Sprintf("%s\nModpack synced successfully (server restart deferred to scheduled maintenance)", updateSummary), nil
		}
	}

	return "No updates found", nil
}

// isWithinMaintenanceWindow checks if the given time falls within a 30-minute window of the cron schedule
func (s *Scheduler) isWithinMaintenanceWindow(cronExpr string, now time.Time) bool {
	sched, err := s.cronParser.Parse(cronExpr)
	if err != nil {
		s.log.Warn("Invalid maintenance window cron expression: %s (%v)", cronExpr, err)
		return false
	}
	prev := sched.Next(now.Add(-30 * time.Minute))
	return !prev.After(now)
}

// executeGracefulRestart notifies in-game players, saves world data, and restarts the server
func (s *Scheduler) executeGracefulRestart(ctx context.Context, server *storage.Server, task *storage.ScheduledTask, countdownSeconds int) (string, error) {
	if countdownSeconds < 1 {
		countdownSeconds = 1
	}

	if server.Status == storage.StatusRunning && server.ContainerID != "" && s.sender != nil {
		_, _ = s.sender.SendCommand(ctx, server.ID, fmt.Sprintf("say [Auto-Update] Modpack update installed. Server restarting in %d seconds for maintenance...", countdownSeconds))

		remaining := countdownSeconds
		for remaining > 0 {
			if remaining == 10 || remaining == 5 || remaining == 3 || remaining == 1 {
				_, _ = s.sender.SendCommand(ctx, server.ID, fmt.Sprintf("say [Auto-Update] Restarting in %d seconds...", remaining))
			}
			if remaining == 5 {
				_, _ = s.sender.SendCommand(ctx, server.ID, "save-all")
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(1 * time.Second):
				remaining--
			}
		}
	}

	return s.executeRestartTask(ctx, server, task)
}

// ChunkyPregenTaskConfig represents configuration for Chunky radius pre-generation tasks
type ChunkyPregenTaskConfig struct {
	World   string `json:"world,omitempty"`
	Radius  int    `json:"radius,omitempty"`
	Shape   string `json:"shape,omitempty"`
	CenterX int    `json:"center_x,omitempty"`
	CenterZ int    `json:"center_z,omitempty"`
}

// executeChunkyPregen runs an automated background Chunky radius pregeneration sequence via RCON
func (s *Scheduler) executeChunkyPregen(ctx context.Context, server *storage.Server, task *storage.ScheduledTask) (string, error) {
	if server.ContainerID == "" {
		return "", fmt.Errorf("server has no container")
	}

	var cfg ChunkyPregenTaskConfig
	if task.Config != "" {
		_ = json.Unmarshal([]byte(task.Config), &cfg)
	}

	world := cfg.World
	if world == "" {
		world = "world"
	}
	radius := cfg.Radius
	if radius <= 0 {
		radius = 2500
	}
	shape := cfg.Shape
	if shape == "" {
		shape = "circle"
	}

	commands := []string{
		fmt.Sprintf("chunky world %s", world),
		fmt.Sprintf("chunky shape %s", shape),
		fmt.Sprintf("chunky center %d %d", cfg.CenterX, cfg.CenterZ),
		fmt.Sprintf("chunky radius %d", radius),
		"chunky start",
	}

	var results []string
	for _, cmd := range commands {
		out, err := s.sender.SendCommand(ctx, server.ID, cmd)
		if err != nil {
			return strings.Join(results, "\n"), fmt.Errorf("chunky command '%s' failed: %w", cmd, err)
		}
		results = append(results, fmt.Sprintf("> %s: %s", cmd, strings.TrimSpace(out)))
	}

	return strings.Join(results, "\n"), nil
}

func copyDirExcluding(src, dst string, excludes []string) error {
	excludeMap := make(map[string]bool, len(excludes))
	for _, e := range excludes {
		excludeMap[e] = true
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) > 0 && excludeMap[parts[0]] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		targetPath := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}
		return copyFileHelper(path, targetPath, info.Mode())
	})
}

func copyFileHelper(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()

	df, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer df.Close()

	_, err = io.Copy(df, sf)
	return err
}
