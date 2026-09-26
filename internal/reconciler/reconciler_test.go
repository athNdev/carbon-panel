package reconciler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"net/netip"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/events"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// --- fakes -----------------------------------------------------------------

type statusUpdate struct {
	id     string
	status db.ServerStatus
}

type containerUpdate struct {
	id          string
	containerID string
}

type fakeStore struct {
	mu         sync.Mutex
	servers    map[string]*db.Server
	configs    map[string]*db.ServerConfig
	statuses   []statusUpdate
	containers []containerUpdate
}

func newFakeStore(servers ...*db.Server) *fakeStore {
	f := &fakeStore{servers: map[string]*db.Server{}, configs: map[string]*db.ServerConfig{}}
	for _, s := range servers {
		f.servers[s.ID] = s
		f.configs[s.ID] = &db.ServerConfig{ServerID: s.ID}
	}
	return f
}

func (f *fakeStore) GetServer(_ context.Context, id string) (*db.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.servers[id]
	if !ok {
		return nil, fmt.Errorf("server not found")
	}
	cp := *s
	return &cp, nil
}

func (f *fakeStore) ListServers(_ context.Context) ([]*db.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*db.Server, 0, len(f.servers))
	for _, s := range f.servers {
		cp := *s
		out = append(out, &cp)
	}
	return out, nil
}

func (f *fakeStore) GetServerConfig(_ context.Context, serverID string) (*db.ServerConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.configs[serverID]
	if !ok {
		return nil, fmt.Errorf("server config not found")
	}
	cp := *c
	return &cp, nil
}

func (f *fakeStore) UpdateServerStatus(_ context.Context, id string, status db.ServerStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses = append(f.statuses, statusUpdate{id: id, status: status})
	if s, ok := f.servers[id]; ok {
		s.Status = status
	}
	return nil
}

func (f *fakeStore) UpdateServerContainerID(_ context.Context, id string, containerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.containers = append(f.containers, containerUpdate{id: id, containerID: containerID})
	if s, ok := f.servers[id]; ok {
		s.ContainerID = containerID
	}
	return nil
}

type fakeClient struct {
	resolveErr  error
	summary     *container.Summary
	state       *docker.ContainerState
	observeErr  error
	started     []string
	recreations int
	recreateErr error
}

func (c *fakeClient) ResolveContainer(_ context.Context, _ string) (*container.Summary, error) {
	if c.resolveErr != nil {
		return nil, c.resolveErr
	}
	return c.summary, nil
}

func (c *fakeClient) ObserveContainer(_ context.Context, _ string) (*docker.ContainerState, error) {
	if c.observeErr != nil {
		return nil, c.observeErr
	}
	return c.state, nil
}

func (c *fakeClient) StartContainer(_ context.Context, containerID string) error {
	c.started = append(c.started, containerID)
	return nil
}

func (c *fakeClient) RecreateContainer(_ context.Context, _ string, _ *db.Server, _ *db.ServerConfig) (*docker.RecreateContainerResult, error) {
	c.recreations++
	if c.recreateErr != nil {
		return nil, c.recreateErr
	}
	return &docker.RecreateContainerResult{NewContainerID: "recreated-container"}, nil
}

type fakeResolver struct {
	cli Client
	err error
}

func (r fakeResolver) GetClientStrict(_ string) (Client, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.cli, nil
}

type fakeProxy struct {
	mu     sync.Mutex
	routes []string
}

func (p *fakeProxy) UpdateServerRoute(server *db.Server) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.routes = append(p.routes, server.ID)
	return nil
}

func (p *fakeProxy) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.routes)
}

type fakeBus struct {
	mu     sync.Mutex
	events []events.Event
}

func (b *fakeBus) Emit(_ context.Context, event events.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, event)
}

func (b *fakeBus) types() []v1.TriggeredEventType {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]v1.TriggeredEventType, 0, len(b.events))
	for _, e := range b.events {
		out = append(out, e.Type)
	}
	return out
}

// --- pure policy tests -----------------------------------------------------

func TestDecide(t *testing.T) {
	const (
		stopped   = db.StatusStopped
		running   = db.StatusRunning
		starting  = db.StatusStarting
		stopping  = db.StatusStopping
		errStatus = db.StatusError
		paused    = db.StatusPaused
		deepsleep = db.StatusDeepSleep
	)

	existsRunning := observedState{Exists: true, ContainerID: "c1", Status: running}
	existsExitedClean := observedState{Exists: true, ContainerID: "c1", Status: stopped, ExitCode: 0}
	existsOOM := observedState{Exists: true, ContainerID: "c1", Status: stopped, ExitCode: 137, OOMKilled: true}
	existsCrash := observedState{Exists: true, ContainerID: "c1", Status: stopped, ExitCode: 1}
	existsSigterm := observedState{Exists: true, ContainerID: "c1", Status: stopped, ExitCode: 143}
	existsDeleted := observedState{Exists: false}
	existsRestarting := observedState{Exists: true, ContainerID: "c1", Status: starting}
	existsPaused := observedState{Exists: true, ContainerID: "c1", Status: paused}

	tests := []struct {
		name          string
		desired       db.ServerStatus
		obs           observedState
		detached      bool
		selfHeal      bool
		wantStatus    db.ServerStatus
		wantAction    reconcileAction
		wantReasonHas string
	}{
		{"missing + desired running self-heals", running, existsDeleted, false, true, starting, actRecreate, "recreating"},
		{"missing + desired running self-heal disabled flags error", running, existsDeleted, false, false, errStatus, actNone, "missing"},
		{"missing + desired running detached never self-heals", running, existsDeleted, true, true, errStatus, actNone, "missing"},
		{"missing + desired stopped is a no-op", stopped, existsDeleted, false, true, "", actNone, ""},
		{"missing + desired stopping converges to stopped", stopping, existsDeleted, false, true, stopped, actNone, ""},
		{"missing + desired starting waits for create", starting, existsDeleted, false, true, "", actNone, ""},
		{"running + desired running is a no-op", running, existsRunning, false, true, "", actNone, ""},
		{"running + desired starting converges up", starting, existsRunning, false, true, running, actNone, ""},
		{"running + desired stopped syncs to reality", stopped, existsRunning, false, true, running, actNone, ""},
		{"running + desired stopping does not flap", stopping, existsRunning, false, true, "", actNone, ""},
		{"exited clean + desired running reports stopped", running, existsExitedClean, false, true, stopped, actNone, ""},
		{"exited 143 from docker stop is a clean stop", running, existsSigterm, false, true, stopped, actNone, ""},
		{"OOM kill is flagged as error", running, existsOOM, false, true, errStatus, actNone, "OOM"},
		{"crash exit is flagged as error", running, existsCrash, false, true, errStatus, actNone, "exited with code 1"},
		{"exited clean while stopping converges to stopped", stopping, existsExitedClean, false, true, stopped, actNone, ""},
		{"restarting is left to settle", running, existsRestarting, false, true, "", actNone, ""},
		{"paused is preserved", running, existsPaused, false, true, paused, actNone, ""},
		{"deepsleep + missing container is stable", deepsleep, existsDeleted, false, true, "", actNone, ""},
		{"deepsleep + exited container is stable", deepsleep, existsExitedClean, false, true, "", actNone, ""},
		{"deepsleep + crashed container never heals", deepsleep, existsCrash, false, true, "", actNone, ""},
		{"deepsleep + running container is a boot in flight", deepsleep, existsRunning, false, true, "", actNone, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decide(tt.desired, tt.obs, tt.detached, tt.selfHeal)
			if got.status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", got.status, tt.wantStatus)
			}
			if got.action != tt.wantAction {
				t.Fatalf("action = %v, want %v", got.action, tt.wantAction)
			}
			if tt.wantReasonHas != "" && !contains(got.reason, tt.wantReasonHas) {
				t.Fatalf("reason = %q, want it to contain %q", got.reason, tt.wantReasonHas)
			}
		})
	}
}

func TestClassifyExit(t *testing.T) {
	if st, _ := classifyExit(observedState{ExitCode: 0}); st != db.StatusStopped {
		t.Fatalf("exit 0 => %q, want stopped", st)
	}
	if st, _ := classifyExit(observedState{ExitCode: 143}); st != db.StatusStopped {
		t.Fatalf("exit 143 => %q, want stopped", st)
	}
	if st, reason := classifyExit(observedState{ExitCode: 137, OOMKilled: true}); st != db.StatusError || !contains(reason, "OOM") {
		t.Fatalf("exit 137 => %q/%q, want error mentioning OOM", st, reason)
	}
	if st, _ := classifyExit(observedState{ExitCode: 139}); st != db.StatusError {
		t.Fatalf("exit 139 => %q, want error", st)
	}
}

// --- debouncer tests -------------------------------------------------------

func TestDebouncerCoalescesBurst(t *testing.T) {
	d := newDebouncer(40 * time.Millisecond)

	var mu sync.Mutex
	fires := 0
	for i := 0; i < 20; i++ {
		d.enqueue("server-1", func() {
			mu.Lock()
			fires++
			mu.Unlock()
		})
	}

	time.Sleep(120 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if fires != 1 {
		t.Fatalf("burst of 20 enqueues fired %d times, want 1", fires)
	}
}

func TestDebouncerKeepsKeysIndependent(t *testing.T) {
	d := newDebouncer(30 * time.Millisecond)

	var mu sync.Mutex
	seen := map[string]int{}
	d.enqueue("a", func() { mu.Lock(); seen["a"]++; mu.Unlock() })
	d.enqueue("b", func() { mu.Lock(); seen["b"]++; mu.Unlock() })

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if seen["a"] != 1 || seen["b"] != 1 {
		t.Fatalf("seen = %v, want a:1 b:1", seen)
	}
}

// --- integration-ish tests over ReconcileServer ----------------------------

func newTestReconciler(t *testing.T, store Store, cli Client, proxy ProxyRouter, bus EventEmitter) *Reconciler {
	t.Helper()
	return New(store, fakeResolver{cli: cli}, proxy, bus, nil, nil, Config{})
}

func TestReconcileStatusTransitionExternalStop(t *testing.T) {
	server := &db.Server{ID: "s1", Name: "Survival", NodeID: "default", ContainerID: "c1", Status: db.StatusRunning}
	store := newFakeStore(server)
	cli := &fakeClient{
		summary: &container.Summary{ID: "c1"},
		state:   &docker.ContainerState{ContainerID: "c1", Status: db.StatusStopped, HasExited: true, ExitCode: 0},
	}
	bus := &fakeBus{}
	proxy := &fakeProxy{}

	r := newTestReconciler(t, store, cli, proxy, bus)
	if err := r.ReconcileServer(context.Background(), "s1"); err != nil {
		t.Fatalf("ReconcileServer: %v", err)
	}

	if len(store.statuses) != 1 || store.statuses[0].status != db.StatusStopped {
		t.Fatalf("status writes = %+v, want one stopped", store.statuses)
	}
	types := bus.types()
	if len(types) != 1 || types[0] != v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_STOP {
		t.Fatalf("events = %v, want [SERVER_STOP]", types)
	}
}

func TestReconcileNoWriteWhenAlreadyConverged(t *testing.T) {
	server := &db.Server{ID: "s1", Name: "Survival", NodeID: "default", ContainerID: "c1", Status: db.StatusRunning}
	store := newFakeStore(server)
	cli := &fakeClient{
		summary: &container.Summary{ID: "c1"},
		state:   &docker.ContainerState{ContainerID: "c1", Status: db.StatusRunning},
	}
	bus := &fakeBus{}

	r := newTestReconciler(t, store, cli, &fakeProxy{}, bus)
	if err := r.ReconcileServer(context.Background(), "s1"); err != nil {
		t.Fatalf("ReconcileServer: %v", err)
	}

	if len(store.statuses) != 0 {
		t.Fatalf("status writes = %+v, want none (already converged)", store.statuses)
	}
	if len(bus.events) != 0 {
		t.Fatalf("events = %v, want none", bus.types())
	}
}

func TestReconcileSelfHealsMissingContainer(t *testing.T) {
	server := &db.Server{ID: "s1", Name: "Survival", NodeID: "default", ContainerID: "gone", Status: db.StatusRunning, ProxyHostname: "mc.example.com"}
	store := newFakeStore(server)
	cli := &fakeClient{resolveErr: fmt.Errorf("%w: s1", docker.ErrContainerNotResolved)}
	bus := &fakeBus{}
	proxy := &fakeProxy{}

	r := newTestReconciler(t, store, cli, proxy, bus)
	if err := r.ReconcileServer(context.Background(), "s1"); err != nil {
		t.Fatalf("ReconcileServer: %v", err)
	}

	if cli.recreations != 1 {
		t.Fatalf("recreations = %d, want 1", cli.recreations)
	}
	if len(cli.started) != 1 || cli.started[0] != "recreated-container" {
		t.Fatalf("started = %v, want [recreated-container]", cli.started)
	}

	// New container ID must be adopted and status moved to starting.
	var adopted bool
	for _, c := range store.containers {
		if c.containerID == "recreated-container" {
			adopted = true
		}
	}
	if !adopted {
		t.Fatalf("container updates = %+v, want adoption of recreated-container", store.containers)
	}
	if len(store.statuses) != 1 || store.statuses[0].status != db.StatusStarting {
		t.Fatalf("status writes = %+v, want one starting", store.statuses)
	}
	if types := bus.types(); len(types) != 1 || types[0] != v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START {
		t.Fatalf("events = %v, want [SERVER_START]", types)
	}
	if proxy.count() == 0 {
		t.Fatalf("proxy route was not refreshed after self-heal")
	}
}

func TestReconcileAdoptsDriftedContainer(t *testing.T) {
	server := &db.Server{ID: "s1", Name: "Survival", NodeID: "default", ContainerID: "old-id", Status: db.StatusRunning, ProxyHostname: "mc.example.com"}
	store := newFakeStore(server)
	cli := &fakeClient{
		summary: &container.Summary{ID: "new-id"},
		state:   &docker.ContainerState{ContainerID: "new-id", Status: db.StatusRunning},
	}
	proxy := &fakeProxy{}

	r := newTestReconciler(t, store, cli, proxy, &fakeBus{})
	if err := r.ReconcileServer(context.Background(), "s1"); err != nil {
		t.Fatalf("ReconcileServer: %v", err)
	}

	if len(store.containers) != 1 || store.containers[0].containerID != "new-id" {
		t.Fatalf("container updates = %+v, want adoption of new-id", store.containers)
	}
	if len(store.statuses) != 0 {
		t.Fatalf("status writes = %+v, want none (status unchanged)", store.statuses)
	}
	if proxy.count() != 1 {
		t.Fatalf("proxy route refreshes = %d, want 1", proxy.count())
	}
}

func TestReconcileSkipsWhenNodeUnavailable(t *testing.T) {
	server := &db.Server{ID: "s1", Name: "Survival", NodeID: "remote", ContainerID: "c1", Status: db.StatusRunning}
	store := newFakeStore(server)
	bus := &fakeBus{}

	r := New(store, fakeResolver{err: errors.New("node offline")}, &fakeProxy{}, bus, nil, nil, Config{})
	if err := r.ReconcileServer(context.Background(), "s1"); err != nil {
		t.Fatalf("ReconcileServer: %v", err)
	}

	if len(store.statuses) != 0 || len(bus.events) != 0 {
		t.Fatalf("node outage must leave DB untouched; statuses=%v events=%v", store.statuses, bus.types())
	}
}

func TestReconcileOOMKillIsReportedAsError(t *testing.T) {
	server := &db.Server{ID: "s1", Name: "Survival", NodeID: "default", ContainerID: "c1", Status: db.StatusRunning}
	store := newFakeStore(server)
	cli := &fakeClient{
		summary: &container.Summary{ID: "c1"},
		state:   &docker.ContainerState{ContainerID: "c1", Status: db.StatusStopped, HasExited: true, ExitCode: 137, OOMKilled: true},
	}
	bus := &fakeBus{}

	r := newTestReconciler(t, store, cli, &fakeProxy{}, bus)
	if err := r.ReconcileServer(context.Background(), "s1"); err != nil {
		t.Fatalf("ReconcileServer: %v", err)
	}

	if len(store.statuses) != 1 || store.statuses[0].status != db.StatusError {
		t.Fatalf("status writes = %+v, want one error", store.statuses)
	}
	if len(bus.events) != 1 {
		t.Fatalf("expected one SERVER_STOP event for crash, got %v", bus.types())
	}
}

func TestStartConsumesEventsAndConverges(t *testing.T) {
	server := &db.Server{ID: "s1", Name: "Survival", NodeID: "default", ContainerID: "c1", Status: db.StatusRunning}
	store := newFakeStore(server)
	cli := &fakeClient{
		summary: &container.Summary{ID: "c1"},
		state:   &docker.ContainerState{ContainerID: "c1", Status: db.StatusRunning},
	}

	r := New(store, fakeResolver{cli: cli}, &fakeProxy{}, &fakeBus{}, nil, nil, Config{
		DebounceWindow: 10 * time.Millisecond,
		SweepInterval:  time.Hour,
	})

	ch := make(chan ContainerEvent, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r.Start(ctx, ch)

	ch <- ContainerEvent{ServerID: "s1", Action: "die", ContainerID: "c1"}

	// Give the debounce + worker a moment; the state is already converged so we
	// just assert the reconciler consumed the event without erroring.
	time.Sleep(80 * time.Millisecond)

	r.Stop()
	r.Wait()
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// --- MINE-108: log subscription migration + dynamic re-routing ---------------

type migrationCall struct {
	serverID string
	oldID    string
	newID    string
}

type fakeLogMigrator struct {
	mu    sync.Mutex
	calls []migrationCall
}

func (m *fakeLogMigrator) MigrateServerLogSubscriptions(serverID, oldContainerID, newContainerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, migrationCall{serverID: serverID, oldID: oldContainerID, newID: newContainerID})
}

func (m *fakeLogMigrator) snapshot() []migrationCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]migrationCall, len(m.calls))
	copy(out, m.calls)
	return out
}

func summaryWithIP(id, ip string) *container.Summary {
	var addr netip.Addr
	if ip != "" {
		addr = netip.MustParseAddr(ip)
	}
	return &container.Summary{
		ID: id,
		NetworkSettings: &container.NetworkSettingsSummary{
			Networks: map[string]*network.EndpointSettings{
				"carbon-panel-network": {IPAddress: addr},
			},
		},
	}
}

func TestReconcileMigratesLogSubscriptionsOnAdoption(t *testing.T) {
	server := &db.Server{ID: "s1", Name: "Survival", NodeID: "default", ContainerID: "old-id", Status: db.StatusRunning}
	store := newFakeStore(server)
	cli := &fakeClient{
		summary: summaryWithIP("new-id", "10.0.0.5"),
		state:   &docker.ContainerState{ContainerID: "new-id", Status: db.StatusRunning},
	}
	migrator := &fakeLogMigrator{}

	r := New(store, fakeResolver{cli: cli}, &fakeProxy{}, &fakeBus{}, migrator, nil, Config{})
	if err := r.ReconcileServer(context.Background(), "s1"); err != nil {
		t.Fatalf("ReconcileServer: %v", err)
	}

	calls := migrator.snapshot()
	if len(calls) != 1 {
		t.Fatalf("migration calls = %+v, want exactly 1", calls)
	}
	if calls[0].serverID != "s1" || calls[0].oldID != "old-id" || calls[0].newID != "new-id" {
		t.Fatalf("migration call = %+v, want s1 old-id -> new-id", calls[0])
	}
}

func TestReconcileMigratesLogSubscriptionsOnSelfHeal(t *testing.T) {
	server := &db.Server{ID: "s1", Name: "Survival", NodeID: "default", ContainerID: "gone", Status: db.StatusRunning}
	store := newFakeStore(server)
	cli := &fakeClient{resolveErr: fmt.Errorf("%w: s1", docker.ErrContainerNotResolved)}
	migrator := &fakeLogMigrator{}

	r := New(store, fakeResolver{cli: cli}, &fakeProxy{}, &fakeBus{}, migrator, nil, Config{})
	if err := r.ReconcileServer(context.Background(), "s1"); err != nil {
		t.Fatalf("ReconcileServer: %v", err)
	}

	calls := migrator.snapshot()
	if len(calls) != 1 {
		t.Fatalf("migration calls = %+v, want exactly 1", calls)
	}
	if calls[0].oldID != "gone" || calls[0].newID != "recreated-container" {
		t.Fatalf("migration call = %+v, want gone -> recreated-container", calls[0])
	}
}

func TestReconcileRefreshesRouteOnIPChange(t *testing.T) {
	server := &db.Server{
		ID: "s1", Name: "Survival", NodeID: "default",
		ContainerID: "c1", Status: db.StatusRunning, ProxyHostname: "mc.example.com",
	}
	store := newFakeStore(server)
	cli := &fakeClient{
		summary: summaryWithIP("c1", "10.0.0.2"),
		state:   &docker.ContainerState{ContainerID: "c1", Status: db.StatusRunning},
	}
	proxy := &fakeProxy{}

	r := newTestReconciler(t, store, cli, proxy, &fakeBus{})

	// First pass learns the backend and pushes the route once.
	if err := r.ReconcileServer(context.Background(), "s1"); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if proxy.count() != 1 {
		t.Fatalf("route refreshes after first pass = %d, want 1", proxy.count())
	}

	// An unchanged backend must not churn the route.
	if err := r.ReconcileServer(context.Background(), "s1"); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if proxy.count() != 1 {
		t.Fatalf("route refreshes after unchanged pass = %d, want 1", proxy.count())
	}

	// A re-allocated IP on the same container must re-point the route.
	cli.summary = summaryWithIP("c1", "10.0.0.9")
	if err := r.ReconcileServer(context.Background(), "s1"); err != nil {
		t.Fatalf("third reconcile: %v", err)
	}
	if proxy.count() != 2 {
		t.Fatalf("route refreshes after IP change = %d, want 2", proxy.count())
	}

	// No status writes: the server was running the whole time.
	if len(store.statuses) != 0 {
		t.Fatalf("status writes = %+v, want none", store.statuses)
	}
}

func TestReconcileMigratesLogSubscriptionsWhenDBAlreadyUpdated(t *testing.T) {
	server := &db.Server{ID: "s1", Name: "Survival", NodeID: "default", ContainerID: "old-id", Status: db.StatusRunning}
	store := newFakeStore(server)
	cli := &fakeClient{
		summary: summaryWithIP("old-id", "10.0.0.1"),
		state:   &docker.ContainerState{ContainerID: "old-id", Status: db.StatusRunning},
	}
	migrator := &fakeLogMigrator{}

	r := New(store, fakeResolver{cli: cli}, &fakeProxy{}, &fakeBus{}, migrator, nil, Config{})

	// Prime the backend cache with the container the subscribers are registered
	// against.
	if err := r.ReconcileServer(context.Background(), "s1"); err != nil {
		t.Fatalf("prime reconcile: %v", err)
	}

	// Simulate an RPC-initiated Force Recreate: it swaps the container and writes
	// the new ID straight to the DB, so the reconciler never sees an adoption.
	store.mu.Lock()
	store.servers["s1"].ContainerID = "new-id"
	store.mu.Unlock()
	cli.summary = summaryWithIP("new-id", "10.0.0.2")
	cli.state = &docker.ContainerState{ContainerID: "new-id", Status: db.StatusRunning}

	migrator.mu.Lock()
	migrator.calls = nil
	migrator.mu.Unlock()

	if err := r.ReconcileServer(context.Background(), "s1"); err != nil {
		t.Fatalf("post-recreate reconcile: %v", err)
	}

	calls := migrator.snapshot()
	if len(calls) != 1 || calls[0].oldID != "old-id" || calls[0].newID != "new-id" {
		t.Fatalf("migration calls = %+v, want one old-id -> new-id", calls)
	}
}
