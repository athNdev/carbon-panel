# Node join failure runbook

A new node fails to join (`cloudctl` join / noded first boot never becomes
`ready` in the registry).

## 1. Identify the stage

Join is: issue token → install noded → noded presents token (`AgentService.JoinNode`)
→ controld verifies signature, org, TTL, single-use → node appears in registry.

```sh
docker compose -f cloud/deploy/compose/docker-compose.yml logs --tail=200 noded controld
```

## 2. Match the error

| Error | Meaning | Fix |
|---|---|---|
| `token unknown` | Typo, or token issued by another org/control plane | Re-copy from `cloudctl nodes join-token issue` |
| `token expired` | Past TTL | Issue a fresh token (tokens are short-lived by design) |
| `token revoked` | Revoked after issue | Issue a fresh token; audit who revoked it |
| `token redeemed` | Single-use already consumed | Tokens are single-use: issue one token per node, never reuse |
| signature invalid | Wrong `CARBONCLOUD_NODE_JOIN_TOKEN_SECRET` on controld vs issuer | Rotate per `key-rotation.md`; all join paths share one pepper |
| org mismatch | Token's org ≠ target org | Issue the token in the correct org context |
| connection refused / TLS | Noded can't reach controld | Check `CLOUD_CONTROL_PLANE_URL`, firewall, ingress TLS |

## 3. Debug checklist

1. `CLOUD_CONTROL_PLANE_URL` reachable from the node?
   `curl -sS $CLOUD_CONTROL_PLANE_URL/healthz`.
2. Token format `ccj_<id>.<hmac>`: both halves present, no trailing newline
   from copy-paste.
3. Clock skew: TTL is validated against controld's clock; >1 min skew can
   falsely expire tokens — sync with NTP.
4. Org context: the token is org-bound; joining a different org always fails.

## 4. Escalate

If the token verifies but the node stays `offline`: check heartbeats
(`LastHeartbeat` staleness) — that is a connectivity/heartbeat problem, not a
join problem. See `control-plane-outage.md` §4.
