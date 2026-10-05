package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(t.TempDir()) // empty dir: no config file, pure defaults
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != "8080" || cfg.Server.Host != "0.0.0.0" {
		t.Fatalf("server defaults: %+v", cfg.Server)
	}
	if cfg.Server.ReadTimeout != 15 || cfg.Server.WriteTimeout != 15 || cfg.Server.IdleTimeout != 60 {
		t.Fatalf("server timeout defaults: %+v", cfg.Server)
	}
	if cfg.Database.MaxConnections != 25 || !cfg.Database.AutoMigrate {
		t.Fatalf("database defaults: %+v", cfg.Database)
	}
	if cfg.Docker.Host != "unix:///var/run/docker.sock" || !cfg.Docker.EnableRateLimit {
		t.Fatalf("docker defaults: %+v", cfg.Docker)
	}
	if cfg.Storage.MaxUploadSize != 500*1024*1024 {
		t.Fatalf("storage max_upload_size=%d", cfg.Storage.MaxUploadSize)
	}
	if cfg.Proxy.Enabled || cfg.Proxy.ListenPort != 25565 {
		t.Fatalf("proxy defaults: %+v", cfg.Proxy)
	}
	if cfg.Proxy.PortRangeMin != 25565 || cfg.Proxy.PortRangeMax != 25665 {
		t.Fatalf("proxy range: %+v", cfg.Proxy)
	}
	if !cfg.Module.Enabled || cfg.Module.PortRangeMin != 8100 || cfg.Module.PortRangeMax != 8199 {
		t.Fatalf("module defaults: %+v", cfg.Module)
	}
	if cfg.Auth.SessionTimeout != 86400 || !cfg.Auth.Local.Enabled || cfg.Auth.AllowNoAuth {
		t.Fatalf("auth defaults: %+v", cfg.Auth)
	}
	if cfg.Upload.SessionTTL != 240 || cfg.Upload.DefaultChunkSize != 5*1024*1024 {
		t.Fatalf("upload defaults: %+v", cfg.Upload)
	}
	if !filepath.IsAbs(cfg.Database.Path) || !filepath.IsAbs(cfg.Storage.DataDir) {
		t.Fatalf("paths should be absolute: %+v %+v", cfg.Database.Path, cfg.Storage.DataDir)
	}
}

func TestLoadRejectsBadProxyRange(t *testing.T) {
	dir := writeConfig(t, "proxy:\n  port_range_min: 25665\n  port_range_max: 25565\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("want validation error for inverted proxy range")
	}
}

func TestLoadRejectsBadModuleRange(t *testing.T) {
	dir := writeConfig(t, "module:\n  port_range_min: 8199\n  port_range_max: 8100\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("want validation error for inverted module range")
	}
}

func TestLoadRejectsReservedDockerLabel(t *testing.T) {
	dir := writeConfig(t, "docker:\n  labels:\n    carbon-panel.managed: \"true\"\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("want validation error for reserved carbon-panel.* label")
	}
}

func TestLoadAllowsCustomDockerLabel(t *testing.T) {
	dir := writeConfig(t, "docker:\n  labels:\n    com.example.team: \"mc\"\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Docker.Labels["com.example.team"] != "mc" {
		t.Fatalf("labels=%v", cfg.Docker.Labels)
	}
}

func TestLoadMergesPrimaryListenPort(t *testing.T) {
	dir := writeConfig(t, "proxy:\n  enabled: true\n  listen_port: 25566\n  listen_ports: [25565]\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Proxy.ListenPorts) != 2 || cfg.Proxy.ListenPorts[0] != 25566 {
		t.Fatalf("listen_ports=%v want [25566 25565]", cfg.Proxy.ListenPorts)
	}
}

func TestLoadFillsEmptyListenPorts(t *testing.T) {
	dir := writeConfig(t, "proxy:\n  enabled: true\n  listen_port: 25577\n  listen_ports: []\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Proxy.ListenPorts) != 1 || cfg.Proxy.ListenPorts[0] != 25577 {
		t.Fatalf("listen_ports=%v want [25577]", cfg.Proxy.ListenPorts)
	}
}

func TestLoadOverridesScalar(t *testing.T) {
	dir := writeConfig(t, "server:\n  port: \"9090\"\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != "9090" {
		t.Fatalf("port=%q want 9090", cfg.Server.Port)
	}
}

func TestFlattenMapNested(t *testing.T) {
	dst := map[string]string{}
	flattenMap(map[string]any{
		"a": map[string]any{"b": "x", "c": 1},
		"d": true,
	}, "", dst)
	if dst["a.b"] != "x" || dst["a.c"] != "1" || dst["d"] != "true" {
		t.Fatalf("flat=%v", dst)
	}
}

func TestJsonStringToMapHook(t *testing.T) {
	hook := jsonStringToMapHook()
	// JSON object string into a map type decodes to a map.
	out, err := hook(
		// from
		reflect.TypeOf(""),
		reflect.TypeOf(map[string]string{}),
		`{"k":"v"}`,
	)
	if err != nil {
		t.Fatalf("hook: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["k"] != "v" {
		t.Fatalf("decoded=%v (%T)", out, out)
	}
	// Non-JSON string passes through untouched.
	out, err = hook(reflect.TypeOf(""), reflect.TypeOf(map[string]string{}), "not-json{{{")
	if err != nil {
		t.Fatalf("hook: %v", err)
	}
	if out != "not-json{{{" {
		t.Fatalf("passthrough=%v", out)
	}
}
