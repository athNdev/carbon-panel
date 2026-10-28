// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package staging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/google/uuid"
)

// ErrStagedModuleMoved means a module field changed after it was staged.
var ErrStagedModuleMoved = errors.New("staged module field changed after it was staged")

// ModuleField is one staged module column. Base is the live value at stage
// time. Value is what apply writes when Base still matches.
type ModuleField struct {
	Base  any `json:"base"`
	Value any `json:"value"`
}

type stagedModuleFile struct {
	ModuleID      string                 `json:"module_id"`
	NeedsRecreate bool                   `json:"needs_recreate"`
	Fields        map[string]ModuleField `json:"fields"`
}

// StageModuleChange records a module config or container-spec change on the
// server's open batch. The live module row and its container stay as they are.
func StageModuleChange(ctx context.Context, store *storage.Store, server *storage.Server, moduleID, summary string, needsRecreate bool, fields map[string]ModuleField) (*storage.StagedContentChange, error) {
	if store == nil || server == nil || server.ID == "" || moduleID == "" || len(fields) == 0 {
		return nil, fmt.Errorf("module stage requires a store, a server, a module, and a change")
	}
	for name := range fields {
		if _, err := moduleFieldValue(name, fields[name].Value); err != nil {
			return nil, err
		}
	}
	acc, err := openAccessor(ctx, store, server)
	if err != nil {
		return nil, err
	}
	defer func() { _ = acc.Close() }()

	id := uuid.NewString()
	body, err := json.Marshal(stagedModuleFile{
		ModuleID:      moduleID,
		NeedsRecreate: needsRecreate,
		Fields:        fields,
	})
	if err != nil {
		return nil, err
	}
	stagePath := stagedContentRoot + "/" + id + "/module.json"
	if err := acc.WriteFile(stagePath, body); err != nil {
		return nil, err
	}
	if summary == "" {
		summary = "module " + moduleID
	}
	row, err := store.AddStagedContent(ctx, &storage.StagedContentChange{
		ID:         id,
		ServerID:   server.ID,
		Kind:       storage.StagedKindModule,
		Action:     storage.StagedActionReplace,
		TargetPath: "module/" + moduleID,
		StagePath:  stagePath,
		FileName:   summary,
	})
	if err != nil {
		_ = acc.RemoveAll(stagedContentRoot + "/" + id)
		return nil, err
	}
	return row, nil
}

// applyStagedModules writes staged module fields onto the live rows. The
// returned function puts those fields back. Module containers are not started
// here; the caller recreates the returned ids.
func applyStagedModules(ctx context.Context, store *storage.Store, server *storage.Server, rows []storage.StagedContentChange) ([]string, func() error, error) {
	var moduleRows []storage.StagedContentChange
	for _, row := range rows {
		if row.Kind == storage.StagedKindModule {
			moduleRows = append(moduleRows, row)
		}
	}
	if len(moduleRows) == 0 {
		return nil, nil, nil
	}
	acc, err := openAccessor(ctx, store, server)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = acc.Close() }()

	type prior struct {
		module *storage.Module
	}
	applied := make([]prior, 0, len(moduleRows))
	ids := make([]string, 0, len(moduleRows))
	undo := func() error {
		var failed error
		for i := len(applied) - 1; i >= 0; i-- {
			if err := store.UpdateModule(ctx, applied[i].module); err != nil {
				failed = errors.Join(failed, err)
			}
		}
		return failed
	}
	for _, row := range moduleRows {
		payload, err := readModulePayload(acc, row.StagePath)
		if err != nil {
			_ = undo()
			return nil, nil, err
		}
		module, err := store.GetModule(ctx, payload.ModuleID)
		if err != nil {
			_ = undo()
			return nil, nil, err
		}
		if module.ServerID != server.ID {
			_ = undo()
			return nil, nil, fmt.Errorf("staged module %s is not on this server", payload.ModuleID)
		}
		before := *module
		if err := checkModuleBase(module, payload.Fields); err != nil {
			_ = undo()
			return nil, nil, err
		}
		if err := assignModuleFields(module, payload.Fields); err != nil {
			_ = undo()
			return nil, nil, err
		}
		if err := store.UpdateModule(ctx, module); err != nil {
			_ = undo()
			return nil, nil, err
		}
		applied = append(applied, prior{module: &before})
		if payload.NeedsRecreate {
			ids = append(ids, module.ID)
		}
	}
	return ids, undo, nil
}

func readModulePayload(acc interface {
	ReadFile(string) ([]byte, error)
}, stagePath string) (stagedModuleFile, error) {
	var payload stagedModuleFile
	data, err := acc.ReadFile(stagePath)
	if err != nil {
		return payload, err
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return payload, err
	}
	if payload.ModuleID == "" || len(payload.Fields) == 0 {
		return payload, fmt.Errorf("staged module payload is empty")
	}
	return payload, nil
}

func checkModuleBase(module *storage.Module, fields map[string]ModuleField) error {
	for name, field := range fields {
		current, err := moduleFieldValue(name, currentModuleField(module, name))
		if err != nil {
			return err
		}
		base, err := moduleFieldValue(name, field.Base)
		if err != nil {
			return err
		}
		if current != base {
			return fmt.Errorf("%w: %s", ErrStagedModuleMoved, name)
		}
	}
	return nil
}

func assignModuleFields(module *storage.Module, fields map[string]ModuleField) error {
	for name, field := range fields {
		value, err := moduleFieldValue(name, field.Value)
		if err != nil {
			return err
		}
		switch name {
		case "config":
			module.Config = value.(string)
		case "env_overrides":
			module.EnvOverrides = value.(string)
		case "volume_overrides":
			module.VolumeOverrides = value.(string)
		case "memory":
			module.Memory = value.(int)
		case "cpu_limit":
			module.CPULimit = value.(float64)
		case "cmd_override":
			module.CmdOverride = value.(string)
		case "uid":
			module.UID = value.(string)
		case "gid":
			module.GID = value.(string)
		case "name":
			module.Name = value.(string)
		case "auto_start":
			module.AutoStart = value.(bool)
		case "follow_server_lifecycle":
			module.FollowServerLifecycle = value.(bool)
		case "detached":
			module.Detached = value.(bool)
		case "init_command":
			module.InitCommand = value.(string)
		case "init_command_delay":
			module.InitCommandDelay = value.(int)
		case "restart_after_init":
			module.RestartAfterInit = value.(bool)
		default:
			return fmt.Errorf("module field %q cannot be staged", name)
		}
	}
	return nil
}

func currentModuleField(module *storage.Module, name string) any {
	switch name {
	case "config":
		return module.Config
	case "env_overrides":
		return module.EnvOverrides
	case "volume_overrides":
		return module.VolumeOverrides
	case "memory":
		return module.Memory
	case "cpu_limit":
		return module.CPULimit
	case "cmd_override":
		return module.CmdOverride
	case "uid":
		return module.UID
	case "gid":
		return module.GID
	case "name":
		return module.Name
	case "auto_start":
		return module.AutoStart
	case "follow_server_lifecycle":
		return module.FollowServerLifecycle
	case "detached":
		return module.Detached
	case "init_command":
		return module.InitCommand
	case "init_command_delay":
		return module.InitCommandDelay
	case "restart_after_init":
		return module.RestartAfterInit
	default:
		return nil
	}
}

func moduleFieldValue(name string, value any) (any, error) {
	switch name {
	case "config", "env_overrides", "volume_overrides", "cmd_override", "uid", "gid", "name", "init_command":
		switch typed := value.(type) {
		case string:
			return typed, nil
		case nil:
			return "", nil
		default:
			return nil, fmt.Errorf("module field %q must be text", name)
		}
	case "memory", "init_command_delay":
		switch typed := value.(type) {
		case int:
			return typed, nil
		case int32:
			return int(typed), nil
		case int64:
			return int(typed), nil
		case float64:
			return int(typed), nil
		case json.Number:
			n, err := typed.Int64()
			if err != nil {
				return nil, err
			}
			return int(n), nil
		default:
			return nil, fmt.Errorf("module field %q must be a number", name)
		}
	case "cpu_limit":
		switch typed := value.(type) {
		case float64:
			return typed, nil
		case int:
			return float64(typed), nil
		case json.Number:
			return typed.Float64()
		default:
			return nil, fmt.Errorf("module field %q must be a number", name)
		}
	case "auto_start", "follow_server_lifecycle", "detached", "restart_after_init":
		switch typed := value.(type) {
		case bool:
			return typed, nil
		default:
			return nil, fmt.Errorf("module field %q must be true or false", name)
		}
	default:
		return nil, fmt.Errorf("module field %q cannot be staged", name)
	}
}
