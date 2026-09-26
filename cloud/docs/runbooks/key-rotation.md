# Key rotation runbook

Covers: Clerk JWKS/issuer keys, `CARBONCLOUD_NODE_JOIN_TOKEN_SECRET` (join-token
HMAC pepper), `CARBONCLOUD_CRYPTO_ENVELOPE_MASTER_KEY`, provider tokens, and
the compose/helm secret plumbing. No key material ever lives in git.

## Principles

- Secrets arrive via env/file/secret-manager only (ADR 0006). Compose reads
  `.env` (copied from `.env.example`, never committed); helm reads the Secret
  named by `global.existingSecret` via `secretKeyRef` — never values.
- Rotation is dual-support then cutover: new value accepted alongside old
  until all outstanding artifacts (tokens, sessions) turn over, then old
  removed.

## Clerk keys

Clerk rotates signing keys on its own schedule; controld tracks them via JWKS:

1. Confirm `CARBONCLOUD_CLERK_ISSUER` / JWKS URL still point at the live issuer.
2. Auth-error spike after a Clerk rotation = stale JWKS cache: restart controld
   (or wait out `clerk.jwks_cache_ttl`, default 10m).
3. Webhook secret (`CARBONCLOUD_CLERK_WEBHOOK_SECRET`): create the new secret in
   Clerk, add it alongside the old, wait 24h, remove the old.

## Join-token pepper (`CARBONCLOUD_NODE_JOIN_TOKEN_SECRET`)

Rotating the pepper invalidates ALL outstanding unredeemed tokens (their HMACs
no longer verify). Already-joined nodes are unaffected (they hold identity,
not the token).

1. Announce a join freeze; let in-flight joins finish.
2. Set the new pepper on controld, restart.
3. Re-issue tokens for any pending joins; revoke leftovers from the old round.
4. There is no dual-pepper mode: keep the freeze window short.

## Envelope master key (`CARBONCLOUD_CRYPTO_ENVELOPE_MASTER_KEY`)

Tenant provider credentials are envelope-encrypted. Rotating the master key
requires re-wrapping data keys — coordinate with the maintainers of
`cloud/internal/cloud/secrets` before attempting; do NOT rotate by just
swapping the env var (old ciphertext becomes unreadable).

## Provider tokens (hetzner/aws/gcp/digitalocean/proxmox)

1. Create the new token in the provider console.
2. Update the secret store (`.env` / external Secret), restart controld.
3. A provider whose credentials are absent reports `Available() == false` with
   the missing key names — use that signal to confirm the rotation took.
4. Delete the old token in the provider console only after step 3 is green.

## Verify after any rotation

```sh
curl -sS http://localhost:8080/readyz; echo
curl -sS http://localhost:8080/metrics | grep carboncloud_rpc_errors_total
```

`readyz` 200 plus flat error counters = rotation clean.
