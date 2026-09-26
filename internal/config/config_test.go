package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfigYAML(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoad_DefaultsWithoutFile(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("load without file failed: %v", err)
	}
	if cfg.Proxy.PortRangeMin >= cfg.Proxy.PortRangeMax {
		t.Errorf("default proxy range invalid: %d-%d", cfg.Proxy.PortRangeMin, cfg.Proxy.PortRangeMax)
	}
	if cfg.Module.PortRangeMin >= cfg.Module.PortRangeMax {
		t.Errorf("default module range invalid: %d-%d", cfg.Module.PortRangeMin, cfg.Module.PortRangeMax)
	}
	if !filepath.IsAbs(cfg.Database.Path) || !filepath.IsAbs(cfg.Storage.DataDir) {
		t.Errorf("paths not absolutized: %+v", cfg)
	}
}

func TestLoad_YAMLFile(t *testing.T) {
	dir := writeConfigYAML(t, "proxy:\n  enabled: true\n  base_url: mc.example.com\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if !cfg.Proxy.Enabled || cfg.Proxy.BaseURL != "mc.example.com" {
		t.Errorf("yaml not applied: %+v", cfg.Proxy)
	}
	// Primary listen port is merged into the list.
	found := false
	for _, p := range cfg.Proxy.ListenPorts {
		if p == cfg.Proxy.ListenPort {
			found = true
		}
	}
	if !found {
		t.Errorf("listen port %d missing from %v", cfg.Proxy.ListenPort, cfg.Proxy.ListenPorts)
	}
}

func TestLoad_InvalidPortRange(t *testing.T) {
	dir := writeConfigYAML(t, "proxy:\n  port_range_min: 40000\n  port_range_max: 40000\n")
	if _, err := Load(dir); err == nil {
		t.Error("equal proxy range accepted")
	}

	dir = writeConfigYAML(t, "module:\n  port_range_min: 30010\n  port_range_max: 30000\n")
	if _, err := Load(dir); err == nil {
		t.Error("inverted module range accepted")
	}
}

func TestLoad_ReservedLabelRejected(t *testing.T) {
	dir := writeConfigYAML(t, "docker:\n  labels:\n    carbon-panel.custom: x\n")
	if _, err := Load(dir); err == nil {
		t.Error("reserved label namespace accepted")
	}
}
