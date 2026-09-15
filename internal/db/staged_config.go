package db

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
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

// StageConfigChange records changes as a diff vs the live config. It works
// regardless of the instance's operational state — nothing is applied here.
func (s *Store) StageConfigChange(ctx context.Context, serverID string, changes map[string]any, mode StagedApplyMode, cronExpr string) (*StagedConfigChange, error) {
	if serverID == "" {
		return nil, fmt.Errorf("server id is required")
	}
	if len(changes) == 0 {
		return nil, fmt.Errorf("no changes supplied")
	}
	if mode != StagedApplyOnRestart && mode != StagedApplyScheduled {
		return nil, fmt.Errorf("apply mode must be %q or %q", StagedApplyOnRestart, StagedApplyScheduled)
	}
	if mode == StagedApplyScheduled {
		if err := ValidateCronExpr(cronExpr); err != nil {
			return nil, err
		}
	}

	if _, err := s.GetServer(ctx, serverID); err != nil {
		return nil, fmt.Errorf("server not found")
	}

	current := map[string]any{}
	if live, err := s.GetServerConfig(ctx, serverID); err == nil && live != nil {
		if m, merr := ServerConfigToMap(live); merr == nil {
			current = m
		}
	}

	diff := DiffMaps(current, changes)
	if len(diff) == 0 {
		return nil, fmt.Errorf("no differences vs current config")
	}
	payload, err := json.Marshal(diff)
	if err != nil {
		return nil, err
	}

	staged := &StagedConfigChange{
		ID:        uuid.New().String(),
		ServerID:  serverID,
		Payload:   string(payload),
		ApplyMode: mode,
		CronExpr:  cronExpr,
		Status:    StagedStatusStaged,
	}
	if err := s.db.WithContext(ctx).Create(staged).Error; err != nil {
		return nil, err
	}
	return staged, nil
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

// ApplyStagedConfigChange merges a staged diff into the live ServerConfig and
// flips the record to applied. Callers decide when to invoke it (restart hook
// or cron tick); container recreation stays the caller's responsibility.
func (s *Store) ApplyStagedConfigChange(ctx context.Context, id string) (*StagedConfigChange, error) {
	staged, err := s.GetStagedConfigChange(ctx, id)
	if err != nil {
		return nil, err
	}
	if staged.Status != StagedStatusStaged {
		return nil, fmt.Errorf("only staged changes can be applied")
	}

	var diff map[string]any
	if err := json.Unmarshal([]byte(staged.Payload), &diff); err != nil {
		return nil, fmt.Errorf("staged payload is corrupt: %w", err)
	}

	live, err := s.GetServerConfig(ctx, staged.ServerID)
	if err != nil {
		live = s.CreateDefaultServerConfig(staged.ServerID)
	}
	base, err := ServerConfigToMap(live)
	if err != nil {
		return nil, err
	}
	merged, err := json.Marshal(MergeMaps(base, diff))
	if err != nil {
		return nil, err
	}
	next := &ServerConfig{}
	if err := json.Unmarshal(merged, next); err != nil {
		return nil, fmt.Errorf("merged config is invalid: %w", err)
	}
	// Preserve identity fields regardless of what the payload contained.
	next.ID = live.ID
	next.ServerID = live.ServerID

	if err := s.SaveServerConfig(ctx, next); err != nil {
		return nil, err
	}

	now := time.Now()
	staged.Status = StagedStatusApplied
	staged.AppliedAt = &now
	if err := s.db.WithContext(ctx).Save(staged).Error; err != nil {
		return nil, err
	}
	return staged, nil
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
