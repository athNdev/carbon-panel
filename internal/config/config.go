// SPDX-License-Identifier: MIT AND MPL-2.0
// This file includes DiscoPanel material under the MIT License,
// Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
// and still applies to that inherited material.
// Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
// If a copy of the MPL was not distributed with this file, You can obtain
// one at https://mozilla.org/MPL/2.0/.

package config

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

type Config struct {
	Server    ServerConfig    `mapstructure:"server" json:"server"`
	Database  DatabaseConfig  `mapstructure:"database" json:"database"`
	Docker    DockerConfig    `mapstructure:"docker" json:"docker"`
	Storage   StorageConfig   `mapstructure:"storage" json:"storage"`
	Proxy     ProxyConfig     `mapstructure:"proxy" json:"proxy"`
	Module    ModuleConfig    `mapstructure:"module" json:"module"`
	Minecraft MinecraftConfig `mapstructure:"minecraft" json:"minecraft"`
	Logging   LoggingConfig   `mapstructure:"logging" json:"logging"`
	Upload    UploadConfig    `mapstructure:"upload" json:"upload"`
	Auth      AuthConfig      `mapstructure:"auth" json:"auth"`
}

type AuthConfig struct {
	SessionTimeout  int         `mapstructure:"session_timeout" json:"session_timeout"`
	AnonymousAccess bool        `mapstructure:"anonymous_access" json:"anonymous_access"`
	JWTSecret       string      `mapstructure:"jwt_secret" json:"jwt_secret"`
	OIDC            OIDCConfig  `mapstructure:"oidc" json:"oidc"`
	Local           LocalConfig `mapstructure:"local" json:"local"`
	// AllowNoAuth permits running the panel with neither local auth nor OIDC
	// enabled. When false (default) every request is rejected instead of
	// being granted admin, and startup fails with a clear error.
	AllowNoAuth bool `mapstructure:"allow_no_auth" json:"allow_no_auth"`
	// RecoveryKeyFile is the on-disk break-glass key. Empty skips the file
	// and keeps the key only in the system setting. The panel sets this to
	// data/recovery.key at startup (MINE-209).
	RecoveryKeyFile string `mapstructure:"-" json:"-"`
}

type OIDCConfig struct {
	Enabled         bool              `mapstructure:"enabled" json:"enabled"`
	IssuerURI       string            `mapstructure:"issuer_uri" json:"issuer_uri"`
	ClientID        string            `mapstructure:"client_id" json:"client_id"`
	ClientSecret    string            `mapstructure:"client_secret" json:"client_secret"`
	RedirectURL     string            `mapstructure:"redirect_url" json:"redirect_url"`
	Scopes          []string          `mapstructure:"scopes" json:"scopes"`
	RoleClaim       string            `mapstructure:"role_claim" json:"role_claim"`
	RoleMapping     map[string]string `mapstructure:"role_mapping" json:"role_mapping"`
	RejectUnmapped  bool              `mapstructure:"reject_unmapped" json:"reject_unmapped"`
	SkipTLSVerify   bool              `mapstructure:"skip_tls_verify" json:"skip_tls_verify"`
	ExtraClaimsURL  string            `mapstructure:"extra_claims_url" json:"extra_claims_url"`
	ExtraClaimsKey  string            `mapstructure:"extra_claims_key" json:"extra_claims_key"`
	ExtraClaimsName string            `mapstructure:"extra_claims_name" json:"extra_claims_name"`
	RequiredClaim   string            `mapstructure:"required_claim" json:"required_claim"`
	RequiredValues  []string          `mapstructure:"required_values" json:"required_values"`
}

type LocalConfig struct {
	Enabled           bool `mapstructure:"enabled" json:"enabled"`
	AllowRegistration bool `mapstructure:"allow_registration" json:"allow_registration"`
}

type ServerConfig struct {
	Port           string   `mapstructure:"port" json:"port"`
	Host           string   `mapstructure:"host" json:"host"`
	ReadTimeout    int      `mapstructure:"read_timeout" json:"read_timeout"`
	WriteTimeout   int      `mapstructure:"write_timeout" json:"write_timeout"`
	IdleTimeout    int      `mapstructure:"idle_timeout" json:"idle_timeout"`
	UserAgent      string   `mapstructure:"user_agent" json:"user_agent"`
	TrustProxy     bool     `mapstructure:"trust_proxy" json:"trust_proxy"`
	AllowedOrigins []string `mapstructure:"allowed_origins" json:"allowed_origins"`
}

type DockerConfig struct {
	SyncInterval    int               `mapstructure:"sync_interval" json:"sync_interval"`
	Host            string            `mapstructure:"host" json:"host"`
	Version         string            `mapstructure:"version" json:"version"`
	NetworkName     string            `mapstructure:"network_name" json:"network_name"`
	NetworkMTU      int               `mapstructure:"network_mtu" json:"network_mtu"`
	RegistryURL     string            `mapstructure:"registry_url" json:"registry_url"`
	DNS             string            `mapstructure:"dns" json:"dns"`
	Labels          map[string]string `mapstructure:"labels" json:"labels"`
	EnableRateLimit bool              `mapstructure:"enable_rate_limit" json:"enable_rate_limit"`
	RateLimitPerMin int               `mapstructure:"rate_limit_per_min" json:"rate_limit_per_min"`
	RateLimitBurst  int               `mapstructure:"rate_limit_burst" json:"rate_limit_burst"`
	// ReconcilePurgeOrphans removes containers that this installation provably
	// created but the database no longer knows about. Disabled by default:
	// orphaned, legacy and foreign containers are quarantined and left running.
	ReconcilePurgeOrphans bool `mapstructure:"reconcile_purge_orphans" json:"reconcile_purge_orphans"`
}

type StorageConfig struct {
	DataDir               string                 `mapstructure:"data_dir" json:"data_dir"`
	BackupDir             string                 `mapstructure:"backup_dir" json:"backup_dir"`
	TempDir               string                 `mapstructure:"temp_dir" json:"temp_dir"`
	MaxUploadSize         int64                  `mapstructure:"max_upload_size" json:"max_upload_size"`
	MaxSnapshots          int                    `mapstructure:"max_snapshots" json:"max_snapshots"`
	SnapshotRetentionDays int                    `mapstructure:"snapshot_retention_days" json:"snapshot_retention_days"`
	BackupEncryption      BackupEncryptionConfig `mapstructure:"backup_encryption" json:"backup_encryption"`
	S3                    S3Config               `mapstructure:"s3" json:"s3"`
}

// BackupEncryptionConfig enables authenticated encryption of backup
// artifacts at rest and offsite (OSS-236). Encryption is explicitly disabled
// when no key material is configured. The key is 32 bytes encoded as hex or
// base64 and should come from the environment
// (CARBONPANEL_STORAGE_BACKUP_ENCRYPTION_KEY) rather than the config file;
// it is never serialized or logged. KeyID is a stable non-secret label
// recorded with every artifact for key rotation and recovery.
type BackupEncryptionConfig struct {
	KeyID string `mapstructure:"key_id" json:"key_id"`
	Key   string `mapstructure:"key" json:"-"`
}

// S3Config enables S3-compatible offsite backup upload (MINE-138).
// Credentials should come from env (S3_ACCESS_KEY / S3_SECRET_KEY override)
// rather than the config file.
type S3Config struct {
	Enabled         bool   `mapstructure:"enabled" json:"enabled"`
	Endpoint        string `mapstructure:"endpoint" json:"endpoint"`
	Region          string `mapstructure:"region" json:"region"`
	Bucket          string `mapstructure:"bucket" json:"bucket"`
	Prefix          string `mapstructure:"prefix" json:"prefix"`
	AccessKey       string `mapstructure:"access_key" json:"access_key"`
	SecretKey       string `mapstructure:"secret_key" json:"secret_key"`
	ForcePathStyle  bool   `mapstructure:"force_path_style" json:"force_path_style"`
	DeleteLocalCopy bool   `mapstructure:"delete_local_copy" json:"delete_local_copy"`
}

type ProxyConfig struct {
	Enabled      bool   `mapstructure:"enabled" json:"enabled"`
	BaseURL      string `mapstructure:"base_url" json:"base_url"`
	ListenPort   int    `mapstructure:"listen_port" json:"listen_port"`   // Primary listen port
	ListenPorts  []int  `mapstructure:"listen_ports" json:"listen_ports"` // Multiple listen ports
	PortRangeMin int    `mapstructure:"port_range_min" json:"port_range_min"`
	PortRangeMax int    `mapstructure:"port_range_max" json:"port_range_max"`
	ValkeyURL    string `mapstructure:"valkey_url" json:"valkey_url"` // Valkey / Dragonfly Redis KV URL (e.g. redis://127.0.0.1:6379)
	// AcceptsTransfers is handshake NextState=3. Nil and true both accept.
	// Velocity defaults this to false; Carbon defaults to true because a
	// rejected transfer is a player-visible outage on a hostname router (MINE-203).
	AcceptsTransfers *bool `mapstructure:"accepts_transfers" json:"accepts_transfers"`
	// LoginRateLimitMS is the per-IP gap between Gate handshakes. 0 disables.
	LoginRateLimitMS int `mapstructure:"login_ratelimit_ms" json:"login_ratelimit_ms"`
}

type ModuleConfig struct {
	Enabled      bool `mapstructure:"enabled" json:"enabled"`
	PortRangeMin int  `mapstructure:"port_range_min" json:"port_range_min"`
	PortRangeMax int  `mapstructure:"port_range_max" json:"port_range_max"`
}

type DatabaseConfig struct {
	Path            string `mapstructure:"path" json:"path"`
	MaxConnections  int    `mapstructure:"max_connections" json:"max_connections"`
	MaxIdleConns    int    `mapstructure:"max_idle_conns" json:"max_idle_conns"`
	ConnMaxLifetime int    `mapstructure:"conn_max_lifetime" json:"conn_max_lifetime"`
	AutoMigrate     bool   `mapstructure:"auto_migrate" json:"auto_migrate"`
	// PanelBackup is "daily" or "off". Daily writes a VACUUM INTO snapshot of
	// this database. It is not Litestream and it does not restore automatically.
	PanelBackup string `mapstructure:"panel_backup" json:"panel_backup"`
	// PanelBackupKeep is how many daily snapshots to retain. Older files are
	// removed only after a new snapshot passes integrity_check.
	PanelBackupKeep int `mapstructure:"panel_backup_keep" json:"panel_backup_keep"`
}

type MinecraftConfig struct {
	ResetGlobal  bool           `mapstructure:"reset_global" json:"reset_global"`
	GlobalConfig map[string]any `mapstructure:"global_config" json:"global_config"`
}

type LoggingConfig struct {
	Enabled    bool   `mapstructure:"enabled" json:"enabled"`
	FilePath   string `mapstructure:"file_path" json:"file_path"`
	MaxSize    int    `mapstructure:"max_size" json:"max_size"`
	MaxBackups int    `mapstructure:"max_backups" json:"max_backups"`
	MaxAge     int    `mapstructure:"max_age" json:"max_age"`
	Compress   bool   `mapstructure:"compress" json:"compress"`
}

type UploadConfig struct {
	SessionTTL       int   `mapstructure:"session_ttl" json:"session_ttl"`               // Minutes, default 240 (4 hours)
	DefaultChunkSize int   `mapstructure:"default_chunk_size" json:"default_chunk_size"` // Bytes, default 5MB, client overriden
	MaxChunkSize     int   `mapstructure:"max_chunk_size" json:"max_chunk_size"`         // Bytes, default 10MB, server overriden
	MaxUploadSize    int64 `mapstructure:"max_upload_size" json:"max_upload_size"`       // Bytes, default 0 (unlimited)
}

func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set config name and type
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	// Add config paths
	if configPath != "" {
		v.AddConfigPath(configPath)
	}
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	v.AddConfigPath("/etc/carbon-panel")

	// Set defaults
	setDefaults(v)

	// Enable environment variables
	v.SetEnvPrefix("CARBONPANEL")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found; use defaults and environment
	}

	// Flatten nested maps like docker.labels into map[string]string
	flattenMapSetting(v, "docker.labels")

	// Unmarshal config with a decode hook that handles JSON strings from env
	var cfg Config
	if err := v.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) {
		dc.DecodeHook = mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
			jsonStringToMapHook(),
		)
	}); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	// Validate and expand paths
	if err := validateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("config validation error: %w", err)
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.port", "8080")
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.read_timeout", 15)
	v.SetDefault("server.write_timeout", 15)
	v.SetDefault("server.idle_timeout", 60)
	v.SetDefault("server.user_agent", "CarbonPanel/1.0 (github.com/athNdev/carbon-panel)")
	v.SetDefault("server.trust_proxy", false)
	v.SetDefault("server.allowed_origins", []string{})

	// Database defaults
	v.SetDefault("database.path", "./data/carbon-panel.db")
	v.SetDefault("database.max_connections", 25)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("database.conn_max_lifetime", 300)
	v.SetDefault("database.auto_migrate", true)
	v.SetDefault("database.panel_backup", "daily")
	v.SetDefault("database.panel_backup_keep", 7)

	// Docker defaults
	v.SetDefault("docker.sync_interval", 5)
	v.SetDefault("docker.host", "unix:///var/run/docker.sock")
	v.SetDefault("docker.version", "")
	v.SetDefault("docker.network_name", "carbon-panel-network")
	v.SetDefault("docker.network_mtu", 0)
	v.SetDefault("docker.registry_url", "")
	v.SetDefault("docker.dns", "")
	v.SetDefault("docker.labels", map[string]string{})
	v.SetDefault("docker.enable_rate_limit", true)
	v.SetDefault("docker.rate_limit_per_min", 10)
	v.SetDefault("docker.rate_limit_burst", 20)

	// Storage defaults
	dataDir, err := filepath.Abs("./data")
	if err != nil {
		panic("Unable to find data dir")
	}
	v.SetDefault("storage.data_dir", dataDir)
	v.SetDefault("storage.backup_dir", "./backups")
	v.SetDefault("storage.temp_dir", "./tmp")
	v.SetDefault("storage.max_upload_size", 500*1024*1024) // 500MB
	v.SetDefault("storage.max_snapshots", 5)
	v.SetDefault("storage.snapshot_retention_days", 0) // 0 = unlimited age

	// Proxy defaults
	v.SetDefault("proxy.enabled", false)
	v.SetDefault("proxy.base_url", "")
	v.SetDefault("proxy.listen_port", 25565)
	v.SetDefault("proxy.listen_ports", []int{25565})
	v.SetDefault("proxy.port_range_min", 25565)
	v.SetDefault("proxy.port_range_max", 25665)
	v.SetDefault("proxy.accepts_transfers", true)
	v.SetDefault("proxy.login_ratelimit_ms", 3000)

	// Module defaults
	v.SetDefault("module.enabled", true)
	v.SetDefault("module.port_range_min", 8100)
	v.SetDefault("module.port_range_max", 8199)

	v.SetDefault("minecraft.reset_global", false)

	// Logging defaults
	v.SetDefault("logging.enabled", true)
	v.SetDefault("logging.file_path", "./data/carbon-panel.log")
	v.SetDefault("logging.max_size", 10)   // 10 MB
	v.SetDefault("logging.max_backups", 5) // keep 5
	v.SetDefault("logging.max_age", 30)    // 30 days
	v.SetDefault("logging.compress", true) // compress rotated

	// Auth defaults
	v.SetDefault("auth.session_timeout", 86400)
	v.SetDefault("auth.anonymous_access", false)
	v.SetDefault("auth.jwt_secret", "")
	v.SetDefault("auth.oidc.enabled", false)
	v.SetDefault("auth.oidc.issuer_uri", "")
	v.SetDefault("auth.oidc.client_id", "")
	v.SetDefault("auth.oidc.client_secret", "")
	v.SetDefault("auth.oidc.redirect_url", "")
	v.SetDefault("auth.oidc.scopes", []string{"openid", "profile", "email"})
	v.SetDefault("auth.oidc.role_claim", "groups")
	v.SetDefault("auth.oidc.role_mapping", map[string]string{})
	v.SetDefault("auth.oidc.reject_unmapped", false)
	v.SetDefault("auth.oidc.skip_tls_verify", false)
	v.SetDefault("auth.oidc.extra_claims_url", "")
	v.SetDefault("auth.oidc.extra_claims_key", "")
	v.SetDefault("auth.oidc.extra_claims_name", "")
	v.SetDefault("auth.oidc.required_claim", "")
	v.SetDefault("auth.oidc.required_values", []string{})
	v.SetDefault("auth.local.enabled", true)
	v.SetDefault("auth.local.allow_registration", false)
	v.SetDefault("auth.allow_no_auth", false)

	// Upload defaults
	v.SetDefault("upload.session_ttl", 240)                // 4 hours (in minutes)
	v.SetDefault("upload.default_chunk_size", 5*1024*1024) // 5MB
	v.SetDefault("upload.max_chunk_size", 10*1024*1024)    // 10MB
	v.SetDefault("upload.max_upload_size", 0)              // unlimited
}

// Validate performs semantic and boundary validation of the configuration (CFG-1).
func (c *Config) Validate() error {
	return validateConfig(c)
}

func validateConfig(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config cannot be nil")
	}

	// Validate server settings
	if cfg.Server.Port != "" {
		portNum, err := strconv.Atoi(cfg.Server.Port)
		if err != nil || portNum < 1 || portNum > 65535 {
			return fmt.Errorf("server port must be a valid port number between 1 and 65535, got %q", cfg.Server.Port)
		}
	}
	if cfg.Server.ReadTimeout < 0 || cfg.Server.WriteTimeout < 0 || cfg.Server.IdleTimeout < 0 {
		return fmt.Errorf("server timeouts cannot be negative")
	}

	// Validate database settings
	if strings.TrimSpace(cfg.Database.Path) == "" {
		return fmt.Errorf("database path cannot be empty")
	}
	var err error
	cfg.Database.Path, err = filepath.Abs(cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("invalid database path: %w", err)
	}

	// Validate storage directories
	if strings.TrimSpace(cfg.Storage.DataDir) == "" {
		return fmt.Errorf("storage data directory cannot be empty")
	}
	cfg.Storage.DataDir, err = filepath.Abs(cfg.Storage.DataDir)
	if err != nil {
		return fmt.Errorf("invalid data directory: %w", err)
	}

	if strings.TrimSpace(cfg.Storage.BackupDir) == "" {
		return fmt.Errorf("storage backup directory cannot be empty")
	}
	cfg.Storage.BackupDir, err = filepath.Abs(cfg.Storage.BackupDir)
	if err != nil {
		return fmt.Errorf("invalid backup directory: %w", err)
	}

	if strings.TrimSpace(cfg.Storage.TempDir) == "" {
		return fmt.Errorf("storage temp directory cannot be empty")
	}
	cfg.Storage.TempDir, err = filepath.Abs(cfg.Storage.TempDir)
	if err != nil {
		return fmt.Errorf("invalid temp directory: %w", err)
	}

	if cfg.Storage.MaxSnapshots < 0 {
		return fmt.Errorf("storage.max_snapshots cannot be negative, got: %d", cfg.Storage.MaxSnapshots)
	}
	if cfg.Storage.SnapshotRetentionDays < 0 {
		return fmt.Errorf("storage.snapshot_retention_days cannot be negative, got: %d", cfg.Storage.SnapshotRetentionDays)
	}

	// Validate upload limits
	if cfg.Upload.SessionTTL <= 0 {
		return fmt.Errorf("upload session TTL must be positive, got: %d", cfg.Upload.SessionTTL)
	}
	if cfg.Upload.DefaultChunkSize <= 0 {
		return fmt.Errorf("upload default chunk size must be positive, got: %d", cfg.Upload.DefaultChunkSize)
	}
	if cfg.Upload.MaxChunkSize < cfg.Upload.DefaultChunkSize {
		return fmt.Errorf("upload max chunk size (%d) cannot be smaller than default chunk size (%d)", cfg.Upload.MaxChunkSize, cfg.Upload.DefaultChunkSize)
	}

	// Validate S3 storage settings if enabled
	if cfg.Storage.S3.Enabled {
		if strings.TrimSpace(cfg.Storage.S3.Bucket) == "" {
			return fmt.Errorf("s3 bucket cannot be empty when s3 storage is enabled")
		}
	}

	// Validate auth settings
	if cfg.Auth.SessionTimeout <= 0 {
		return fmt.Errorf("auth session timeout must be positive, got: %d", cfg.Auth.SessionTimeout)
	}
	if cfg.Auth.OIDC.Enabled {
		if strings.TrimSpace(cfg.Auth.OIDC.IssuerURI) == "" {
			return fmt.Errorf("oidc issuer URI cannot be empty when oidc is enabled")
		}
		if strings.TrimSpace(cfg.Auth.OIDC.ClientID) == "" {
			return fmt.Errorf("oidc client ID cannot be empty when oidc is enabled")
		}
	}

	// Validate port ranges
	if cfg.Proxy.PortRangeMin < 1 || cfg.Proxy.PortRangeMin > 65535 {
		return fmt.Errorf("proxy port range min must be between 1 and 65535, got: %d", cfg.Proxy.PortRangeMin)
	}
	if cfg.Proxy.PortRangeMax < 1 || cfg.Proxy.PortRangeMax > 65535 {
		return fmt.Errorf("proxy port range max must be between 1 and 65535, got: %d", cfg.Proxy.PortRangeMax)
	}
	if cfg.Proxy.PortRangeMin >= cfg.Proxy.PortRangeMax {
		return fmt.Errorf("proxy port range min must be less than max")
	}
	if cfg.Proxy.ListenPort < 1 || cfg.Proxy.ListenPort > 65535 {
		return fmt.Errorf("proxy listen port must be between 1 and 65535, got: %d", cfg.Proxy.ListenPort)
	}

	if cfg.Module.PortRangeMin < 1 || cfg.Module.PortRangeMin > 65535 {
		return fmt.Errorf("module port range min must be between 1 and 65535, got: %d", cfg.Module.PortRangeMin)
	}
	if cfg.Module.PortRangeMax < 1 || cfg.Module.PortRangeMax > 65535 {
		return fmt.Errorf("module port range max must be between 1 and 65535, got: %d", cfg.Module.PortRangeMax)
	}
	if cfg.Module.PortRangeMin >= cfg.Module.PortRangeMax {
		return fmt.Errorf("module port range min must be less than max")
	}

	// Ensure ListenPorts includes Primary ListenPort
	if cfg.Proxy.Enabled {
		if len(cfg.Proxy.ListenPorts) == 0 {
			cfg.Proxy.ListenPorts = []int{cfg.Proxy.ListenPort}
		} else {
			// Make sure the primary port is in the list
			hasPort := slices.Contains(cfg.Proxy.ListenPorts, cfg.Proxy.ListenPort)
			if !hasPort {
				cfg.Proxy.ListenPorts = append([]int{cfg.Proxy.ListenPort}, cfg.Proxy.ListenPorts...)
			}
		}
	}

	// Validate custom Docker labels do not use reserved namespace 'carbon-panel.'
	for k := range cfg.Docker.Labels {
		if strings.HasPrefix(k, "carbon-panel.") {
			return fmt.Errorf("custom docker labels cannot begin with 'carbon-panel.', namespace reserved for internal management, invalid key: %s", k)
		}
	}

	return nil
}

// Decodes JSON object strings into map types
func jsonStringToMapHook() mapstructure.DecodeHookFuncType {
	return func(from, to reflect.Type, data any) (any, error) {
		if from.Kind() != reflect.String || to.Kind() != reflect.Map {
			return data, nil
		}
		var m any
		if err := json.Unmarshal([]byte(data.(string)), &m); err != nil {
			return data, nil
		}
		return m, nil
	}
}

// flattenMapSetting flattens a nested viper setting into a flat map[string]string.
// For example, docker.labels.com.example.enable: true becomes {"com.example.enable": "true"}.
func flattenMapSetting(v *viper.Viper, key string) {
	val := v.Get(key)
	if val == nil {
		return
	}

	m, ok := val.(map[string]any)
	if !ok {
		return
	}

	flat := make(map[string]string)
	flattenMap(m, "", flat)
	v.Set(key, flat)
}

func flattenMap(src map[string]any, prefix string, dst map[string]string) {
	for k, v := range src {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch val := v.(type) {
		case map[string]any:
			flattenMap(val, key, dst)
		default:
			dst[key] = fmt.Sprintf("%v", val)
		}
	}
}
