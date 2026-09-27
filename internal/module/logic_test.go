package module

import (
	"context"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/events"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

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
