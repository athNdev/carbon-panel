package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

func newManagerUnitTest(t *testing.T, cfg config.ProxyConfig) (*Manager, *storage.Store) {
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
	m := &Manager{
		proxies: make(map[int]Proxier),
		store:   store,
		config:  &cfg,
		logger:  logger.New(),
	}
	return m, store
}

func TestManager_HandlersAndRunning(t *testing.T) {
	m, _ := newManagerUnitTest(t, config.ProxyConfig{})

	if m.IsRunning() {
		t.Error("running with no proxies")
	}
	called := 0
	m.SetWakeHandler(func(_ context.Context, _ string) error { called++; return nil })
	m.SetActivityHandler(func(_ string) { called++ })
	m.SetSleepWakeHandler(func(_ context.Context, _ string) error { called++; return nil })
	_ = called
	if m.wakeHandler == nil || m.activityHandler == nil || m.sleepWakeHandler == nil {
		t.Error("handlers not installed")
	}
}

func TestManager_GenerateHostname(t *testing.T) {
	m, _ := newManagerUnitTest(t, config.ProxyConfig{BaseURL: "mc.example.com"})

	if got := m.generateHostname(&storage.Server{ProxyHostname: "custom.example.com"}); got != "custom.example.com" {
		t.Errorf("custom = %q", got)
	}
	if got := m.generateHostname(&storage.Server{ID: "abc", Name: "My Survival"}); got != "my-survival.mc.example.com" {
		t.Errorf("pattern = %q", got)
	}

	m.config.BaseURL = ""
	if got := m.generateHostname(&storage.Server{ID: "abc"}); got != "server-abc.minecraft.mc" {
		t.Errorf("fallback = %q", got)
	}
}

func TestManager_RemoveRouteByHostnameGuards(t *testing.T) {
	m, _ := newManagerUnitTest(t, config.ProxyConfig{})

	// No proxies / disabled / empty hostname are all safe no-ops.
	if err := m.RemoveRouteByHostname("x.example.com", ""); err != nil {
		t.Errorf("no proxies: %v", err)
	}
	m.config.Enabled = true
	if err := m.RemoveRouteByHostname("", ""); err != nil {
		t.Errorf("empty hostname: %v", err)
	}
	if err := m.RemoveRouteByHostname("x.example.com", "no-such-listener"); err != nil {
		t.Errorf("unknown listener: %v", err)
	}
	if routes := m.GetRoutes(); len(routes) != 0 {
		t.Errorf("routes = %v", routes)
	}
}

func TestManager_AllocateProxyPort(t *testing.T) {
	cfg := config.ProxyConfig{PortRangeMin: 40000, PortRangeMax: 40001}
	m, store := newManagerUnitTest(t, cfg)

	if err := store.DB().Create(&storage.Server{ID: "srv-1", Name: "s1", DataPath: t.TempDir(), ProxyPort: 40000}).Error; err != nil {
		t.Fatalf("failed to insert server: %v", err)
	}

	port, err := m.AllocateProxyPort("srv-2")
	if err != nil || port != 40001 {
		t.Errorf("allocate = %d, %v; want 40001", port, err)
	}
	// Own port does not block self: re-allocation returns the same port.
	port, err = m.AllocateProxyPort("srv-1")
	if err != nil || port != 40000 {
		t.Errorf("self allocate = %d, %v; want 40000", port, err)
	}

	if err := store.DB().Create(&storage.Server{ID: "srv-2", Name: "s2", DataPath: t.TempDir(), ProxyPort: 40001}).Error; err != nil {
		t.Fatalf("failed to insert server: %v", err)
	}
	if _, err := m.AllocateProxyPort("srv-3"); err == nil {
		t.Error("exhausted range allocated without error")
	}
}

type testRouteProxier struct {
	routes map[string]*Route
}

func (f *testRouteProxier) Start() error { return nil }
func (f *testRouteProxier) Stop() error  { return nil }
func (f *testRouteProxier) AddRoute(serverID, hostname, backendHost string, backendPort int) {
	f.routes[hostname] = &Route{ServerID: serverID, Hostname: hostname, BackendHost: backendHost, BackendPort: backendPort}
}
func (f *testRouteProxier) RemoveRoute(hostname string) {
	delete(f.routes, hostname)
}
func (f *testRouteProxier) UpdateRoute(hostname, backendHost string, backendPort int) {
	if r, ok := f.routes[hostname]; ok {
		r.BackendHost = backendHost
		r.BackendPort = backendPort
	}
}
func (f *testRouteProxier) SetRouteHibernated(hostname string, hibernated bool) {}
func (f *testRouteProxier) SetRouteDown(hostname string, down bool)             {}
func (f *testRouteProxier) GetRoutes() map[string]*Route                        { return f.routes }
func (f *testRouteProxier) IsRunning() bool                                     { return true }

func TestManager_UpdateServerRoute_StatusErrorRemovesRoute(t *testing.T) {
	cfg := config.ProxyConfig{Enabled: true}
	m, store := newManagerUnitTest(t, cfg)
	m.serverHostnames = make(map[string]string)

	listener := &storage.ProxyListener{
		ID:      "plist-1",
		Port:    25565,
		Enabled: true,
	}
	if err := store.CreateProxyListener(context.Background(), listener); err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	fake := &testRouteProxier{routes: make(map[string]*Route)}
	m.proxies[25565] = fake

	// Pre-populate route
	hostname := "crash.mc.example.com"
	fake.AddRoute("srv-crash", hostname, "172.20.0.10", 25565)
	if len(fake.GetRoutes()) != 1 {
		t.Fatalf("expected 1 route, got %d", len(fake.GetRoutes()))
	}

	server := &storage.Server{
		ID:              "srv-crash",
		Name:            "CrashServer",
		ProxyHostname:   hostname,
		ProxyListenerID: listener.ID,
		Status:          storage.StatusError,
	}
	if err := store.CreateServer(context.Background(), server); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if err := m.UpdateServerRoute(server); err != nil {
		t.Fatalf("UpdateServerRoute failed: %v", err)
	}

	if len(fake.GetRoutes()) != 0 {
		t.Fatalf("expected route to be removed on StatusError, got %+v", fake.GetRoutes())
	}
}

func TestManager_RemoveServerRoute_WithDeletedServerUsesCache(t *testing.T) {
	cfg := config.ProxyConfig{Enabled: true}
	m, store := newManagerUnitTest(t, cfg)
	m.serverHostnames = make(map[string]string)

	fake := &testRouteProxier{routes: make(map[string]*Route)}
	m.proxies[25565] = fake

	hostname := "vanished.mc.example.com"
	fake.AddRoute("srv-deleted", hostname, "172.20.0.11", 25565)

	// Populate the cache as UpdateServerRoute would
	m.serverHostnames["srv-deleted"] = hostname

	// Ensure server does NOT exist in store
	_, err := store.GetServer(context.Background(), "srv-deleted")
	if err == nil {
		t.Fatal("expected server to not exist in store")
	}

	// RemoveServerRoute must succeed using the cached hostname even when DB lookup fails
	if err := m.RemoveServerRoute("srv-deleted"); err != nil {
		t.Fatalf("RemoveServerRoute failed on deleted server: %v", err)
	}

	if len(fake.GetRoutes()) != 0 {
		t.Fatalf("expected route to be removed from proxies via cache, got %+v", fake.GetRoutes())
	}
}
