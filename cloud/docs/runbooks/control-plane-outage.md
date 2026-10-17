# Control-plane outage runbook

When `controld` is down or unhealthy (`/readyz` failing, compose `controld`
unhealthy, helm probes failing).

## 1. Confirm scope

```sh
docker compose -f cloud/deploy/compose/docker-compose.yml ps
curl -sS http://localhost:8080/readyz; echo
curl -sS http://localhost:8080/metrics | head   # obs RED metrics (w2-deploy)
```

`/healthz` = process alive, `/readyz` = can serve (DB reachable, required
capabilities loaded). Metrics keep working while degraded — check
`carboncloud_rpc_errors_total` for the failing procedure.

## 2. Read the logs (structured slog, request fields attached)

```sh
docker compose -f cloud/deploy/compose/docker-compose.yml logs --tail=200 controld
```

Every line carries `procedure`, `trace_id`, and (unless
`CARBONCLOUD_OBS_REDACT_ORG_ID=1`) `org_id` / `actor_id`. Copy the `trace_id`
of the first failure and grep for it to follow one request end to end.

## 3. Common causes

| Symptom | Likely cause | Fix |
|---|---|---|
| `readyz` 503 right after boot | Postgres not up / wrong `CARBONCLOUD_DATABASE_URL` | `docker compose up -d postgres`, verify `.env` |
| Auth failures spike on all RPCs | Clerk JWKS unreachable (keys rotate) | Check `CARBONCLOUD_CLERK_ISSUER`, see `key-rotation.md` |
| Nodes flapping offline | Noded can't reach controld | Check `CLOUD_CONTROL_PLANE_URL`, node network |
| OOM / restart loop | `controld.resources` too small (helm) | Raise limits, `helm upgrade` |

## 4. Node-side behaviour during an outage

Nodes buffer status and reconnect with backoff (w3-noded contract); workloads
already placed keep running. Do NOT drain nodes during a control-plane outage
— placement decisions need capacity data that is currently stale.

## 5. Recover

```sh
docker compose -f cloud/deploy/compose/docker-compose.yml up -d controld
# or: helm upgrade carbon-cloud ./cloud/deploy/helm/carbon-cloud
```

Verify `/readyz` returns 200, then watch `carboncloud_rpc_errors_total` return
to baseline before declaring the incident over.
