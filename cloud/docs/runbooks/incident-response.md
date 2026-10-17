# Incident Response Runbook

Operational procedures for triaging, containing, mitigating, and reviewing production security and availability incidents in Carbon Cloud.

---

## 1. Severity Classification Matrix

| Level | Impact | Initial Response SLA | Notification Channels | Commander |
| :--- | :--- | :--- | :--- | :--- |
| **SEV1** | Total control-plane outage, cross-tenant data leak, active compromise of node agent or join secrets, widespread data corruption. | < 15 minutes | PagerDuty, Executive Team, #security-incidents | Head of Engineering / CISO |
| **SEV2** | Provisioning engine offline, API error rate > 5%, webhook delivery pipeline down, single-region agent connectivity failure. | < 30 minutes | PagerDuty, #cloud-oncall | Senior SRE / On-call Lead |
| **SEV3** | Degraded performance (P99 latency > 2s), non-critical background jobs delayed, single tenant capacity synchronization discrepancy. | < 2 hours | Slack #cloud-alerts | On-call Engineer |
| **SEV4** | Minor dashboard UI glitch, non-blocking documentation/metrics discrepancy. | Next business day | Jira / GitHub Issues | Assigned Engineer |

---

## 2. Immediate Containment Checklist (SEV1 / SEV2)

### A. Contain Compromised API Keys or Join Tokens
If a credential or join token is suspected of leaking or being abused:
```sh
# 1. Immediately revoke the compromised join token
cloudctl node revoke-token <token-id>

# 2. Immediately revoke the compromised API key
cloudctl apikey revoke <key-id>

# 3. Query the audit log for all actions executed using the compromised key
cloudctl audit tail --action=* --actor=<key-id> --limit=100
```

### B. Isolate Compromised or Rogue Compute Node
If a compute host displays anomalous behavior or telemetry:
```sh
# 1. Mark node as draining to prevent new workload placements
cloudctl node drain <node-id>

# 2. Terminate running workloads on the compromised host
cloudctl workload stop <workload-id>

# 3. Revoke node registration from the control plane
cloudctl node delete <node-id>
```

### C. Rotate Control-Plane Secrets
If master HMAC pepper or database credentials are exposed, follow [`key-rotation.md`](file:///home/prox/carbon-panel/cloud/docs/runbooks/key-rotation.md) and cycle `CARBONCLOUD_NODE_JOIN_TOKEN_SECRET` and `CARBONCLOUD_DATABASE_URL`.

---

## 3. Forensic Investigation

Every mutating action and authentication event is immutably logged with actor, org, client IP, and request trace identifier:

```sh
# Fetch audit events for the affected tenant organization
curl -sS -H "Authorization: Bearer $ADMIN_TOKEN" \
  "http://localhost:8080/carbon.cloud.v1.AuditService/ListAuditEvents" \
  -d '{"org_id": "<org-id>", "limit": 100}'

# Search control-plane structured logs by trace ID
docker compose -f cloud/deploy/compose/docker-compose.yml logs controld | grep '"trace_id":"<trace-id>"'
```

---

## 4. Rollback & Post-Incident Review

1. **Verify Integrity**: Run integration test suite (`make -C cloud cloud-test`) and inspect `/readyz` endpoints across all instances.
2. **Post-Mortem**: Document root causes, timeline, blast radius, and preventative action items within 48 hours.
