package svc

import (
	"context"
	"log/slog"
	"sync"
	"time"

	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
)

// NodeSession tracks an active streaming connection with a node agent.
type NodeSession struct {
	NodeID string
	sendCh chan *v1.ControlMessage
	done   chan struct{}
}

// NextMessage returns a channel of outbound control envelopes for the node.
func (s *NodeSession) NextMessage() <-chan *v1.ControlMessage {
	return s.sendCh
}

// AgentDispatcher manages active bi-directional gRPC/Connect streams to node agents.
type AgentDispatcher struct {
	mu                   sync.RWMutex
	nodes                map[string]*NodeSession
	cmdMu                sync.RWMutex
	cmdWaiters           map[string]chan *v1.AgentCommandResult
	logMu                sync.RWMutex
	logListeners         map[string]map[chan *v1.WorkloadLogLine]struct{}
	fileMu               sync.RWMutex
	fileListWaiters      map[string]chan *v1.AgentFileListResult
	fileChunkWaiters     map[string]chan *v1.AgentReadFileChunk
	fileWriteWaiters     map[string]chan *v1.AgentWriteFileResult
	fileDeleteWaiters    map[string]chan *v1.AgentDeleteFileResult
	dirCreateWaiters     map[string]chan *v1.AgentCreateDirectoryResult
	fileStatWaiters      map[string]chan *v1.AgentStatFileResult
	fileRenameWaiters    map[string]chan *v1.AgentFileRenameResult
	backupMu             sync.RWMutex
	backupCreateWaiters  map[string]chan *v1.AgentCreateBackupResult
	backupRestoreWaiters map[string]chan *v1.AgentRestoreBackupResult
	backupDeleteWaiters  map[string]chan *v1.AgentDeleteBackupResult
	metricsMu            sync.RWMutex
	cachedMetrics        map[string]*v1.WorkloadMetrics
	metricsWaiters       map[string]chan *v1.AgentGetWorkloadMetricsResult
	hibWakeMu            sync.RWMutex
	hibernateWaiters     map[string]chan *v1.AgentHibernateResult
	wakeWaiters          map[string]chan *v1.AgentWakeResult
	logger               *slog.Logger
}

// NewAgentDispatcher creates a thread-safe registry of connected node agents.
func NewAgentDispatcher(logger *slog.Logger) *AgentDispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &AgentDispatcher{
		nodes:                make(map[string]*NodeSession),
		cmdWaiters:           make(map[string]chan *v1.AgentCommandResult),
		logListeners:         make(map[string]map[chan *v1.WorkloadLogLine]struct{}),
		fileListWaiters:      make(map[string]chan *v1.AgentFileListResult),
		fileChunkWaiters:     make(map[string]chan *v1.AgentReadFileChunk),
		fileWriteWaiters:     make(map[string]chan *v1.AgentWriteFileResult),
		fileDeleteWaiters:    make(map[string]chan *v1.AgentDeleteFileResult),
		dirCreateWaiters:     make(map[string]chan *v1.AgentCreateDirectoryResult),
		fileStatWaiters:      make(map[string]chan *v1.AgentStatFileResult),
		fileRenameWaiters:    make(map[string]chan *v1.AgentFileRenameResult),
		backupCreateWaiters:  make(map[string]chan *v1.AgentCreateBackupResult),
		backupRestoreWaiters: make(map[string]chan *v1.AgentRestoreBackupResult),
		backupDeleteWaiters:  make(map[string]chan *v1.AgentDeleteBackupResult),
		cachedMetrics:        make(map[string]*v1.WorkloadMetrics),
		metricsWaiters:       make(map[string]chan *v1.AgentGetWorkloadMetricsResult),
		hibernateWaiters:     make(map[string]chan *v1.AgentHibernateResult),
		wakeWaiters:          make(map[string]chan *v1.AgentWakeResult),
		logger:               logger,
	}
}

// Register creates or updates the active session for a given node ID.
func (d *AgentDispatcher) Register(nodeID string) *NodeSession {
	d.mu.Lock()
	defer d.mu.Unlock()
	if old, exists := d.nodes[nodeID]; exists {
		close(old.done)
		close(old.sendCh)
	}
	sess := &NodeSession{
		NodeID: nodeID,
		sendCh: make(chan *v1.ControlMessage, 256),
		done:   make(chan struct{}),
	}
	d.nodes[nodeID] = sess
	d.logger.Info("node agent registered in dispatcher", "node_id", nodeID)
	return sess
}

// Unregister cleanly closes the session and removes it from the active registry.
func (d *AgentDispatcher) Unregister(nodeID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if sess, exists := d.nodes[nodeID]; exists {
		close(sess.done)
		close(sess.sendCh)
		delete(d.nodes, nodeID)
		d.logger.Info("node agent unregistered from dispatcher", "node_id", nodeID)
	}
}

// Dispatch queues a control message to be sent to a connected node, waiting up to 10s if buffer is full.
func (d *AgentDispatcher) Dispatch(nodeID string, msg *v1.ControlMessage) bool {
	return d.DispatchTimeout(nodeID, msg, 10*time.Second)
}

// DispatchTimeout queues a control message, waiting up to timeout if buffer is full.
func (d *AgentDispatcher) DispatchTimeout(nodeID string, msg *v1.ControlMessage, timeout time.Duration) bool {
	d.mu.RLock()
	sess, exists := d.nodes[nodeID]
	d.mu.RUnlock()
	if !exists {
		d.logger.Debug("cannot dispatch control message: node not connected", "node_id", nodeID)
		return false
	}
	if timeout <= 0 {
		select {
		case <-sess.done:
			return false
		case sess.sendCh <- msg:
			return true
		default:
			d.logger.Warn("control message buffer full for node", "node_id", nodeID)
			return false
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-sess.done:
		return false
	case sess.sendCh <- msg:
		return true
	case <-timer.C:
		d.logger.Warn("control message dispatch timed out (buffer full) for node", "node_id", nodeID)
		return false
	}
}

// DispatchContext queues a control message, blocking until space is available or ctx is done.
func (d *AgentDispatcher) DispatchContext(ctx context.Context, nodeID string, msg *v1.ControlMessage) bool {
	d.mu.RLock()
	sess, exists := d.nodes[nodeID]
	d.mu.RUnlock()
	if !exists {
		d.logger.Debug("cannot dispatch control message: node not connected", "node_id", nodeID)
		return false
	}
	select {
	case <-sess.done:
		return false
	case <-ctx.Done():
		return false
	case sess.sendCh <- msg:
		return true
	}
}

// IsConnected returns whether the node agent currently holds an active stream.
func (d *AgentDispatcher) IsConnected(nodeID string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, exists := d.nodes[nodeID]
	return exists
}

// ExpectCommand registers a correlation channel waiting for a command result.
func (d *AgentDispatcher) ExpectCommand(commandID string) chan *v1.AgentCommandResult {
	ch := make(chan *v1.AgentCommandResult, 1)
	d.cmdMu.Lock()
	d.cmdWaiters[commandID] = ch
	d.cmdMu.Unlock()
	return ch
}

// ResolveCommand delivers a command result to any waiting caller.
func (d *AgentDispatcher) ResolveCommand(res *v1.AgentCommandResult) {
	if res == nil || res.CommandId == "" {
		return
	}
	d.cmdMu.Lock()
	ch, exists := d.cmdWaiters[res.CommandId]
	if exists {
		delete(d.cmdWaiters, res.CommandId)
	}
	d.cmdMu.Unlock()
	if exists {
		ch <- res
	}
}

// CancelCommand cleans up an abandoned command waiter.
func (d *AgentDispatcher) CancelCommand(commandID string) {
	d.cmdMu.Lock()
	delete(d.cmdWaiters, commandID)
	d.cmdMu.Unlock()
}

// ExpectFileList registers a channel waiting for directory contents.
func (d *AgentDispatcher) ExpectFileList(commandID string) chan *v1.AgentFileListResult {
	ch := make(chan *v1.AgentFileListResult, 1)
	d.fileMu.Lock()
	d.fileListWaiters[commandID] = ch
	d.fileMu.Unlock()
	return ch
}

// ResolveFileList delivers directory contents to a waiting caller.
func (d *AgentDispatcher) ResolveFileList(res *v1.AgentFileListResult) {
	if res == nil || res.CommandId == "" {
		return
	}
	d.fileMu.Lock()
	ch, exists := d.fileListWaiters[res.CommandId]
	if exists {
		delete(d.fileListWaiters, res.CommandId)
	}
	d.fileMu.Unlock()
	if exists {
		ch <- res
	}
}

// CancelFileList cleans up an abandoned file list waiter.
func (d *AgentDispatcher) CancelFileList(commandID string) {
	d.fileMu.Lock()
	delete(d.fileListWaiters, commandID)
	d.fileMu.Unlock()
}

// ExpectFileChunk registers a channel waiting for streaming file chunks.
func (d *AgentDispatcher) ExpectFileChunk(commandID string) chan *v1.AgentReadFileChunk {
	ch := make(chan *v1.AgentReadFileChunk, 64)
	d.fileMu.Lock()
	d.fileChunkWaiters[commandID] = ch
	d.fileMu.Unlock()
	return ch
}

// ResolveFileChunk delivers a file chunk to a waiting streaming caller.
func (d *AgentDispatcher) ResolveFileChunk(chunk *v1.AgentReadFileChunk) {
	if chunk == nil || chunk.CommandId == "" {
		return
	}
	d.fileMu.RLock()
	ch, exists := d.fileChunkWaiters[chunk.CommandId]
	d.fileMu.RUnlock()
	if exists {
		ch <- chunk
	}
}

// CancelFileChunk cleans up an abandoned file chunk channel.
func (d *AgentDispatcher) CancelFileChunk(commandID string) {
	d.fileMu.Lock()
	if ch, exists := d.fileChunkWaiters[commandID]; exists {
		delete(d.fileChunkWaiters, commandID)
		close(ch)
	}
	d.fileMu.Unlock()
}

// ExpectFileWrite registers a channel waiting for file write completion.
func (d *AgentDispatcher) ExpectFileWrite(commandID string) chan *v1.AgentWriteFileResult {
	ch := make(chan *v1.AgentWriteFileResult, 1)
	d.fileMu.Lock()
	d.fileWriteWaiters[commandID] = ch
	d.fileMu.Unlock()
	return ch
}

// ResolveFileWrite delivers file write result to a waiting caller.
func (d *AgentDispatcher) ResolveFileWrite(res *v1.AgentWriteFileResult) {
	if res == nil || res.CommandId == "" {
		return
	}
	d.fileMu.Lock()
	ch, exists := d.fileWriteWaiters[res.CommandId]
	if exists {
		delete(d.fileWriteWaiters, res.CommandId)
	}
	d.fileMu.Unlock()
	if exists {
		ch <- res
	}
}

// CancelFileWrite cleans up an abandoned file write waiter.
func (d *AgentDispatcher) CancelFileWrite(commandID string) {
	d.fileMu.Lock()
	delete(d.fileWriteWaiters, commandID)
	d.fileMu.Unlock()
}

// ExpectFileDelete registers a channel waiting for file deletion.
func (d *AgentDispatcher) ExpectFileDelete(commandID string) chan *v1.AgentDeleteFileResult {
	ch := make(chan *v1.AgentDeleteFileResult, 1)
	d.fileMu.Lock()
	d.fileDeleteWaiters[commandID] = ch
	d.fileMu.Unlock()
	return ch
}

// ResolveFileDelete delivers file delete result to a waiting caller.
func (d *AgentDispatcher) ResolveFileDelete(res *v1.AgentDeleteFileResult) {
	if res == nil || res.CommandId == "" {
		return
	}
	d.fileMu.Lock()
	ch, exists := d.fileDeleteWaiters[res.CommandId]
	if exists {
		delete(d.fileDeleteWaiters, res.CommandId)
	}
	d.fileMu.Unlock()
	if exists {
		ch <- res
	}
}

// CancelFileDelete cleans up an abandoned file delete waiter.
func (d *AgentDispatcher) CancelFileDelete(commandID string) {
	d.fileMu.Lock()
	delete(d.fileDeleteWaiters, commandID)
	d.fileMu.Unlock()
}

// ExpectDirCreate registers a channel waiting for directory creation.
func (d *AgentDispatcher) ExpectDirCreate(commandID string) chan *v1.AgentCreateDirectoryResult {
	ch := make(chan *v1.AgentCreateDirectoryResult, 1)
	d.fileMu.Lock()
	d.dirCreateWaiters[commandID] = ch
	d.fileMu.Unlock()
	return ch
}

// ResolveDirCreate delivers directory create result to a waiting caller.
func (d *AgentDispatcher) ResolveDirCreate(res *v1.AgentCreateDirectoryResult) {
	if res == nil || res.CommandId == "" {
		return
	}
	d.fileMu.Lock()
	ch, exists := d.dirCreateWaiters[res.CommandId]
	if exists {
		delete(d.dirCreateWaiters, res.CommandId)
	}
	d.fileMu.Unlock()
	if exists {
		ch <- res
	}
}

// CancelDirCreate cleans up an abandoned dir create waiter.
func (d *AgentDispatcher) CancelDirCreate(commandID string) {
	d.fileMu.Lock()
	delete(d.dirCreateWaiters, commandID)
	d.fileMu.Unlock()
}

// ExpectFileStat registers a channel waiting for file stat.
func (d *AgentDispatcher) ExpectFileStat(commandID string) chan *v1.AgentStatFileResult {
	ch := make(chan *v1.AgentStatFileResult, 1)
	d.fileMu.Lock()
	d.fileStatWaiters[commandID] = ch
	d.fileMu.Unlock()
	return ch
}

// ResolveFileStat delivers file stat result to a waiting caller.
func (d *AgentDispatcher) ResolveFileStat(res *v1.AgentStatFileResult) {
	if res == nil || res.CommandId == "" {
		return
	}
	d.fileMu.Lock()
	ch, exists := d.fileStatWaiters[res.CommandId]
	if exists {
		delete(d.fileStatWaiters, res.CommandId)
	}
	d.fileMu.Unlock()
	if exists {
		ch <- res
	}
}

// CancelFileStat cleans up an abandoned file stat waiter.
func (d *AgentDispatcher) CancelFileStat(commandID string) {
	d.fileMu.Lock()
	delete(d.fileStatWaiters, commandID)
	d.fileMu.Unlock()
}

// ExpectFileRename registers a channel waiting for file rename.
func (d *AgentDispatcher) ExpectFileRename(commandID string) chan *v1.AgentFileRenameResult {
	ch := make(chan *v1.AgentFileRenameResult, 1)
	d.fileMu.Lock()
	d.fileRenameWaiters[commandID] = ch
	d.fileMu.Unlock()
	return ch
}

// ResolveFileRename delivers file rename result to a waiting caller.
func (d *AgentDispatcher) ResolveFileRename(res *v1.AgentFileRenameResult) {
	if res == nil || res.CommandId == "" {
		return
	}
	d.fileMu.Lock()
	ch, exists := d.fileRenameWaiters[res.CommandId]
	if exists {
		delete(d.fileRenameWaiters, res.CommandId)
	}
	d.fileMu.Unlock()
	if exists {
		ch <- res
	}
}

// CancelFileRename cleans up an abandoned file rename waiter.
func (d *AgentDispatcher) CancelFileRename(commandID string) {
	d.fileMu.Lock()
	delete(d.fileRenameWaiters, commandID)
	d.fileMu.Unlock()
}

// ExpectCreateBackup registers a channel waiting for backup creation.
func (d *AgentDispatcher) ExpectCreateBackup(commandID string) chan *v1.AgentCreateBackupResult {
	ch := make(chan *v1.AgentCreateBackupResult, 1)
	d.backupMu.Lock()
	d.backupCreateWaiters[commandID] = ch
	d.backupMu.Unlock()
	return ch
}

// ResolveCreateBackup delivers backup creation result to a waiting caller.
func (d *AgentDispatcher) ResolveCreateBackup(res *v1.AgentCreateBackupResult) {
	if res == nil || res.CommandId == "" {
		return
	}
	d.backupMu.Lock()
	ch, exists := d.backupCreateWaiters[res.CommandId]
	if exists {
		delete(d.backupCreateWaiters, res.CommandId)
	}
	d.backupMu.Unlock()
	if exists {
		ch <- res
	}
}

// CancelCreateBackup cleans up an abandoned create backup waiter.
func (d *AgentDispatcher) CancelCreateBackup(commandID string) {
	d.backupMu.Lock()
	delete(d.backupCreateWaiters, commandID)
	d.backupMu.Unlock()
}

// ExpectRestoreBackup registers a channel waiting for backup restoration.
func (d *AgentDispatcher) ExpectRestoreBackup(commandID string) chan *v1.AgentRestoreBackupResult {
	ch := make(chan *v1.AgentRestoreBackupResult, 1)
	d.backupMu.Lock()
	d.backupRestoreWaiters[commandID] = ch
	d.backupMu.Unlock()
	return ch
}

// ResolveRestoreBackup delivers backup restore result to a waiting caller.
func (d *AgentDispatcher) ResolveRestoreBackup(res *v1.AgentRestoreBackupResult) {
	if res == nil || res.CommandId == "" {
		return
	}
	d.backupMu.Lock()
	ch, exists := d.backupRestoreWaiters[res.CommandId]
	if exists {
		delete(d.backupRestoreWaiters, res.CommandId)
	}
	d.backupMu.Unlock()
	if exists {
		ch <- res
	}
}

// CancelRestoreBackup cleans up an abandoned restore backup waiter.
func (d *AgentDispatcher) CancelRestoreBackup(commandID string) {
	d.backupMu.Lock()
	delete(d.backupRestoreWaiters, commandID)
	d.backupMu.Unlock()
}

// ExpectDeleteBackup registers a channel waiting for backup deletion.
func (d *AgentDispatcher) ExpectDeleteBackup(commandID string) chan *v1.AgentDeleteBackupResult {
	ch := make(chan *v1.AgentDeleteBackupResult, 1)
	d.backupMu.Lock()
	d.backupDeleteWaiters[commandID] = ch
	d.backupMu.Unlock()
	return ch
}

// ResolveDeleteBackup delivers backup delete result to a waiting caller.
func (d *AgentDispatcher) ResolveDeleteBackup(res *v1.AgentDeleteBackupResult) {
	if res == nil || res.CommandId == "" {
		return
	}
	d.backupMu.Lock()
	ch, exists := d.backupDeleteWaiters[res.CommandId]
	if exists {
		delete(d.backupDeleteWaiters, res.CommandId)
	}
	d.backupMu.Unlock()
	if exists {
		ch <- res
	}
}

// CancelDeleteBackup cleans up an abandoned delete backup waiter.
func (d *AgentDispatcher) CancelDeleteBackup(commandID string) {
	d.backupMu.Lock()
	delete(d.backupDeleteWaiters, commandID)
	d.backupMu.Unlock()
}

// BroadcastLogs delivers log lines to active subscribers for a workload.
func (d *AgentDispatcher) BroadcastLogs(chunk *v1.AgentLogChunk) {
	if chunk == nil || chunk.WorkloadId == "" || len(chunk.Lines) == 0 {
		return
	}
	d.logMu.RLock()
	subs, exists := d.logListeners[chunk.WorkloadId]
	if !exists || len(subs) == 0 {
		d.logMu.RUnlock()
		return
	}
	for _, line := range chunk.Lines {
		for ch := range subs {
			select {
			case ch <- line:
			default:
			}
		}
	}
	d.logMu.RUnlock()
}

// SubscribeLogs subscribes a channel to real-time log lines for a workload.
func (d *AgentDispatcher) SubscribeLogs(workloadID string) (<-chan *v1.WorkloadLogLine, func()) {
	ch := make(chan *v1.WorkloadLogLine, 128)
	d.logMu.Lock()
	subs, exists := d.logListeners[workloadID]
	if !exists {
		subs = make(map[chan *v1.WorkloadLogLine]struct{})
		d.logListeners[workloadID] = subs
	}
	subs[ch] = struct{}{}
	d.logMu.Unlock()

	cancel := func() {
		d.logMu.Lock()
		if s, ok := d.logListeners[workloadID]; ok {
			delete(s, ch)
			if len(s) == 0 {
				delete(d.logListeners, workloadID)
			}
		}
		d.logMu.Unlock()
		close(ch)
	}
	return ch, cancel
}

// UpdateWorkloadMetrics ingests fresh telemetry reported in agent heartbeats.
func (d *AgentDispatcher) UpdateWorkloadMetrics(metrics []*v1.WorkloadMetrics) {
	if len(metrics) == 0 {
		return
	}
	d.metricsMu.Lock()
	defer d.metricsMu.Unlock()
	for _, m := range metrics {
		if m != nil && m.WorkloadId != "" {
			d.cachedMetrics[m.WorkloadId] = m
		}
	}
}

// GetWorkloadMetrics returns the latest cached metrics for a workload, if any.
func (d *AgentDispatcher) GetWorkloadMetrics(workloadID string) *v1.WorkloadMetrics {
	d.metricsMu.RLock()
	defer d.metricsMu.RUnlock()
	return d.cachedMetrics[workloadID]
}

// ExpectMetrics registers a channel waiting for an on-demand metrics result.
func (d *AgentDispatcher) ExpectMetrics(commandID string) chan *v1.AgentGetWorkloadMetricsResult {
	ch := make(chan *v1.AgentGetWorkloadMetricsResult, 1)
	d.metricsMu.Lock()
	d.metricsWaiters[commandID] = ch
	d.metricsMu.Unlock()
	return ch
}

// ResolveMetrics delivers an on-demand metrics result to a waiting caller and updates cache.
func (d *AgentDispatcher) ResolveMetrics(res *v1.AgentGetWorkloadMetricsResult) {
	if res == nil || res.CommandId == "" {
		return
	}
	d.metricsMu.Lock()
	ch, exists := d.metricsWaiters[res.CommandId]
	if exists {
		delete(d.metricsWaiters, res.CommandId)
	}
	if res.Success && res.Metrics != nil && res.WorkloadId != "" {
		d.cachedMetrics[res.WorkloadId] = res.Metrics
	}
	d.metricsMu.Unlock()
	if exists {
		ch <- res
	}
}

// CancelMetrics cleans up an abandoned metrics waiter.
func (d *AgentDispatcher) CancelMetrics(commandID string) {
	d.metricsMu.Lock()
	delete(d.metricsWaiters, commandID)
	d.metricsMu.Unlock()
}

// ExpectHibernate registers a channel waiting for a workload hibernate result.
func (d *AgentDispatcher) ExpectHibernate(commandID string) chan *v1.AgentHibernateResult {
	ch := make(chan *v1.AgentHibernateResult, 1)
	d.hibWakeMu.Lock()
	d.hibernateWaiters[commandID] = ch
	d.hibWakeMu.Unlock()
	return ch
}

// ResolveHibernate delivers a hibernate result to a waiting caller.
func (d *AgentDispatcher) ResolveHibernate(res *v1.AgentHibernateResult) {
	if res == nil || res.CommandId == "" {
		return
	}
	d.hibWakeMu.Lock()
	ch, exists := d.hibernateWaiters[res.CommandId]
	if exists {
		delete(d.hibernateWaiters, res.CommandId)
	}
	d.hibWakeMu.Unlock()
	if exists {
		ch <- res
	}
}

// CancelHibernate cleans up an abandoned hibernate waiter.
func (d *AgentDispatcher) CancelHibernate(commandID string) {
	d.hibWakeMu.Lock()
	delete(d.hibernateWaiters, commandID)
	d.hibWakeMu.Unlock()
}

// ExpectWake registers a channel waiting for a workload wake result.
func (d *AgentDispatcher) ExpectWake(commandID string) chan *v1.AgentWakeResult {
	ch := make(chan *v1.AgentWakeResult, 1)
	d.hibWakeMu.Lock()
	d.wakeWaiters[commandID] = ch
	d.hibWakeMu.Unlock()
	return ch
}

// ResolveWake delivers a wake result to a waiting caller.
func (d *AgentDispatcher) ResolveWake(res *v1.AgentWakeResult) {
	if res == nil || res.CommandId == "" {
		return
	}
	d.hibWakeMu.Lock()
	ch, exists := d.wakeWaiters[res.CommandId]
	if exists {
		delete(d.wakeWaiters, res.CommandId)
	}
	d.hibWakeMu.Unlock()
	if exists {
		ch <- res
	}
}

// CancelWake cleans up an abandoned wake waiter.
func (d *AgentDispatcher) CancelWake(commandID string) {
	d.hibWakeMu.Lock()
	delete(d.wakeWaiters, commandID)
	d.hibWakeMu.Unlock()
}
