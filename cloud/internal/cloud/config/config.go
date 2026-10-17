// Package config loads and validates Carbon Cloud control-plane configuration.
//
// Configuration comes from a YAML file with environment overrides. Every
// environment variable uses the CARBONCLOUD_ prefix with dots replaced by
// underscores, e.g. CARBONCLOUD_DATABASE_URL or CARBONCLOUD_CLERK_ISSUER.
//
// The process must boot without any external credential: Clerk and provider
// keys are capabilities, not boot requirements (see ADR 0006). Validate only
// rejects structurally invalid configuration.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// EnvPrefix is the environment variable prefix for all config overrides.
const EnvPrefix = "CARBONCLOUD"

// Config is the root control-plane configuration. Field names are a contract.
type Config struct {
	Server      Server      `mapstructure:"server" yaml:"server"`
	Database    Database    `mapstructure:"database" yaml:"database"`
	Clerk       Clerk       `mapstructure:"clerk" yaml:"clerk"`
	Secrets     Secrets     `mapstructure:"secrets" yaml:"secrets"`
	Providers   Providers   `mapstructure:"providers" yaml:"providers"`
	Provisioner Provisioner `mapstructure:"provisioner" yaml:"provisioner"`
	Telemetry   Telemetry   `mapstructure:"telemetry" yaml:"telemetry"`
	Console     Console     `mapstructure:"console" yaml:"console"`
}

// Server holds HTTP server settings.
type Server struct {
	Addr            string        `mapstructure:"addr" yaml:"addr"`
	PublicURL       string        `mapstructure:"public_url" yaml:"public_url"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout" yaml:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout" yaml:"write_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout" yaml:"shutdown_timeout"`
	CORSOrigins     []string      `mapstructure:"cors_origins" yaml:"cors_origins"`
}

// Database holds storage connection settings.
type Database struct {
	Driver          string        `mapstructure:"driver" yaml:"driver"`
	URL             string        `mapstructure:"url" yaml:"url"`
	MaxOpenConns    int           `mapstructure:"max_open_conns" yaml:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns" yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime" yaml:"conn_max_lifetime"`
	AutoMigrate     bool          `mapstructure:"auto_migrate" yaml:"auto_migrate"`
}

// Clerk holds Clerk identity settings. All fields are optional at boot; a
// missing issuer only disables the clerk capability (see ADR 0006).
type Clerk struct {
	Issuer            string        `mapstructure:"issuer" yaml:"issuer"`
	JWKSURL           string        `mapstructure:"jwks_url" yaml:"jwks_url"`
	Audience          string        `mapstructure:"audience" yaml:"audience"`
	AuthorizedParties []string      `mapstructure:"authorized_parties" yaml:"authorized_parties"`
	WebhookTolerance  time.Duration `mapstructure:"webhook_tolerance" yaml:"webhook_tolerance"`
	JWKSCacheTTL      time.Duration `mapstructure:"jwks_cache_ttl" yaml:"jwks_cache_ttl"`
}

// Secrets selects how credentials are provided.
type Secrets struct {
	Provider string `mapstructure:"provider" yaml:"provider"`
	File     string `mapstructure:"file" yaml:"file"`
	Prefix   string `mapstructure:"prefix" yaml:"prefix"`
}

// Providers holds managed-node provider selection.
type Providers struct {
	Enabled       []string `mapstructure:"enabled" yaml:"enabled"`
	DefaultRegion string   `mapstructure:"default_region" yaml:"default_region"`
	TenantCreds   bool     `mapstructure:"tenant_credentials" yaml:"tenant_credentials"`
}

// Provisioner holds Terraform workspace driver settings.
type Provisioner struct {
	TerraformPath string        `mapstructure:"terraform_path" yaml:"terraform_path"`
	WorkDir       string        `mapstructure:"work_dir" yaml:"work_dir"`
	StateBackend  string        `mapstructure:"state_backend" yaml:"state_backend"`
	StateLocalDir string        `mapstructure:"state_local_dir" yaml:"state_local_dir"`
	PlanTimeout   time.Duration `mapstructure:"plan_timeout" yaml:"plan_timeout"`
	ApplyTimeout  time.Duration `mapstructure:"apply_timeout" yaml:"apply_timeout"`
	MaxConcurrent int           `mapstructure:"max_concurrent" yaml:"max_concurrent"`
}

// Telemetry holds logging and metrics settings.
type Telemetry struct {
	LogLevel    string `mapstructure:"log_level" yaml:"log_level"`
	LogFormat   string `mapstructure:"log_format" yaml:"log_format"`
	MetricsAddr string `mapstructure:"metrics_addr" yaml:"metrics_addr"`
}

// Console holds web console settings.
type Console struct {
	BaseURL        string   `mapstructure:"base_url" yaml:"base_url"`
	AllowedOrigins []string `mapstructure:"allowed_origins" yaml:"allowed_origins"`
}

// Default returns the documented default configuration.
func Default() *Config {
	return &Config{
		Server: Server{
			Addr:            ":8080",
			PublicURL:       "",
			ReadTimeout:     15 * time.Second,
			WriteTimeout:    15 * time.Second,
			ShutdownTimeout: 30 * time.Second,
			CORSOrigins:     []string{},
		},
		Database: Database{
			Driver:          "sqlite",
			URL:             "./data/cloud.db",
			MaxOpenConns:    25,
			MaxIdleConns:    5,
			ConnMaxLifetime: 5 * time.Minute,
			AutoMigrate:     true,
		},
		Clerk: Clerk{
			Issuer:            "",
			JWKSURL:           "",
			Audience:          "",
			AuthorizedParties: []string{},
			WebhookTolerance:  5 * time.Minute,
			JWKSCacheTTL:      10 * time.Minute,
		},
		Secrets: Secrets{
			Provider: "env",
			File:     "",
			Prefix:   EnvPrefix + "_",
		},
		Providers: Providers{
			Enabled:       []string{},
			DefaultRegion: "",
			TenantCreds:   false,
		},
		Provisioner: Provisioner{
			TerraformPath: "tofu",
			WorkDir:       "./data/terraform",
			StateBackend:  "local",
			StateLocalDir: "./data/tfstate",
			PlanTimeout:   10 * time.Minute,
			ApplyTimeout:  30 * time.Minute,
			MaxConcurrent: 4,
		},
		Telemetry: Telemetry{
			LogLevel:    "info",
			LogFormat:   "text",
			MetricsAddr: "",
		},
		Console: Console{
			BaseURL:        "",
			AllowedOrigins: []string{},
		},
	}
}

// Load reads configuration from path with environment overrides. An empty path
// means defaults plus environment only. An explicit path that cannot be read
// is an error.
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	setDefaults(v)

	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("config: read %s: %w", path, err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) {
		dc.DecodeHook = mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
		)
	}); err != nil {
		return nil, fmt.Errorf("config: decode: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate rejects structurally invalid configuration. It never requires
// Clerk or provider credentials; those gate capabilities, not boot.
func (c *Config) Validate() error {
	switch c.Database.Driver {
	case "sqlite":
		if c.Database.URL == "" {
			c.Database.URL = Default().Database.URL
		}
	case "postgres":
		if strings.TrimSpace(c.Database.URL) == "" {
			return fmt.Errorf("config: database.url is required when database.driver is postgres")
		}
	default:
		return fmt.Errorf("config: database.driver must be one of postgres|sqlite, got %q", c.Database.Driver)
	}

	switch c.Secrets.Provider {
	case "", "env", "file", "chain":
		// ok ("": treated as env by the secrets package)
	default:
		return fmt.Errorf("config: secrets.provider must be one of env|file|chain, got %q", c.Secrets.Provider)
	}
	if c.Secrets.Provider == "file" && strings.TrimSpace(c.Secrets.File) == "" {
		return fmt.Errorf("config: secrets.file is required when secrets.provider is file")
	}

	switch c.Provisioner.StateBackend {
	case "", "local":
		// ok
	case "s3":
		if missing := missingS3Keys(); len(missing) > 0 {
			return fmt.Errorf("config: provisioner.state_backend is s3 but missing keys: %s", strings.Join(missing, ", "))
		}
	default:
		return fmt.Errorf("config: provisioner.state_backend must be one of local|s3, got %q", c.Provisioner.StateBackend)
	}

	switch c.Telemetry.LogLevel {
	case "debug", "info", "warn", "error":
		// ok
	default:
		return fmt.Errorf("config: telemetry.log_level must be one of debug|info|warn|error, got %q", c.Telemetry.LogLevel)
	}
	switch c.Telemetry.LogFormat {
	case "json", "text":
		// ok
	default:
		return fmt.Errorf("config: telemetry.log_format must be one of json|text, got %q", c.Telemetry.LogFormat)
	}

	for name, d := range map[string]time.Duration{
		"server.read_timeout":        c.Server.ReadTimeout,
		"server.write_timeout":       c.Server.WriteTimeout,
		"server.shutdown_timeout":    c.Server.ShutdownTimeout,
		"database.conn_max_lifetime": c.Database.ConnMaxLifetime,
		"clerk.webhook_tolerance":    c.Clerk.WebhookTolerance,
		"clerk.jwks_cache_ttl":       c.Clerk.JWKSCacheTTL,
		"provisioner.plan_timeout":   c.Provisioner.PlanTimeout,
		"provisioner.apply_timeout":  c.Provisioner.ApplyTimeout,
	} {
		if d < 0 {
			return fmt.Errorf("config: %s must not be negative, got %s", name, d)
		}
	}
	for name, n := range map[string]int{
		"database.max_open_conns":    c.Database.MaxOpenConns,
		"database.max_idle_conns":    c.Database.MaxIdleConns,
		"provisioner.max_concurrent": c.Provisioner.MaxConcurrent,
	} {
		if n < 0 {
			return fmt.Errorf("config: %s must not be negative, got %d", name, n)
		}
	}
	return nil
}

// missingS3Keys reports the S3 state-backend env vars that are absent. The S3
// credentials live in the secrets provider, not in Config; Validate consults
// the environment so that state_backend=s3 without credentials fails fast
// instead of mid-apply.
func missingS3Keys() []string {
	var missing []string
	for _, env := range []string{
		"CARBONCLOUD_STATE_S3_BUCKET",
		"CARBONCLOUD_STATE_S3_ACCESS_KEY_ID",
		"CARBONCLOUD_STATE_S3_SECRET_ACCESS_KEY",
	} {
		if strings.TrimSpace(os.Getenv(env)) == "" {
			missing = append(missing, env)
		}
	}
	return missing
}

func setDefaults(v *viper.Viper) {
	d := Default()
	v.SetDefault("server.addr", d.Server.Addr)
	v.SetDefault("server.public_url", d.Server.PublicURL)
	v.SetDefault("server.read_timeout", d.Server.ReadTimeout)
	v.SetDefault("server.write_timeout", d.Server.WriteTimeout)
	v.SetDefault("server.shutdown_timeout", d.Server.ShutdownTimeout)
	v.SetDefault("server.cors_origins", d.Server.CORSOrigins)

	v.SetDefault("database.driver", d.Database.Driver)
	v.SetDefault("database.url", d.Database.URL)
	v.SetDefault("database.max_open_conns", d.Database.MaxOpenConns)
	v.SetDefault("database.max_idle_conns", d.Database.MaxIdleConns)
	v.SetDefault("database.conn_max_lifetime", d.Database.ConnMaxLifetime)
	v.SetDefault("database.auto_migrate", d.Database.AutoMigrate)

	v.SetDefault("clerk.issuer", d.Clerk.Issuer)
	v.SetDefault("clerk.jwks_url", d.Clerk.JWKSURL)
	v.SetDefault("clerk.audience", d.Clerk.Audience)
	v.SetDefault("clerk.authorized_parties", d.Clerk.AuthorizedParties)
	v.SetDefault("clerk.webhook_tolerance", d.Clerk.WebhookTolerance)
	v.SetDefault("clerk.jwks_cache_ttl", d.Clerk.JWKSCacheTTL)

	v.SetDefault("secrets.provider", d.Secrets.Provider)
	v.SetDefault("secrets.file", d.Secrets.File)
	v.SetDefault("secrets.prefix", d.Secrets.Prefix)

	v.SetDefault("providers.enabled", d.Providers.Enabled)
	v.SetDefault("providers.default_region", d.Providers.DefaultRegion)
	v.SetDefault("providers.tenant_credentials", d.Providers.TenantCreds)

	v.SetDefault("provisioner.terraform_path", d.Provisioner.TerraformPath)
	v.SetDefault("provisioner.work_dir", d.Provisioner.WorkDir)
	v.SetDefault("provisioner.state_backend", d.Provisioner.StateBackend)
	v.SetDefault("provisioner.state_local_dir", d.Provisioner.StateLocalDir)
	v.SetDefault("provisioner.plan_timeout", d.Provisioner.PlanTimeout)
	v.SetDefault("provisioner.apply_timeout", d.Provisioner.ApplyTimeout)
	v.SetDefault("provisioner.max_concurrent", d.Provisioner.MaxConcurrent)

	v.SetDefault("telemetry.log_level", d.Telemetry.LogLevel)
	v.SetDefault("telemetry.log_format", d.Telemetry.LogFormat)
	v.SetDefault("telemetry.metrics_addr", d.Telemetry.MetricsAddr)

	v.SetDefault("console.base_url", d.Console.BaseURL)
	v.SetDefault("console.allowed_origins", d.Console.AllowedOrigins)
}
