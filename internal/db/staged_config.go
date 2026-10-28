// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// standardCronParser accepts classic 5-field cron expressions ("0 3 * * *").
var standardCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// ValidateCronExpr reports whether expr is a parseable 5-field cron expression.
func ValidateCronExpr(expr string) error {
	if expr == "" {
		return fmt.Errorf("cron expression is required for scheduled rollout")
	}
	if _, err := standardCronParser.Parse(expr); err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}
	return nil
}

// NextRunAfter returns the first scheduled fire time strictly after ref.
func NextRunAfter(expr string, ref time.Time) (time.Time, error) {
	sched, err := standardCronParser.Parse(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}
	return sched.Next(ref), nil
}

// DiffMaps returns the subset of desired whose values differ from current.
// Keys are ServerConfig JSON field names. A key missing from current counts
// as different; explicit nil is a meaningful value (clear the field).
func DiffMaps(current, desired map[string]any) map[string]any {
	diff := map[string]any{}
	for k, want := range desired {
		have, ok := current[k]
		if !ok || !reflect.DeepEqual(have, want) {
			diff[k] = want
		}
	}
	return diff
}

// stagedConfigStillMatches reports whether the live values of the keys captured
// at stage time are unchanged. An empty base payload is a legacy row.
func stagedConfigStillMatches(live map[string]any, basePayload string) error {
	if strings.TrimSpace(basePayload) == "" {
		return nil
	}
	var expected map[string]any
	if err := json.Unmarshal([]byte(basePayload), &expected); err != nil {
		return fmt.Errorf("staged base payload is corrupt: %w", err)
	}
	for key, want := range expected {
		have, ok := live[key]
		if !ok {
			have = nil
		}
		if !reflect.DeepEqual(have, want) {
			return fmt.Errorf("%w: %s", ErrStagedConfigMoved, key)
		}
	}
	return nil
}

// MergeMaps returns base with every entry of overlay applied on top.
func MergeMaps(base, overlay map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}

// ServerConfigToMap renders a ServerConfig as a JSON-field-name map so staged
// payloads can be diffed and merged generically.
func ServerConfigToMap(cfg *ServerConfig) (map[string]any, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func validateStagedConfigFields(config *ServerConfig, changes map[string]any) error {
	allowed, err := ServerConfigToMap(config)
	if err != nil {
		return fmt.Errorf("could not inspect server config fields: %w", err)
	}
	for key := range changes {
		if key == "id" || key == "serverId" {
			return fmt.Errorf("server config identity field %q cannot be staged", key)
		}
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unknown server config field %q", key)
		}
	}
	return nil
}

// ErrStagedConfigMoved is returned when a live field changed after it was staged.
var ErrStagedConfigMoved = errors.New("staged config field changed since it was recorded")

// StageConfigChange records changes as a diff vs the live config. It works
// regardless of the instance's operational state — nothing is applied here.
func (s *Store) StageConfigChange(ctx context.Context, serverID string, changes map[string]any, mode StagedApplyMode, cronExpr string) (*StagedConfigChange, error) {
	return s.stageConfigChange(ctx, serverID, changes, stageConfigOptions{Mode: mode, CronExpr: cronExpr})
}

// StageServerConfigUpdate records a config diff on the server's one open batch
// and sets that batch's apply window. Other staged rows in the batch adopt
// the same window. The live config and the container are left unchanged.
func (s *Store) StageServerConfigUpdate(ctx context.Context, serverID string, changes map[string]any, window UpdateWindow, applyAt *time.Time, idleMinutes int) (*StagedConfigChange, error) {
	mode, err := modeForWindow(window, applyAt, idleMinutes)
	if err != nil {
		return nil, err
	}
	return s.stageConfigChange(ctx, serverID, changes, stageConfigOptions{
		Mode:           mode,
		ApplyAt:        applyAt,
		IdleMinutes:    idleMinutes,
		Window:         window,
		SetBatchWindow: true,
	})
}

type stageConfigOptions struct {
	Mode           StagedApplyMode
	CronExpr       string
	ApplyAt        *time.Time
	IdleMinutes    int
	Window         UpdateWindow
	SetBatchWindow bool
}

func modeForWindow(window UpdateWindow, applyAt *time.Time, idleMinutes int) (StagedApplyMode, error) {
	switch window {
	case UpdateWindowNextRestart:
		return StagedApplyOnRestart, nil
	case UpdateWindowAtTime:
		if applyAt == nil || applyAt.IsZero() {
			return "", fmt.Errorf("apply time is required")
		}
		return StagedApplyAtTime, nil
	case UpdateWindowIdle:
		if idleMinutes <= 0 {
			return "", fmt.Errorf("idle minutes must be positive")
		}
		return StagedApplyIdle, nil
	default:
		return "", fmt.Errorf("unknown update window %q", window)
	}
}

func (s *Store) stageConfigChange(ctx context.Context, serverID string, changes map[string]any, opts stageConfigOptions) (*StagedConfigChange, error) {
	mode := opts.Mode
	cronExpr := opts.CronExpr
	if serverID == "" {
		return nil, fmt.Errorf("server id is required")
	}
	if len(changes) == 0 {
		return nil, fmt.Errorf("no changes supplied")
	}
	switch mode {
	case StagedApplyOnRestart, StagedApplyAtTime, StagedApplyIdle:
	case StagedApplyScheduled:
		if err := ValidateCronExpr(cronExpr); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("apply mode must be %q, %q, %q, or %q", StagedApplyOnRestart, StagedApplyScheduled, StagedApplyAtTime, StagedApplyIdle)
	}
	if mode == StagedApplyAtTime && (opts.ApplyAt == nil || opts.ApplyAt.IsZero()) {
		return nil, fmt.Errorf("apply time is required")
	}
	if mode == StagedApplyIdle && opts.IdleMinutes <= 0 {
		return nil, fmt.Errorf("idle minutes must be positive")
	}

	if _, err := s.GetServer(ctx, serverID); err != nil {
		return nil, fmt.Errorf("server not found")
	}

	live, err := s.GetServerConfig(ctx, serverID)
	if err != nil {
		if !errors.Is(err, ErrServerConfigNotFound) {
			return nil, fmt.Errorf("failed to read current server config: %w", err)
		}
		live = s.CreateDefaultServerConfig(serverID)
	}
	current, err := ServerConfigToMap(live)
	if err != nil {
		return nil, fmt.Errorf("failed to read current server config: %w", err)
	}
	if err := validateStagedConfigFields(live, changes); err != nil {
		return nil, err
	}

	diff := DiffMaps(current, changes)
	if len(diff) == 0 {
		return nil, fmt.Errorf("no differences vs current config")
	}
	payload, err := json.Marshal(diff)
	if err != nil {
		return nil, err
	}
	baseSnap := make(map[string]any, len(diff))
	for key := range diff {
		if value, ok := current[key]; ok {
			baseSnap[key] = value
		} else {
			baseSnap[key] = nil
		}
	}
	basePayload, err := json.Marshal(baseSnap)
	if err != nil {
		return nil, err
	}

	window := string(opts.Window)
	if window == "" {
		window = string(mode)
	}
	batch, err := s.ensureOpenBatch(ctx, serverID, window)
	if err != nil {
		return nil, err
	}
	if opts.SetBatchWindow {
		batch.Window = string(opts.Window)
		batch.ApplyAt = opts.ApplyAt
		batch.IdleMinutes = opts.IdleMinutes
		if err := s.db.WithContext(ctx).Save(batch).Error; err != nil {
			return nil, err
		}
		if err := s.db.WithContext(ctx).Model(&StagedConfigChange{}).
			Where("batch_id = ? AND status = ?", batch.ID, string(StagedStatusStaged)).
			Updates(map[string]any{
				"apply_mode":   string(mode),
				"apply_at":     opts.ApplyAt,
				"idle_minutes": opts.IdleMinutes,
			}).Error; err != nil {
			return nil, err
		}
	}

	staged := &StagedConfigChange{
		ID:          uuid.New().String(),
		ServerID:    serverID,
		BatchID:     batch.ID,
		Payload:     string(payload),
		BasePayload: string(basePayload),
		ApplyMode:   mode,
		CronExpr:    cronExpr,
		ApplyAt:     opts.ApplyAt,
		IdleMinutes: opts.IdleMinutes,
		Status:      StagedStatusStaged,
	}
	if err := s.db.WithContext(ctx).Create(staged).Error; err != nil {
		return nil, err
	}
	return staged, nil
}

func (s *Store) ensureOpenBatch(ctx context.Context, serverID, window string) (*ServerUpdateBatch, error) {
	var batch ServerUpdateBatch
	err := s.db.WithContext(ctx).Where("open_key = ?", serverID).First(&batch).Error
	if err == nil {
		return &batch, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	key := serverID
	batch = ServerUpdateBatch{
		ID:       uuid.New().String(),
		ServerID: serverID,
		OpenKey:  &key,
		Window:   window,
		Status:   UpdateBatchOpen,
	}
	if err := s.db.WithContext(ctx).Create(&batch).Error; err != nil {
		if isUniqueViolation(err) {
			if err := s.db.WithContext(ctx).Where("open_key = ?", serverID).First(&batch).Error; err != nil {
				return nil, err
			}
			return &batch, nil
		}
		return nil, err
	}
	return &batch, nil
}

// ListStagedConfigChanges returns staged records for a server, newest first.
// Pass an empty status to list every status.
func (s *Store) ListStagedConfigChanges(ctx context.Context, serverID string, status StagedStatus) ([]*StagedConfigChange, error) {
	var out []*StagedConfigChange
	q := s.db.WithContext(ctx).Where("server_id = ?", serverID).Order("created_at DESC")
	if status != "" {
		q = q.Where("status = ?", string(status))
	}
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// GetStagedConfigChange fetches a single staged record by id.
func (s *Store) GetStagedConfigChange(ctx context.Context, id string) (*StagedConfigChange, error) {
	var staged StagedConfigChange
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&staged).Error; err != nil {
		return nil, fmt.Errorf("staged config change not found")
	}
	return &staged, nil
}

// DiscardStagedConfigChange moves a staged record to discarded.
func (s *Store) DiscardStagedConfigChange(ctx context.Context, id string) error {
	staged, err := s.GetStagedConfigChange(ctx, id)
	if err != nil {
		return err
	}
	if staged.Status != StagedStatusStaged {
		return fmt.Errorf("only staged changes can be discarded")
	}
	staged.Status = StagedStatusDiscarded
	return s.db.WithContext(ctx).Save(staged).Error
}

// StagedConfigConflict reports whether a live field moved after a staged row
// in the batch was recorded. It does not write the live config.
func (s *Store) StagedConfigConflict(ctx context.Context, serverID, batchID string) error {
	var rows []StagedConfigChange
	if err := s.db.WithContext(ctx).
		Where("server_id = ? AND batch_id = ? AND status = ?", serverID, batchID, StagedStatusStaged).
		Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	live := &ServerConfig{}
	err := s.db.WithContext(ctx).Where("server_id = ?", serverID).First(live).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		live = s.CreateDefaultServerConfig(serverID)
	} else if err != nil {
		return err
	}
	base, err := ServerConfigToMap(live)
	if err != nil {
		return err
	}
	for i := range rows {
		if err := stagedConfigStillMatches(base, rows[i].BasePayload); err != nil {
			return err
		}
	}
	return nil
}

// ListBatchConfig returns staged config rows still waiting in one batch, oldest first.
func (s *Store) ListBatchConfig(ctx context.Context, batchID string) ([]*StagedConfigChange, error) {
	var rows []*StagedConfigChange
	err := s.db.WithContext(ctx).
		Where("batch_id = ? AND status = ?", batchID, StagedStatusStaged).
		Order("created_at asc").
		Find(&rows).Error
	return rows, err
}

// ApplyStagedConfigChange merges a staged diff into the live ServerConfig and
// flips the record to applied. Callers decide when to invoke it (restart hook
// or cron tick); container recreation stays the caller's responsibility.
func (s *Store) ApplyStagedConfigChange(ctx context.Context, id string) (*StagedConfigChange, error) {
	var applied *StagedConfigChange
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claim := tx.Model(&StagedConfigChange{}).
			Where("id = ? AND status = ?", id, string(StagedStatusStaged)).
			UpdateColumn("status", "applying")
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			return fmt.Errorf("only staged changes can be applied")
		}

		var staged StagedConfigChange
		if err := tx.Where("id = ?", id).First(&staged).Error; err != nil {
			return fmt.Errorf("staged config change not found: %w", err)
		}

		var diff map[string]any
		if err := json.Unmarshal([]byte(staged.Payload), &diff); err != nil {
			return fmt.Errorf("staged payload is corrupt: %w", err)
		}
		if diff == nil {
			return fmt.Errorf("staged payload must be a JSON object")
		}

		live := &ServerConfig{}
		err := tx.Where("server_id = ?", staged.ServerID).First(live).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			live = s.CreateDefaultServerConfig(staged.ServerID)
		} else if err != nil {
			return fmt.Errorf("failed to read live server config: %w", err)
		}
		if err := validateStagedConfigFields(live, diff); err != nil {
			return err
		}

		base, err := ServerConfigToMap(live)
		if err != nil {
			return err
		}
		if err := stagedConfigStillMatches(base, staged.BasePayload); err != nil {
			return err
		}

		// Capture the previous values of every key touched by diff (SCH-1)
		prevDiff := make(map[string]any, len(diff))
		for k := range diff {
			if v, ok := base[k]; ok {
				prevDiff[k] = v
			} else {
				prevDiff[k] = nil
			}
		}
		prevBytes, err := json.Marshal(prevDiff)
		if err != nil {
			return fmt.Errorf("failed to serialize previous config state: %w", err)
		}
		staged.PreviousPayload = string(prevBytes)

		merged, err := json.Marshal(MergeMaps(base, diff))
		if err != nil {
			return err
		}
		next := &ServerConfig{}
		if err := json.Unmarshal(merged, next); err != nil {
			return fmt.Errorf("merged config is invalid: %w", err)
		}
		// Preserve identity fields regardless of what the payload contained.
		next.ID = live.ID
		next.ServerID = live.ServerID
		if err := tx.Save(next).Error; err != nil {
			return fmt.Errorf("failed to save merged server config: %w", err)
		}

		now := time.Now()
		staged.Status = StagedStatusApplied
		staged.AppliedAt = &now
		if err := tx.Save(&staged).Error; err != nil {
			return fmt.Errorf("failed to mark staged config as applied: %w", err)
		}
		applied = &staged
		return nil
	})
	if err != nil {
		return nil, err
	}
	return applied, nil
}

// RollbackStagedConfigChange reverts an applied staged config change by restoring the
// previous values of all modified fields back onto the live ServerConfig, and marking
// the staged record as rolled_back (SCH-1).
func (s *Store) RollbackStagedConfigChange(ctx context.Context, id string) (*StagedConfigChange, error) {
	var rolledBack *StagedConfigChange
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claim := tx.Model(&StagedConfigChange{}).
			Where("id = ? AND status = ?", id, string(StagedStatusApplied)).
			UpdateColumn("status", "rolling_back")
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			return fmt.Errorf("only applied changes can be rolled back")
		}

		var staged StagedConfigChange
		if err := tx.Where("id = ?", id).First(&staged).Error; err != nil {
			return fmt.Errorf("staged config change not found: %w", err)
		}

		if staged.PreviousPayload == "" {
			return fmt.Errorf("staged config change has no recorded previous state to roll back to")
		}

		var prevDiff map[string]any
		if err := json.Unmarshal([]byte(staged.PreviousPayload), &prevDiff); err != nil {
			return fmt.Errorf("previous staged payload is corrupt: %w", err)
		}
		if prevDiff == nil {
			return fmt.Errorf("previous staged payload must be a JSON object")
		}

		live := &ServerConfig{}
		err := tx.Where("server_id = ?", staged.ServerID).First(live).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			live = s.CreateDefaultServerConfig(staged.ServerID)
		} else if err != nil {
			return fmt.Errorf("failed to read live server config: %w", err)
		}

		base, err := ServerConfigToMap(live)
		if err != nil {
			return err
		}
		merged, err := json.Marshal(MergeMaps(base, prevDiff))
		if err != nil {
			return err
		}
		next := &ServerConfig{}
		if err := json.Unmarshal(merged, next); err != nil {
			return fmt.Errorf("rolled back config is invalid: %w", err)
		}
		// Preserve identity fields regardless of what the payload contained.
		next.ID = live.ID
		next.ServerID = live.ServerID
		if err := tx.Save(next).Error; err != nil {
			return fmt.Errorf("failed to save rolled back server config: %w", err)
		}

		now := time.Now()
		staged.Status = StagedStatusRolledBack
		staged.RolledBackAt = &now
		if err := tx.Save(&staged).Error; err != nil {
			return fmt.Errorf("failed to mark staged config as rolled back: %w", err)
		}
		rolledBack = &staged
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rolledBack, nil
}

// ListDueScheduledStagedChanges returns staged + scheduled records whose cron
// expression has fired at least once since creation and which are therefore
// due for a one-shot apply. Malformed expressions are skipped.
func (s *Store) ListDueScheduledStagedChanges(ctx context.Context, now time.Time) ([]*StagedConfigChange, error) {
	var candidates []*StagedConfigChange
	if err := s.db.WithContext(ctx).
		Where("status = ? AND apply_mode = ?", string(StagedStatusStaged), string(StagedApplyScheduled)).
		Find(&candidates).Error; err != nil {
		return nil, err
	}

	var due []*StagedConfigChange
	for _, c := range candidates {
		next, err := NextRunAfter(c.CronExpr, c.CreatedAt)
		if err != nil {
			continue
		}
		if !next.After(now) {
			due = append(due, c)
		}
	}
	return due, nil
}
