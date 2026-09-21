package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultStable(t *testing.T) {
	t.Parallel()
	d := Default()
	assert.Equal(t, ":8080", d.Server.Addr)
	assert.Equal(t, 15*time.Second, d.Server.ReadTimeout)
	assert.Equal(t, 15*time.Second, d.Server.WriteTimeout)
	assert.Equal(t, 30*time.Second, d.Server.ShutdownTimeout)
	assert.Equal(t, "sqlite", d.Database.Driver)
	assert.Equal(t, "./data/cloud.db", d.Database.URL)
	assert.True(t, d.Database.AutoMigrate)
	assert.Equal(t, "env", d.Secrets.Provider)
	assert.Equal(t, "CARBONCLOUD_", d.Secrets.Prefix)
	assert.Equal(t, "tofu", d.Provisioner.TerraformPath)
	assert.Equal(t, "local", d.Provisioner.StateBackend)
	assert.Equal(t, 4, d.Provisioner.MaxConcurrent)
	assert.Equal(t, "info", d.Telemetry.LogLevel)
	assert.Equal(t, "text", d.Telemetry.LogFormat)
	require.NoError(t, d.Validate())
}

func TestLoadDefaultsOnly(t *testing.T) {
	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, Default(), cfg)
}

func TestLoadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cloud.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
server:
  addr: ":9090"
  public_url: https://api.cloud.example
  cors_origins: ["https://app.example"]
database:
  driver: sqlite
  url: /tmp/x.db
clerk:
  issuer: https://issuer.example
  audience: aud-1
secrets:
  provider: chain
  file: /tmp/secrets.env
providers:
  enabled: ["generic", "hetzner"]
  default_region: fsn1
provisioner:
  terraform_path: /usr/bin/tofu
  state_backend: local
  max_concurrent: 2
telemetry:
  log_level: debug
  log_format: json
console:
  base_url: https://app.example
  allowed_origins: ["https://app.example"]
`), 0o600))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, ":9090", cfg.Server.Addr)
	assert.Equal(t, "https://api.cloud.example", cfg.Server.PublicURL)
	assert.Equal(t, []string{"https://app.example"}, cfg.Server.CORSOrigins)
	assert.Equal(t, "/tmp/x.db", cfg.Database.URL)
	assert.Equal(t, "https://issuer.example", cfg.Clerk.Issuer)
	assert.Equal(t, "aud-1", cfg.Clerk.Audience)
	assert.Equal(t, "chain", cfg.Secrets.Provider)
	assert.Equal(t, []string{"generic", "hetzner"}, cfg.Providers.Enabled)
	assert.Equal(t, "fsn1", cfg.Providers.DefaultRegion)
	assert.Equal(t, "/usr/bin/tofu", cfg.Provisioner.TerraformPath)
	assert.Equal(t, 2, cfg.Provisioner.MaxConcurrent)
	assert.Equal(t, "debug", cfg.Telemetry.LogLevel)
	assert.Equal(t, "json", cfg.Telemetry.LogFormat)
	assert.Equal(t, "https://app.example", cfg.Console.BaseURL)
	// Defaults survive for fields the file does not set.
	assert.Equal(t, 15*time.Second, cfg.Server.ReadTimeout)
	assert.Equal(t, "CARBONCLOUD_", cfg.Secrets.Prefix)
}

func TestLoadYAMLDurations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cloud.yaml")
	require.NoError(t, os.WriteFile(path, []byte("server:\n  read_timeout: 42s\n"), 0o600))
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, 42*time.Second, cfg.Server.ReadTimeout)
}

func TestEnvOverrideWins(t *testing.T) {
	t.Setenv("CARBONCLOUD_SERVER_ADDR", ":7070")
	t.Setenv("CARBONCLOUD_DATABASE_URL", "/tmp/env.db")
	t.Setenv("CARBONCLOUD_CLERK_ISSUER", "https://env-issuer.example")
	t.Setenv("CARBONCLOUD_TELEMETRY_LOG_LEVEL", "warn")

	dir := t.TempDir()
	path := filepath.Join(dir, "cloud.yaml")
	require.NoError(t, os.WriteFile(path, []byte("server:\n  addr: \":9090\"\n"), 0o600))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, ":7070", cfg.Server.Addr)
	assert.Equal(t, "/tmp/env.db", cfg.Database.URL)
	assert.Equal(t, "https://env-issuer.example", cfg.Clerk.Issuer)
	assert.Equal(t, "warn", cfg.Telemetry.LogLevel)
}

func TestLoadMissingFileErrors(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	require.Error(t, err)
}

func TestValidatePostgresRequiresURL(t *testing.T) {
	t.Parallel()
	c := Default()
	c.Database.Driver = "postgres"
	c.Database.URL = ""
	err := c.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database.url")

	c.Database.URL = "postgres://db:5432/cloud?sslmode=disable"
	require.NoError(t, c.Validate())
}

func TestValidateRejectsUnknownEnums(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"driver", func(c *Config) { c.Database.Driver = "mysql"; c.Database.URL = "x" }, "database.driver"},
		{"secrets", func(c *Config) { c.Secrets.Provider = "vault" }, "secrets.provider"},
		{"backend", func(c *Config) { c.Provisioner.StateBackend = "gcs" }, "provisioner.state_backend"},
		{"level", func(c *Config) { c.Telemetry.LogLevel = "verbose" }, "telemetry.log_level"},
		{"format", func(c *Config) { c.Telemetry.LogFormat = "yaml" }, "telemetry.log_format"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := Default()
			tc.mutate(c)
			err := c.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestValidateRejectsNegatives(t *testing.T) {
	t.Parallel()
	c := Default()
	c.Server.ReadTimeout = -time.Second
	require.ErrorContains(t, c.Validate(), "server.read_timeout")

	c = Default()
	c.Database.MaxOpenConns = -1
	require.ErrorContains(t, c.Validate(), "database.max_open_conns")

	c = Default()
	c.Provisioner.MaxConcurrent = -2
	require.ErrorContains(t, c.Validate(), "provisioner.max_concurrent")

	c = Default()
	c.Clerk.JWKSCacheTTL = -time.Minute
	require.ErrorContains(t, c.Validate(), "clerk.jwks_cache_ttl")
}

func TestValidateS3BackendNeedsKeys(t *testing.T) {
	t.Setenv("CARBONCLOUD_STATE_S3_BUCKET", "")
	t.Setenv("CARBONCLOUD_STATE_S3_ACCESS_KEY_ID", "")
	t.Setenv("CARBONCLOUD_STATE_S3_SECRET_ACCESS_KEY", "")
	os.Unsetenv("CARBONCLOUD_STATE_S3_BUCKET")
	os.Unsetenv("CARBONCLOUD_STATE_S3_ACCESS_KEY_ID")
	os.Unsetenv("CARBONCLOUD_STATE_S3_SECRET_ACCESS_KEY")

	c := Default()
	c.Provisioner.StateBackend = "s3"
	err := c.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "s3")

	t.Setenv("CARBONCLOUD_STATE_S3_BUCKET", "tf-state")
	t.Setenv("CARBONCLOUD_STATE_S3_ACCESS_KEY_ID", "AKIDEXAMPLE")
	t.Setenv("CARBONCLOUD_STATE_S3_SECRET_ACCESS_KEY", "secret-example-value")
	require.NoError(t, c.Validate())
}

func TestValidateFileProviderNeedsPath(t *testing.T) {
	t.Parallel()
	c := Default()
	c.Secrets.Provider = "file"
	c.Secrets.File = ""
	require.ErrorContains(t, c.Validate(), "secrets.file")
}

func TestValidateDoesNotRequireClerk(t *testing.T) {
	t.Parallel()
	c := Default()
	c.Clerk.Issuer = ""
	require.NoError(t, c.Validate())
}

func TestDevConfigsLoad(t *testing.T) {
	for _, name := range []string{"controld.dev.yaml", "noded.dev.yaml"} {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(filepath.Join("..", "..", "..", "deploy", "docker", name))
			require.NoError(t, err)
			require.NoError(t, cfg.Validate())
		})
	}
}
