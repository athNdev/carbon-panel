// Merged in the MINE-172 integration: this file existed independently on
// both lines of development, so the union of both test sets is kept.

package config

import (
	"os"
	"path/filepath"
	"reflect"
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

// --- tests present only on the pre-merge line of development ---

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

func TestConfigValidateMethod(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should validate cleanly, got: %v", err)
	}

	// Invalid server port
	cfgBadPort := *cfg
	cfgBadPort.Server.Port = "99999"
	if err := cfgBadPort.Validate(); err == nil {
		t.Fatal("expected error for port > 65535")
	}

	// Non-numeric port
	cfgNonNumPort := *cfg
	cfgNonNumPort.Server.Port = "not-a-port"
	if err := cfgNonNumPort.Validate(); err == nil {
		t.Fatal("expected error for non-numeric port")
	}

	// Negative server timeout
	cfgNegTimeout := *cfg
	cfgNegTimeout.Server.ReadTimeout = -1
	if err := cfgNegTimeout.Validate(); err == nil {
		t.Fatal("expected error for negative server read timeout")
	}

	// Empty database path
	cfgEmptyDB := *cfg
	cfgEmptyDB.Database.Path = ""
	if err := cfgEmptyDB.Validate(); err == nil {
		t.Fatal("expected error for empty database path")
	}

	// Bad upload max chunk size
	cfgBadUpload := *cfg
	cfgBadUpload.Upload.DefaultChunkSize = 10 * 1024 * 1024
	cfgBadUpload.Upload.MaxChunkSize = 5 * 1024 * 1024
	if err := cfgBadUpload.Validate(); err == nil {
		t.Fatal("expected error when max chunk size is less than default chunk size")
	}

	// S3 enabled without bucket
	cfgBadS3 := *cfg
	cfgBadS3.Storage.S3.Enabled = true
	cfgBadS3.Storage.S3.Bucket = ""
	if err := cfgBadS3.Validate(); err == nil {
		t.Fatal("expected error when S3 is enabled but bucket is empty")
	}

	// OIDC enabled without issuer URI
	cfgBadOIDC := *cfg
	cfgBadOIDC.Auth.OIDC.Enabled = true
	cfgBadOIDC.Auth.OIDC.IssuerURI = ""
	if err := cfgBadOIDC.Validate(); err == nil {
		t.Fatal("expected error when OIDC is enabled but issuer URI is empty")
	}

	// Negative storage snapshot config
	cfgNegMaxSnap := *cfg
	cfgNegMaxSnap.Storage.MaxSnapshots = -1
	if err := cfgNegMaxSnap.Validate(); err == nil {
		t.Fatal("expected error for negative storage.max_snapshots")
	}

	cfgNegSnapRet := *cfg
	cfgNegSnapRet.Storage.SnapshotRetentionDays = -1
	if err := cfgNegSnapRet.Validate(); err == nil {
		t.Fatal("expected error for negative storage.snapshot_retention_days")
	}
}
