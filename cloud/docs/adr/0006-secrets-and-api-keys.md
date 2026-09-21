# ADR 0006 — Secrets and API keys: provided later, never required at build time

- Status: accepted
- Date: 2026-09-21

## Context

Every external credential (Clerk, cloud providers, S3 state backend, SMTP, Stripe)
will be supplied **after** the code exists. The build must not depend on any of
them, tests must run without them, and adding them later must not require code
changes.

## Decision

- **One interface:** `cloud/internal/cloud/secrets.Provider`
  `{ Get(ctx, key) (value, error); Kind() string; Health(ctx) error }`.
  Implementations: `env`, `file` (dotenv/JSON, SOPS-encrypted in prod), `aws-sm`,
  `gcp-sm`, `vault`. Selected by `SECRETS_PROVIDER`, composed in a chain
  (`env → file → cloud`) so any variable can be overridden locally without a
  cloud account.
- **Typed keys:** a `keys` package declares every credential the system understands
  (`clerk.issuer`, `clerk.jwks_url`, `clerk.secret_key`, `clerk.webhook_secret`,
  `provider.hetzner.token`, `provider.aws.access_key_id`, …, `state.s3.*`). Config
  validation checks *presence of the ones a feature needs*, and a feature whose key
  is missing is **disabled with an explicit status**, not crashed on. `/readyz`
  reports which capabilities are unavailable and why.
- **No defaults that look like secrets.** Placeholder values are detected and
  rejected (`CHANGEME`, `xxx`, `<...>`, empty).
- **Redaction is a test.** A test asserts that a provider token placed in the
  environment never appears in logs, error messages, Terraform command lines, or
  the audit log.
- **`.env.example`** enumerates every key with a comment on where to get it and
  which capability it unlocks — this is the contract for "API keys later".
- **Tenant-supplied provider credentials** (a tenant bringing their own cloud
  account for managed nodes) are stored with envelope encryption: per-org data key,
  master key from the secrets provider, ciphertext at rest, never returned by any
  read RPC (write-only fields).

## Consequences

- The system is runnable and testable today with zero external keys; capabilities
  light up as keys are added.
- Envelope encryption for tenant creds is real work in L3 but is the only
  defensible way to hold someone else's cloud token.
