# Lane `cfg` — configuration and secret providers

Branch: `cloud/cfg`
Write scope: `cloud/internal/cloud/config/**`, `cloud/internal/cloud/secrets/**`,
`cloud/deploy/docker/.env.example`, `cloud/deploy/docker/controld.dev.yaml`,
`cloud/deploy/docker/noded.dev.yaml`

Depends on: nothing (plus `cloud/internal/cloud/keys`, owned by the orchestrator).

## Goal

Make Carbon Cloud configurable and startable-without-any-secrets. The control
plane must boot, report exactly which capabilities are unavailable, and refuse
only the operations that need a missing key.

## `cloud/internal/cloud/config`

Mirror the OSS style (`internal/config/config.go`): YAML file + environment
overrides. Env prefix is `CARBONCLOUD_` with `.` replaced by `_`
(`CARBONCLOUD_DATABASE_URL`, `CARBONCLOUD_CLERK_ISSUER`, ...).

```go
func Default() *Config
func Load(path string) (*Config, error)   // empty path = defaults + env only
func (c *Config) Validate() error
```

Required shape (names are a contract, keep them):

```go
type Config struct {
    Server      Server      `yaml:"server"`
    Database    Database    `yaml:"database"`
    Clerk       Clerk       `yaml:"clerk"`
    Secrets     Secrets     `yaml:"secrets"`
    Providers   Providers   `yaml:"providers"`
    Provisioner Provisioner `yaml:"provisioner"`
    Telemetry   Telemetry   `yaml:"telemetry"`
    Console     Console     `yaml:"console"`
}

type Server struct {
    Addr            string        `yaml:"addr"`              // default ":8080"
    PublicURL       string        `yaml:"public_url"`         // e.g. https://api.cloud.example
    ReadTimeout     time.Duration `yaml:"read_timeout"`
    WriteTimeout    time.Duration `yaml:"write_timeout"`
    ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
    CORSOrigins     []string      `yaml:"cors_origins"`
}

type Database struct {
    Driver          string        `yaml:"driver"`            // "postgres" | "sqlite"
    URL             string        `yaml:"url"`               // postgres DSN; sqlite = file path
    MaxOpenConns    int           `yaml:"max_open_conns"`
    MaxIdleConns    int           `yaml:"max_idle_conns"`
    ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
    AutoMigrate     bool          `yaml:"auto_migrate"`
}

type Clerk struct {
    Issuer            string        `yaml:"issuer"`
    JWKSURL           string        `yaml:"jwks_url"`
    Audience          string        `yaml:"audience"`
    AuthorizedParties []string      `yaml:"authorized_parties"`
    WebhookTolerance  time.Duration `yaml:"webhook_tolerance"`
    JWKSCacheTTL      time.Duration `yaml:"jwks_cache_ttl"`
}

type Secrets struct {
    Provider string `yaml:"provider"` // "env" | "file" | "chain"
    File     string `yaml:"file"`     // dotenv/JSON path
    Prefix   string `yaml:"prefix"`   // env var prefix, default CARBONCLOUD_
}

type Providers struct {
    Enabled        []string `yaml:"enabled"`
    DefaultRegion  string   `yaml:"default_region"`
    TenantCreds    bool     `yaml:"tenant_credentials"`
}

type Provisioner struct {
    TerraformPath  string        `yaml:"terraform_path"` // binary name/path, default "tofu"
    WorkDir        string        `yaml:"work_dir"`
    StateBackend   string        `yaml:"state_backend"`  // "local" | "s3"
    StateLocalDir  string        `yaml:"state_local_dir"`
    PlanTimeout    time.Duration `yaml:"plan_timeout"`
    ApplyTimeout   time.Duration `yaml:"apply_timeout"`
    MaxConcurrent  int           `yaml:"max_concurrent"`
}

type Telemetry struct {
    LogLevel    string `yaml:"log_level"`   // debug|info|warn|error
    LogFormat   string `yaml:"log_format"`  // json|text
    MetricsAddr string `yaml:"metrics_addr"` // empty disables the metrics server
}

type Console struct {
    BaseURL        string   `yaml:"base_url"`
    AllowedOrigins []string `yaml:"allowed_origins"`
}
```

`Validate()` must at minimum:
- require `database.driver` ∈ {postgres, sqlite}; require `database.url` when the
  driver is postgres; apply sane defaults otherwise (all fields above have a
  documented default and `Default()` returns them);
- reject a `secrets.provider` it does not implement;
- reject `provisioner.state_backend == "s3"` without the S3 keys present;
- reject durations/limits that are negative.
- **Not** require Clerk or provider keys — those are capabilities, not boot
  requirements (see ADR 0006).

## `cloud/internal/cloud/secrets`

```go
var ErrNotFound = errors.New("secrets: not found")

type Provider interface {
    Kind() string
    Get(ctx context.Context, key string) (string, error) // ErrNotFound when absent
    Health(ctx context.Context) error
}

func NewEnv(prefix string) Provider
func NewFile(path string) (Provider, error)        // .env / KEY=VALUE or JSON
func NewChain(providers ...Provider) Provider      // first hit wins

type Resolver struct{ /* ... */ }
func NewResolver(p Provider) *Resolver
func (r *Resolver) Get(ctx context.Context, key string) (string, error)
func (r *Resolver) Has(ctx context.Context, key string) bool
func (r *Resolver) Missing(ctx context.Context, keys []string) []string
func (r *Resolver) Require(ctx context.Context, keys ...string) error // error lists all missing

type Capability struct {
    ID           string
    Enabled      bool
    RequiredKeys []string
    MissingKeys  []string
    Detail       string
}
func (r *Resolver) Capability(ctx context.Context, id string) Capability
func (r *Resolver) Capabilities(ctx context.Context) []Capability // uses keys.Capabilities()
func FromConfig(cfg config.Secrets) (Provider, error)
```

Rules:
- Env provider maps `clerk.issuer` → `CARBONCLOUD_CLERK_ISSUER`.
- Chain order: env → file → (future: aws-sm/vault). Provider name in config picks
  the composition; `chain` means all of them in that order.
- A value that `keys.LooksLikePlaceholder` rejects must be treated as **absent**
  (`ErrNotFound`), so a half-filled `.env` behaves like a missing key instead of
  silently sending garbage to an API.
- `Capability.Detail` must be a specific human sentence naming the missing keys,
  e.g. `"disabled: missing CARBONCLOUD_CLERK_ISSUER"`.

## Deliverables

1. The two packages above, with tests:
   - config: defaults are stable; YAML → struct; env override wins; each missing
     required field produces a specific error; unknown value for an enum field is
     rejected.
   - secrets: env lookup, file lookup, chain precedence, `ErrNotFound`, placeholder
     rejection, `Missing`/`Require` error content, `Capabilities()` marks clerk
     disabled with no issuer and enabled with one.
2. `cloud/deploy/docker/.env.example` enumerating **every** key from
   `keys.All()` with a one-line comment saying where to get it and which
   capability it unlocks. Placeholder values only.
3. `cloud/deploy/docker/controld.dev.yaml` and `noded.dev.yaml`: a working local
   dev config (sqlite or a commented postgres URL, telemetry to stderr, CORS for
   `http://localhost:5173`).
4. `cloud/internal/cloud/config/README.md`: the full option table.

## Acceptance

```sh
gofmt -l ./cloud/... # empty
go build ./cloud/... && go vet ./cloud/... && go test ./cloud/...
./cmd-less check: no new module in go.mod (git diff --stat go.mod go.sum must be empty)
```

Report per conventions §6.
