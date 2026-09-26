// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


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
