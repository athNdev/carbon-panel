// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package module

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/events"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func newLogicTestManager(t *testing.T, cfg *config.Config) (*Manager, *storage.Store) {
	t.Helper()

	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	store, err := storage.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:           fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName),
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	return NewManager(store, nil, nil, cfg, nil, logger.New()), store
}

func TestEvaluateCondition_Operators(t *testing.T) {
	m, _ := newLogicTestManager(t, nil)
	server := &storage.Server{ID: "srv-1", Name: "Survival"}
	module := &storage.Module{ID: "mod-1", Name: "Geyser"}

	cases := []struct {
		name      string
		condition string
		want      bool
	}{
		{"empty is true", "", true},
		{"literal equality", "1 == 1", true},
		{"literal inequality", "a != b", true},
		{"numeric greater", "10 > 2", true},
		{"numeric less false", "2 > 10", false},
		{"numeric gte", "10 >= 10", true},
		{"numeric lte", "3 <= 4", true},
		{"string equality ci", "Java == java", true},
		{"no operator is false", "just some words", false},
		{"server alias", "{{server.id}} == srv-1", true},
		{"server alias mismatch", "{{server.id}} == srv-2", false},
		{"module alias", "{{module.name}} == Geyser", true},
		{"numeric alias", "{{module.memory}} >= 100", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			module.Memory = 512
			if got := m.evaluateCondition(tc.condition, server, module); got != tc.want {
				t.Errorf("evaluateCondition(%q) = %v, want %v", tc.condition, got, tc.want)
			}
		})
	}
}

func TestInitBuiltinTemplates_SeedsAndIdempotent(t *testing.T) {
	_, store := newLogicTestManager(t, nil)
	ctx := context.Background()

	if err := InitBuiltinTemplates(store); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	templates, err := store.ListModuleTemplates(ctx)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(templates) == 0 {
		t.Fatal("no builtin templates seeded")
	}
	byID := make(map[string]bool, len(templates))
	for _, tpl := range templates {
		byID[tpl.ID] = true
		if tpl.DockerImage == "" {
			t.Errorf("template %q has no docker image", tpl.ID)
		}
	}
	if !byID["builtin-geyser"] {
		t.Error("builtin-geyser template missing")
	}

	countBefore := len(templates)
	if err := InitBuiltinTemplates(store); err != nil {
		t.Fatalf("reseed failed: %v", err)
	}
	templates, err = store.ListModuleTemplates(ctx)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(templates) != countBefore {
		t.Errorf("reseed changed template count %d -> %d, want idempotent", countBefore, len(templates))
	}
}

func TestNewManagerWithPool_CreatesPool(t *testing.T) {
	m, _ := newLogicTestManager(t, nil)
	if m.pool != nil {
		t.Error("expected nil pool when no docker client is given")
	}
	pooled := NewManagerWithPool(nil, &docker.Client{}, nil, nil, &config.Config{}, nil, logger.New())
	if pooled.pool == nil {
		t.Error("expected pool to be created around the docker client")
	}
}

func TestAllocateModulePort_SkipsUsedAndExcluded(t *testing.T) {
	cfg := &config.Config{}
	cfg.Module.PortRangeMin = 30000
	cfg.Module.PortRangeMax = 30002
	m, store := newLogicTestManager(t, cfg)
	ctx := context.Background()

	// Occupy 30000 via an existing module.
	if err := store.DB().Create(&storage.Module{
		ID:         "mod-used",
		Name:       "used",
		ServerID:   "srv-1",
		TemplateID: "tpl",
		Ports:      []*v1.ModulePort{{Name: "web", ContainerPort: 80, HostPort: 30000}},
	}).Error; err != nil {
		t.Fatalf("failed to insert module: %v", err)
	}

	port, err := m.AllocateModulePortExcluding(ctx, map[int]bool{30001: true})
	if err != nil {
		t.Fatalf("allocate failed: %v", err)
	}
	if port != 30002 {
		t.Errorf("allocated port = %d, want 30002 (30000 used, 30001 excluded)", port)
	}

	// Exhaust the range: everything used or excluded.
	if _, err := m.AllocateModulePortExcluding(ctx, map[int]bool{30001: true, 30002: true}); err == nil {
		t.Error("expected exhaustion error, got nil")
	}
}

func TestLifecycleHooks_NoModulesIsNoOp(t *testing.T) {
	m, _ := newLogicTestManager(t, nil)
	ctx := context.Background()

	m.autoStartModules(ctx, "srv-missing")
	m.stopLifecycleModules(ctx, "srv-missing")
	m.dispatchHooks(ctx, "srv-missing", v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START)
	m.HandleServerEvent(ctx, events.Event{Type: v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START})
}

func TestModuleNotFound_Paths(t *testing.T) {
	m, _ := newLogicTestManager(t, nil)
	ctx := context.Background()

	if err := m.StopModule(ctx, "nope"); err == nil {
		t.Error("StopModule unknown module: want error")
	}
	if err := m.DeleteModule(ctx, "nope"); err == nil {
		t.Error("DeleteModule unknown module: want error")
	}
	status, err := m.GetModuleStatus(ctx, "nope")
	if err == nil {
		t.Error("GetModuleStatus unknown module: want error")
	}
	if status != storage.ModuleStatusError {
		t.Errorf("GetModuleStatus unknown = %q, want error status", status)
	}
}

func TestExecuteHook_UnknownActionWarns(t *testing.T) {
	m, _ := newLogicTestManager(t, nil)
	ctx := context.Background()

	module := &storage.Module{ID: "mod-hook", Name: "hookmod"}
	m.executeHook(ctx, module, &v1.ModuleEventHook{
		Event:  v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START,
		Action: v1.ModuleEventAction_MODULE_EVENT_ACTION_UNSPECIFIED,
	}, "srv-1")
}

func TestExecuteHook_ConditionGating(t *testing.T) {
	m, store := newLogicTestManager(t, nil)
	ctx := context.Background()

	if err := store.DB().Create(&storage.Server{
		ID:       "srv-cond",
		Name:     "Cond",
		DataPath: t.TempDir(),
	}).Error; err != nil {
		t.Fatalf("failed to insert server: %v", err)
	}
	module := &storage.Module{ID: "mod-cond", Name: "condmod", ServerID: "srv-cond"}

	// False condition: hook must not execute (no docker touched).
	m.executeHook(ctx, module, &v1.ModuleEventHook{
		Event:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START,
		Action:    v1.ModuleEventAction_MODULE_EVENT_ACTION_UNSPECIFIED,
		Condition: "1 == 2",
	}, "srv-cond")

	// True condition with unknown action: falls through to warn branch.
	m.executeHook(ctx, module, &v1.ModuleEventHook{
		Event:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START,
		Action:    v1.ModuleEventAction_MODULE_EVENT_ACTION_UNSPECIFIED,
		Condition: "1 == 1",
	}, "srv-cond")

	// Missing server for condition: logged and skipped, no panic.
	m.executeHook(ctx, module, &v1.ModuleEventHook{
		Event:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START,
		Action:    v1.ModuleEventAction_MODULE_EVENT_ACTION_UNSPECIFIED,
		Condition: "1 == 1",
	}, "srv-missing")
}

func TestDispatchHooks_SpawnsMatchingHook(t *testing.T) {
	m, store := newLogicTestManager(t, nil)
	ctx := context.Background()

	if err := store.DB().Create(&storage.Module{
		ID:         "mod-dispatch",
		Name:       "dispatch",
		ServerID:   "srv-1",
		TemplateID: "tpl",
		EventHooks: []*v1.ModuleEventHook{
			{
				Event:  v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START,
				Action: v1.ModuleEventAction_MODULE_EVENT_ACTION_UNSPECIFIED,
			},
			{
				// Wrong event: must be skipped.
				Event:  v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_STOP,
				Action: v1.ModuleEventAction_MODULE_EVENT_ACTION_UNSPECIFIED,
			},
		},
	}).Error; err != nil {
		t.Fatalf("failed to insert module: %v", err)
	}

	m.dispatchHooks(ctx, "srv-1", v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START)
}
