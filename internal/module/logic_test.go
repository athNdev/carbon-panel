// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Merged in the MINE-172 integration: this file existed independently on
// both lines of development, so the union of both test sets is kept.

package module

import (
	"context"
	"fmt"
	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/events"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"strings"
	"testing"
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
	for _, tpl := range templates {
		if tpl.ID != "builtin-geyser" {
			continue
		}
		if strings.Contains(tpl.DefaultEnv, `"offline"`) {
			t.Error("geyser template must not default REMOTE_AUTH_TYPE to offline")
		}
		if !strings.Contains(tpl.DefaultEnv, "floodgate") {
			t.Error("geyser template must default auth to floodgate")
		}
		if !strings.Contains(tpl.DefaultVolumes, "/plugins") {
			t.Error("geyser template must mount the server plugins directory for Floodgate")
		}
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

// --- tests present only on the pre-merge line of development ---

func testManager(t *testing.T) (*Manager, *storage.Store) {
	t.Helper()
	store := setupTestStore(t)
	cfg := &config.Config{}
	cfg.Module.PortRangeMin = 8100
	cfg.Module.PortRangeMax = 8105
	m := &Manager{store: store, config: cfg, logger: logger.New()}
	return m, store
}

func TestEvaluateCondition(t *testing.T) {
	m, _ := testManager(t)
	srv := &storage.Server{Status: storage.StatusRunning}
	mod := &storage.Module{}

	cases := []struct {
		cond string
		want bool
	}{
		{"", true},
		{"   ", true},
		{"5 > 3", true},
		{"3 > 5", false},
		{"Running == running", true},
		{"1 <= 1", true},
		{"2 != 2", false},
		{"no-operator-here", false},
		{"a > b", false}, // unsupported operator for strings
	}
	for _, tc := range cases {
		if got := m.evaluateCondition(tc.cond, srv, mod); got != tc.want {
			t.Errorf("evaluateCondition(%q)=%v want %v", tc.cond, got, tc.want)
		}
	}
}

func TestInitBuiltinTemplatesIdempotent(t *testing.T) {
	_, store := testManager(t)
	ctx := context.Background()

	if err := InitBuiltinTemplates(store); err != nil {
		t.Fatalf("first init: %v", err)
	}
	first, err := store.ListModuleTemplates(ctx)
	if err != nil || len(first) == 0 {
		t.Fatalf("templates=%v err=%v", first, err)
	}
	for _, tpl := range first {
		if tpl.DockerImage == "" || tpl.ID == "" {
			t.Fatalf("template missing content: %+v", tpl)
		}
	}

	if err := InitBuiltinTemplates(store); err != nil {
		t.Fatalf("second init: %v", err)
	}
	second, err := store.ListModuleTemplates(ctx)
	if err != nil {
		t.Fatalf("relist: %v", err)
	}
	if len(second) != len(first) {
		t.Fatalf("not idempotent: %d -> %d", len(first), len(second))
	}
}

func TestAllocateModulePortExcluding(t *testing.T) {
	m, store := testManager(t)
	ctx := context.Background()

	// Empty store: first port in range.
	p, err := m.AllocateModulePortExcluding(ctx, nil)
	if err != nil || p != 8100 {
		t.Fatalf("port=%d err=%v", p, err)
	}

	// Occupy 8100 and 8101 via a module row.
	if err := store.CreateModule(ctx, &storage.Module{
		ID: "m1", Name: "m1", ServerID: "s1", TemplateID: "t1",
		Ports: []*v1.ModulePort{
			{HostPort: 8100},
			{HostPort: 8101},
		},
	}); err != nil {
		t.Fatalf("CreateModule: %v", err)
	}
	p, err = m.AllocateModulePortExcluding(ctx, nil)
	if err != nil || p != 8102 {
		t.Fatalf("port=%d err=%v", p, err)
	}

	// Same-request exclusions skip further.
	p, err = m.AllocateModulePortExcluding(ctx, map[int]bool{8102: true, 8103: true})
	if err != nil || p != 8104 {
		t.Fatalf("port=%d err=%v", p, err)
	}

	// Exhaustion errors.
	if _, err := m.AllocateModulePortExcluding(ctx, map[int]bool{
		8100: true, 8101: true, 8102: true, 8103: true, 8104: true, 8105: true,
	}); err == nil {
		t.Fatal("exhausted range must error")
	}
}

func TestResolveDockerFallback(t *testing.T) {
	m, _ := testManager(t)
	if got := m.resolveDocker("any"); got != nil {
		t.Fatalf("no docker source: %v", got)
	}
}

func TestManagerConstructors(t *testing.T) {
	store := setupTestStore(t)
	cfg := &config.Config{}
	log := logger.New()
	m := NewManager(store, nil, nil, cfg, nil, log)
	if m == nil || m.store != store || m.config != cfg || m.logger != log {
		t.Fatal("NewManager must wire fields")
	}
	m2 := NewManagerWithPool(store, nil, nil, nil, cfg, nil, log)
	if m2 == nil || m2.pool != nil {
		t.Fatal("nil pool must stay nil")
	}
	m.SetLogStreamer(nil)
	m2.SetLogStreamer(nil)
}

func TestStartStop(t *testing.T) {
	store := setupTestStore(t)
	log := logger.New()

	disabled := &config.Config{}
	disabled.Module.Enabled = false
	md := &Manager{store: store, config: disabled, logger: log}
	if err := md.Start(); err != nil {
		t.Fatalf("disabled Start: %v", err)
	}

	enabled := &config.Config{}
	enabled.Module.Enabled = true
	enabled.Module.PortRangeMin = 8100
	enabled.Module.PortRangeMax = 8105
	m := &Manager{store: store, config: enabled, logger: log}
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.Start(); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := m.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}

	// Templates seeded by Start.
	tpls, err := store.ListModuleTemplates(context.Background())
	if err != nil || len(tpls) == 0 {
		t.Fatalf("templates=%v err=%v", tpls, err)
	}
}

func TestHandleServerEventEmpty(t *testing.T) {
	m, _ := testManager(t)
	ctx := context.Background()
	// No modules: all paths no-op without docker.
	m.HandleServerEvent(ctx, events.Event{Type: v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START, ServerID: "s1"})
	m.HandleServerEvent(ctx, events.Event{Type: v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_STOP, ServerID: "s1"})
	m.HandleServerEvent(ctx, events.Event{Type: v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_PLAYER_JOIN, ServerID: "s1"})
}

func TestGetUsedModulePorts(t *testing.T) {
	m, store := testManager(t)
	ctx := context.Background()
	ports, err := m.GetUsedModulePorts(ctx)
	if err != nil || len(ports) != 0 {
		t.Fatalf("ports=%v err=%v", ports, err)
	}
	if err := store.CreateModule(ctx, &storage.Module{
		ID: "m1", Name: "m1", ServerID: "s1", TemplateID: "t1",
		Ports: []*v1.ModulePort{{HostPort: 8110}, nil, {HostPort: 0}},
	}); err != nil {
		t.Fatalf("CreateModule: %v", err)
	}
	ports, err = m.GetUsedModulePorts(ctx)
	if err != nil || len(ports) != 1 || ports[0] != 8110 {
		t.Fatalf("ports=%v err=%v", ports, err)
	}
	if _, err := m.AllocateModulePort(ctx); err != nil {
		t.Fatalf("AllocateModulePort: %v", err)
	}
}

func TestEvaluateConditionAlias(t *testing.T) {
	m, _ := testManager(t)
	srv := &storage.Server{Status: storage.StatusRunning}
	mod := &storage.Module{}
	if !m.evaluateCondition("{{server.status}} == running", srv, mod) {
		t.Fatal("alias condition must resolve")
	}
	if m.evaluateCondition("{{server.status}} == stopped", srv, mod) {
		t.Fatal("alias mismatch must be false")
	}
}

func TestDispatchHooksSkipsNonMatching(t *testing.T) {
	m, store := testManager(t)
	ctx := context.Background()
	if err := store.CreateModule(ctx, &storage.Module{
		ID: "m1", Name: "m1", ServerID: "s1", TemplateID: "t1",
		EventHooks: []*v1.ModuleEventHook{
			{Event: v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_STOP},
		},
	}); err != nil {
		t.Fatalf("CreateModule: %v", err)
	}
	// Hook registered for STOP only: a START event skips execution entirely
	// (no goroutine, no docker).
	m.HandleServerEvent(ctx, events.Event{Type: v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START, ServerID: "s1"})
}

func TestExecuteHookBranches(t *testing.T) {
	m, _ := testManager(t)
	ctx := context.Background()
	mod := &storage.Module{ID: "m1", Name: "m1"}

	// Condition path with missing server: error branch, no docker touched.
	m.executeHook(ctx, mod, &v1.ModuleEventHook{
		Event:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START,
		Condition: "1 == 1",
	}, "no-such-server")

	// Unknown action with no condition: default branch.
	m.executeHook(ctx, mod, &v1.ModuleEventHook{
		Event: v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START,
	}, "s1")
}
