# `cloud/internal/cloud/config`

YAML file + environment overrides, mirroring the OSS `internal/config` style.
Env prefix is `CARBONCLOUD_` with `.` replaced by `_`:
`CARBONCLOUD_DATABASE_URL`, `CARBONCLOUD_CLERK_ISSUER`, ... Environment always
wins over the file. `Load("")` means defaults plus environment only.

The process boots with zero external credentials. Clerk and provider keys are
**capabilities, not boot requirements** (ADR 0006): `Validate` never demands
them; the secrets lane reports them as disabled instead.

## Options

| Key | Env | Default | Notes |
| --- | --- | --- | --- |
| `server.addr` | `CARBONCLOUD_SERVER_ADDR` | `:8080` | listen address |
| `server.public_url` | `CARBONCLOUD_SERVER_PUBLIC_URL` | `""` | external URL, e.g. https://api.cloud.example |
| `server.read_timeout` | `CARBONCLOUD_SERVER_READ_TIMEOUT` | `15s` | |
| `server.write_timeout` | `CARBONCLOUD_SERVER_WRITE_TIMEOUT` | `15s` | |
| `server.shutdown_timeout` | `CARBONCLOUD_SERVER_SHUTDOWN_TIMEOUT` | `30s` | |
| `server.cors_origins` | `CARBONCLOUD_SERVER_CORS_ORIGINS` | `[]` | comma-separated in env |
| `database.driver` | `CARBONCLOUD_DATABASE_DRIVER` | `sqlite` | `postgres` \| `sqlite` |
| `database.url` | `CARBONCLOUD_DATABASE_URL` | `./data/cloud.db` | required when driver is postgres; sqlite file path otherwise |
| `database.max_open_conns` | `CARBONCLOUD_DATABASE_MAX_OPEN_CONNS` | `25` | |
| `database.max_idle_conns` | `CARBONCLOUD_DATABASE_MAX_IDLE_CONNS` | `5` | |
| `database.conn_max_lifetime` | `CARBONCLOUD_DATABASE_CONN_MAX_LIFETIME` | `5m` | |
| `database.auto_migrate` | `CARBONCLOUD_DATABASE_AUTO_MIGRATE` | `true` | |
| `clerk.issuer` | `CARBONCLOUD_CLERK_ISSUER` | `""` | empty disables the `clerk` capability |
| `clerk.jwks_url` | `CARBONCLOUD_CLERK_JWKS_URL` | `""` | override; derived from issuer by default |
| `clerk.audience` | `CARBONCLOUD_CLERK_AUDIENCE` | `""` | |
| `clerk.authorized_parties` | `CARBONCLOUD_CLERK_AUTHORIZED_PARTIES` | `[]` | |
| `clerk.webhook_tolerance` | `CARBONCLOUD_CLERK_WEBHOOK_TOLERANCE` | `5m` | |
| `clerk.jwks_cache_ttl` | `CARBONCLOUD_CLERK_JWKS_CACHE_TTL` | `10m` | |
| `secrets.provider` | `CARBONCLOUD_SECRETS_PROVIDER` | `env` | `env` \| `file` \| `chain` |
| `secrets.file` | `CARBONCLOUD_SECRETS_FILE` | `""` | dotenv/JSON path; required when provider is `file` |
| `secrets.prefix` | `CARBONCLOUD_SECRETS_PREFIX` | `CARBONCLOUD_` | env var prefix for the env provider |
| `providers.enabled` | `CARBONCLOUD_PROVIDERS_ENABLED` | `[]` | e.g. `generic,hetzner` |
| `providers.default_region` | `CARBONCLOUD_PROVIDERS_DEFAULT_REGION` | `""` | |
| `providers.tenant_credentials` | `CARBONCLOUD_PROVIDERS_TENANT_CREDENTIALS` | `false` | allow tenants to bring their own cloud account |
| `provisioner.terraform_path` | `CARBONCLOUD_PROVISIONER_TERRAFORM_PATH` | `tofu` | binary name/path |
| `provisioner.work_dir` | `CARBONCLOUD_PROVISIONER_WORK_DIR` | `./data/terraform` | |
| `provisioner.state_backend` | `CARBONCLOUD_PROVISIONER_STATE_BACKEND` | `local` | `local` \| `s3` (`s3` needs the `state.s3.*` keys) |
| `provisioner.state_local_dir` | `CARBONCLOUD_PROVISIONER_STATE_LOCAL_DIR` | `./data/tfstate` | |
| `provisioner.plan_timeout` | `CARBONCLOUD_PROVISIONER_PLAN_TIMEOUT` | `10m` | |
| `provisioner.apply_timeout` | `CARBONCLOUD_PROVISIONER_APPLY_TIMEOUT` | `30m` | |
| `provisioner.max_concurrent` | `CARBONCLOUD_PROVISIONER_MAX_CONCURRENT` | `4` | |
| `telemetry.log_level` | `CARBONCLOUD_TELEMETRY_LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error` |
| `telemetry.log_format` | `CARBONCLOUD_TELEMETRY_LOG_FORMAT` | `text` | `json` \| `text` (`text` = stderr-friendly) |
| `telemetry.metrics_addr` | `CARBONCLOUD_TELEMETRY_METRICS_ADDR` | `""` | empty disables the metrics server |
| `console.base_url` | `CARBONCLOUD_CONSOLE_BASE_URL` | `""` | |
| `console.allowed_origins` | `CARBONCLOUD_CONSOLE_ALLOWED_ORIGINS` | `[]` | |

## Validation rules

- `database.driver` must be `postgres` or `sqlite`; `postgres` requires `database.url`.
- `secrets.provider` must be `env`, `file`, or `chain`; `file` requires `secrets.file`.
- `provisioner.state_backend` must be `local` or `s3`; `s3` requires the
  `CARBONCLOUD_STATE_S3_{BUCKET,ACCESS_KEY_ID,SECRET_ACCESS_KEY}` keys present.
- `telemetry.log_level` and `telemetry.log_format` reject unknown values.
- No duration or limit may be negative.
