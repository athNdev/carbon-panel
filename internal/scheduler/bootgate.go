package scheduler

import (
	"context"
	"fmt"
	"time"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/minecraft"
)

// defaultBootGateTimeout is the SLP boot verification window (MINE-145).
const defaultBootGateTimeout = 3 * time.Minute

// waitForBoot polls the server's SLP endpoint until it answers or timeout
// elapses. It verifies the JVM is actually serving players, not just that
// the container started.
func (s *Scheduler) waitForBoot(ctx context.Context, server *storage.Server, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultBootGateTimeout
	}
	deadline := time.Now().Add(timeout)
	client := minecraft.NewSLPClient(10 * time.Second)
	port := server.Port
	if port == 0 {
		port = 25565
	}
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := client.Ping(pingCtx, "127.0.0.1", port, server.MCVersion)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return fmt.Errorf("boot gate timed out after %s: %w", timeout.Round(time.Second), lastErr)
}

// verifyBootAfterUpdate runs the SLP boot gate after a modpack-update
// restart (MINE-145). Skipped when no pre-update snapshot exists or the gate
// is disabled (BootGateTimeoutSecs == 0). On timeout it rolls back when
// AutoRollbackOnFailure is set and reports failure.
func (s *Scheduler) verifyBootAfterUpdate(ctx context.Context, server *storage.Server, task *storage.ScheduledTask, cfg ModpackUpdateTaskConfig, createdSnapshot *storage.ServerSnapshot) error {
	if createdSnapshot == nil || cfg.BootGateTimeoutSecs == 0 {
		return nil
	}
	timeout := time.Duration(cfg.BootGateTimeoutSecs) * time.Second
	if timeout <= 0 {
		timeout = defaultBootGateTimeout
	}
	s.log.Info("ModpackTask %s: verifying boot via SLP gate (timeout %s)", task.Name, timeout.Round(time.Second))
	if err := s.waitForBoot(ctx, server, timeout); err != nil {
		if cfg.AutoRollbackOnFailure && s.snapshotEngine != nil {
			s.log.Warn("ModpackTask %s: boot gate failed, rolling back to pre-update snapshot %s", task.Name, createdSnapshot.ID)
			_ = s.snapshotEngine.Rollback(ctx, server, createdSnapshot.ID)
		}
		return fmt.Errorf("server failed boot verification after update: %w", err)
	}
	s.log.Info("ModpackTask %s: boot gate passed", task.Name)
	return nil
}
