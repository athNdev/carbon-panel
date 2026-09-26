# Disaster Recovery & Database Restoration Runbook

Procedures for restoring Carbon Cloud control-plane state from point-in-time backups, rotating envelope encryption keys, and reconnecting distributed node agents.

---

## 1. RTO & RPO Targets

- **Recovery Point Objective (RPO)**: $\le 15$ minutes (achieved via continuous WAL archiving or periodic snapshot replication).
- **Recovery Time Objective (RTO)**: $\le 60$ minutes from declaration of total control-plane loss to full API availability.

---

## 2. PostgreSQL Point-In-Time Restoration (PITR)

### A. Stop Corrupted / Degraded Control Plane
```sh
docker compose -f cloud/deploy/compose/docker-compose.yml stop controld
```

### B. Restore Database Snapshot
1. Identify the latest valid base backup and WAL archives.
2. Initialize target PostgreSQL data directory:
```sh
# Drop and recreate database from snapshot
docker exec -i $(docker ps -q -f name=postgres) dropdb -U carbon carboncloud --if-exists
docker exec -i $(docker ps -q -f name=postgres) createdb -U carbon carboncloud
gunzip -c /backups/carboncloud-latest.sql.gz | docker exec -i $(docker ps -q -f name=postgres) psql -U carbon -d carboncloud
```

### C. Execute Database Schema Verification
Verify all tables, indexes, and unique constraints are sound:
```sh
docker exec -it $(docker ps -q -f name=postgres) psql -U carbon -d carboncloud -c "\dt"
```

---

## 3. Secret Re-Keying & Envelope Re-Encryption

If disaster recovery involves migration to a new infrastructure host or recovering from compromise:
1. Verify master secrets in environment or vault (`CARBONCLOUD_ENCRYPTION_KEY`, `CARBONCLOUD_NODE_JOIN_TOKEN_SECRET`).
2. Start `controld` with auto-migration enabled:
```sh
docker compose -f cloud/deploy/compose/docker-compose.yml up -d controld
```
3. Confirm health endpoints:
```sh
curl -f http://localhost:8080/healthz
curl -f http://localhost:8080/readyz
```

---

## 4. Node Reconnection & Resynchronization

During a control-plane outage or recovery window:
1. Distributed `cloudnoded` agents continue executing their local container workloads without interruption.
2. Agents back off and retry heartbeat connections to the control plane.
3. Once `controld` is healthy, agents reconnect and send heartbeats with their persistent agent fingerprint.
4. If a node identity was wiped on the agent side, re-issue a join token and execute:
```sh
cloudnoded join --token ccj_<id>.<mac> --control-plane http://<control-plane-url>
```
5. Monitor `carboncloud_rpc_requests_total{procedure="AgentService/Heartbeat"}` to confirm cluster-wide fleet recovery.
