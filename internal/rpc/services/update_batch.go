// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"connectrpc.com/connect"
	storage "github.com/athNdev/carbon-panel/internal/db"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"gorm.io/gorm"
)

// GetUpdateBatch returns the server's open staged batch and the items in it.
func (s *ConfigService) GetUpdateBatch(ctx context.Context, req *connect.Request[v1.GetUpdateBatchRequest]) (*connect.Response[v1.GetUpdateBatchResponse], error) {
	if _, err := s.store.GetServer(ctx, req.Msg.GetServerId()); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}
	batch, err := s.store.OpenUpdateBatch(ctx, req.Msg.GetServerId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if batch == nil {
		return connect.NewResponse(&v1.GetUpdateBatchResponse{}), nil
	}
	items, err := s.batchItems(ctx, batch.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &v1.GetUpdateBatchResponse{
		Open:        true,
		Window:      protoApplyWindow(batch.Window),
		IdleMinutes: int32(batch.IdleMinutes),
		Items:       items,
	}
	if batch.ApplyAt != nil {
		resp.ApplyAtUnix = batch.ApplyAt.Unix()
	}
	return connect.NewResponse(resp), nil
}

// DiscardUpdateBatch drops one staged item, or the whole batch when no item is named.
func (s *ConfigService) DiscardUpdateBatch(ctx context.Context, req *connect.Request[v1.DiscardUpdateBatchRequest]) (*connect.Response[v1.DiscardUpdateBatchResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.GetServerId())
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}
	if req.Msg.GetItemId() == "" {
		if err := s.discardWholeBatch(ctx, server); err != nil {
			return nil, err
		}
		return connect.NewResponse(&v1.DiscardUpdateBatchResponse{}), nil
	}
	if err := s.discardBatchItem(ctx, server, req.Msg.GetItemId()); err != nil {
		return nil, err
	}
	if err := s.closeBatchIfEmpty(ctx, server.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.DiscardUpdateBatchResponse{}), nil
}

func (s *ConfigService) batchItems(ctx context.Context, batchID string) ([]*v1.StagedUpdateItem, error) {
	configs, err := s.store.ListBatchConfig(ctx, batchID)
	if err != nil {
		return nil, err
	}
	content, err := s.store.ListBatchContent(ctx, batchID)
	if err != nil {
		return nil, err
	}
	items := make([]*v1.StagedUpdateItem, 0, len(configs)+len(content))
	for _, cfg := range configs {
		items = append(items, &v1.StagedUpdateItem{
			Id:      cfg.ID,
			Kind:    "config",
			Action:  "update",
			Summary: configDiffSummary(cfg.Payload),
		})
	}
	for _, row := range content {
		items = append(items, &v1.StagedUpdateItem{
			Id:      row.ID,
			Kind:    row.Kind,
			Action:  row.Action,
			Summary: row.Action + " " + row.FileName,
		})
	}
	return items, nil
}

func (s *ConfigService) discardWholeBatch(ctx context.Context, server *storage.Server) error {
	batch, err := s.store.OpenUpdateBatch(ctx, server.ID)
	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}
	if batch == nil {
		return nil
	}
	content, err := s.store.ListBatchContent(ctx, batch.ID)
	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}
	for i := range content {
		if err := discardStagedContent(ctx, s.store, server, content[i].ID); err != nil {
			return connect.NewError(connect.CodeInternal, err)
		}
	}
	configs, err := s.store.ListBatchConfig(ctx, batch.ID)
	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}
	for _, cfg := range configs {
		if err := s.store.DiscardStagedConfigChange(ctx, cfg.ID); err != nil {
			return connect.NewError(connect.CodeInternal, err)
		}
	}
	if err := s.store.CloseUpdateBatch(ctx, batch.ID, storage.UpdateBatchDiscarded); err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}
	return nil
}

func (s *ConfigService) discardBatchItem(ctx context.Context, server *storage.Server, id string) error {
	row, err := s.store.GetStagedContent(ctx, id)
	if err == nil {
		if row.ServerID != server.ID || row.Status != storage.StagedStatusStaged {
			return connect.NewError(connect.CodeNotFound, errors.New("staged item not found"))
		}
		if err := discardStagedContent(ctx, s.store, server, id); err != nil {
			return connect.NewError(connect.CodeInternal, err)
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return connect.NewError(connect.CodeInternal, err)
	}
	cfg, err := s.store.GetStagedConfigChange(ctx, id)
	if err != nil || cfg.ServerID != server.ID || cfg.Status != storage.StagedStatusStaged {
		return connect.NewError(connect.CodeNotFound, errors.New("staged item not found"))
	}
	if err := s.store.DiscardStagedConfigChange(ctx, id); err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}
	return nil
}

func (s *ConfigService) closeBatchIfEmpty(ctx context.Context, serverID string) error {
	batch, err := s.store.OpenUpdateBatch(ctx, serverID)
	if err != nil || batch == nil {
		return err
	}
	content, err := s.store.ListBatchContent(ctx, batch.ID)
	if err != nil {
		return err
	}
	configs, err := s.store.ListBatchConfig(ctx, batch.ID)
	if err != nil {
		return err
	}
	if len(content) > 0 || len(configs) > 0 {
		return nil
	}
	return s.store.CloseUpdateBatch(ctx, batch.ID, storage.UpdateBatchDiscarded)
}

func configDiffSummary(payload string) string {
	var diff map[string]any
	if err := json.Unmarshal([]byte(payload), &diff); err != nil || len(diff) == 0 {
		return "config update"
	}
	keys := make([]string, 0, len(diff))
	for key := range diff {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return "config: " + strings.Join(keys, ", ")
}

func protoApplyWindow(window string) v1.ConfigApplyWindow {
	switch storage.UpdateWindow(window) {
	case storage.UpdateWindowAtTime:
		return v1.ConfigApplyWindow_CONFIG_APPLY_WINDOW_AT_TIME
	case storage.UpdateWindowIdle:
		return v1.ConfigApplyWindow_CONFIG_APPLY_WINDOW_IDLE
	default:
		return v1.ConfigApplyWindow_CONFIG_APPLY_WINDOW_NEXT_RESTART
	}
}
