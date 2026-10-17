// Package reconcile provides background maintenance and health reconciliation
// for the Carbon Cloud control plane.
//
// It monitors node heartbeats, marks silent nodes offline, degrades stranded
// workloads, purges expired join tokens, and flushes transactional outbox emails.
package reconcile

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/notify"
	"github.com/google/uuid"
)

const (
	// DefaultStaleNodeThreshold is the maximum duration a node can go without
	// a heartbeat before being marked offline.
	DefaultStaleNodeThreshold = 5 * time.Minute

	// DefaultExpiredTokenRetention is the duration after expiration or redemption
	// after which join tokens are safely purged from the database.
	DefaultExpiredTokenRetention = 24 * time.Hour

	// DefaultFlushBatch is the maximum number of outbox emails sent per tick.
	DefaultFlushBatch = 50
)

// Options configures the background reconciler.
type Options struct {
	Store                 *db.Store
	Notifier              *notify.Dispatcher
	Outbox                *notify.Outbox
	StaleNodeThreshold    time.Duration
	ExpiredTokenRetention time.Duration
	Logger                *slog.Logger
	Now                   func() time.Time
}

// Reconciler runs periodic maintenance and health checks across the cluster.
type Reconciler struct {
	store                 *db.Store
	notifier              *notify.Dispatcher
	outbox                *notify.Outbox
	staleNodeThreshold    time.Duration
	expiredTokenRetention time.Duration
	logger                *slog.Logger
	now                   func() time.Time
}

// New constructs a Reconciler with sensible defaults.
func New(opts Options) *Reconciler {
	staleThreshold := opts.StaleNodeThreshold
	if staleThreshold <= 0 {
		staleThreshold = DefaultStaleNodeThreshold
	}
	retention := opts.ExpiredTokenRetention
	if retention <= 0 {
		retention = DefaultExpiredTokenRetention
	}
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Reconciler{
		store:                 opts.Store,
		notifier:              opts.Notifier,
		outbox:                opts.Outbox,
		staleNodeThreshold:    staleThreshold,
		expiredTokenRetention: retention,
		logger:                logger,
		now:                   nowFn,
	}
}

// ReconcileResult summarizes the actions taken during a reconciliation pass.
type ReconcileResult struct {
	ReapedNodes       int
	DegradedWorkloads int
	PurgedTokens      int64
	FlushedEmails     int
}

// Run executes the reconciliation loop at the given interval until ctx is cancelled.
func (r *Reconciler) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	r.logger.Info("cloud reconciler started", "interval", interval.String())

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("cloud reconciler stopping", "reason", ctx.Err())
			return
		case <-ticker.C:
			res, err := r.ReconcileOnce(ctx)
			if err != nil {
				r.logger.Error("reconciliation pass encountered error", "err", err)
			} else if res.ReapedNodes > 0 || res.DegradedWorkloads > 0 || res.PurgedTokens > 0 || res.FlushedEmails > 0 {
				r.logger.Info("reconciliation pass completed",
					"reaped_nodes", res.ReapedNodes,
					"degraded_workloads", res.DegradedWorkloads,
					"purged_tokens", res.PurgedTokens,
					"flushed_emails", res.FlushedEmails,
				)
			}
		}
	}
}

// ReconcileOnce performs a single maintenance cycle across nodes, workloads, tokens, and outbox.
func (r *Reconciler) ReconcileOnce(ctx context.Context) (ReconcileResult, error) {
	var res ReconcileResult

	reaped, degraded, err := r.ReconcileNodes(ctx)
	if err != nil {
		return res, fmt.Errorf("reconcile nodes: %w", err)
	}
	res.ReapedNodes = reaped
	res.DegradedWorkloads = degraded

	purged, err := r.PurgeExpiredTokens(ctx)
	if err != nil {
		return res, fmt.Errorf("purge tokens: %w", err)
	}
	res.PurgedTokens = purged

	flushed, err := r.FlushOutbox(ctx, DefaultFlushBatch)
	if err != nil {
		return res, fmt.Errorf("flush outbox: %w", err)
	}
	res.FlushedEmails = flushed

	return res, nil
}

// ReconcileNodes detects silent nodes whose heartbeats have expired, marks them offline,
// updates dependent workloads to degraded, and emits lifecycle notifications.
func (r *Reconciler) ReconcileNodes(ctx context.Context) (int, int, error) {
	if r.store == nil {
		return 0, 0, nil
	}

	cutoff := r.now().Add(-r.staleNodeThreshold)

	// Query silent nodes across all organizations
	var staleNodes []db.Node
	err := r.store.DB().WithContext(ctx).
		Where("status != ? AND (last_heartbeat IS NULL OR last_heartbeat < ?)", "offline", cutoff).
		Find(&staleNodes).Error
	if err != nil {
		return 0, 0, err
	}

	reapedCount := 0
	degradedCount := 0

	for _, n := range staleNodes {
		// Transition node to offline
		n.Status = "offline"
		if err := r.store.DB().WithContext(ctx).Save(&n).Error; err != nil {
			r.logger.Error("failed to mark stale node offline", "node_id", n.ID, "err", err)
			continue
		}
		reapedCount++

		// Dispatch node.offline event
		if r.notifier != nil {
			r.notifier.Dispatch(ctx, notify.WebhookPayload{
				EventID:   uuid.NewString(),
				EventType: "node.offline",
				OrgID:     n.OrgID,
				Timestamp: r.now().Unix(),
				Data: map[string]any{
					"node_id":   n.ID,
					"name":      n.Name,
					"region":    n.Region,
					"stale_at":  cutoff.Format(time.RFC3339),
					"reaped_at": r.now().Format(time.RFC3339),
				},
			})
		}

		// Find active workloads scheduled on the stale node
		var affectedWorkloads []db.Workload
		if err := r.store.DB().WithContext(ctx).
			Where("node_id = ? AND status = ?", n.ID, "running").
			Find(&affectedWorkloads).Error; err != nil {
			r.logger.Error("failed to query workloads for stale node", "node_id", n.ID, "err", err)
			continue
		}

		for _, w := range affectedWorkloads {
			w.Status = "degraded"
			w.StatusDetail = "Host node went offline (heartbeat timeout)"
			if err := r.store.DB().WithContext(ctx).Save(&w).Error; err != nil {
				r.logger.Error("failed to update degraded workload", "workload_id", w.ID, "err", err)
				continue
			}
			degradedCount++

			// Record workload breadcrumb event
			ev := db.WorkloadEvent{
				TenantBase: db.TenantBase{ID: uuid.NewString(), OrgID: w.OrgID},
				WorkloadID: w.ID,
				Kind:       "degraded",
				Message:    fmt.Sprintf("Host node %s went offline due to missed heartbeats", n.Name),
			}
			_ = r.store.DB().WithContext(ctx).Create(&ev).Error

			// Dispatch workload.degraded event
			if r.notifier != nil {
				r.notifier.Dispatch(ctx, notify.WebhookPayload{
					EventID:   uuid.NewString(),
					EventType: "workload.degraded",
					OrgID:     w.OrgID,
					Timestamp: r.now().Unix(),
					Data: map[string]any{
						"workload_id": w.ID,
						"node_id":     n.ID,
						"name":        w.Name,
						"reason":      "host node offline",
					},
				})
			}
		}
	}

	return reapedCount, degradedCount, nil
}

// PurgeExpiredTokens deletes join tokens that have exceeded their retention window
// past expiration or redemption.
func (r *Reconciler) PurgeExpiredTokens(ctx context.Context) (int64, error) {
	if r.store == nil {
		return 0, nil
	}

	cutoff := r.now().Add(-r.expiredTokenRetention)

	res := r.store.DB().WithContext(ctx).
		Where("(expires_at IS NOT NULL AND expires_at < ?) OR (used_at IS NOT NULL AND used_at < ?)", cutoff, cutoff).
		Delete(&db.JoinToken{})

	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// FlushOutbox processes pending outbound transactional emails.
func (r *Reconciler) FlushOutbox(ctx context.Context, maxBatch int) (int, error) {
	if r.outbox == nil {
		return 0, nil
	}
	return r.outbox.Flush(ctx, maxBatch)
}
