# ADR 0008 — Transactional Webhook Outbox and Event Delivery

- Status: accepted
- Date: 2026-09-24

## Context

Tenants require programmatic notifications when infrastructure events occur (node registered, node heartbeat lost, node drained, workload created, workload stopped, provision failed).
Direct synchronous HTTP calls inside RPC handlers introduce severe reliability risks:
1. Increased RPC latency and vulnerability to third-party timeouts.
2. Inability to rollback domain mutations if external HTTP fails, leading to state inconsistencies.
3. Lost events when external customer webhook endpoints experience temporary outages.

## Decision

- **Transactional Outbox Pattern**:
  - Implemented in `cloud/internal/cloud/notify`.
  - Mutation RPC handlers record webhook event records into the `webhook_events` table within the same database transaction that persists the resource mutation (e.g. node join, workload start).
- **Asynchronous Delivery Engine**:
  - Decoupled worker processes scan pending deliveries in batches.
  - Delivery attempts are logged in `webhook_attempts` with response status codes, execution duration, and error traces.
  - Failed attempts are retried with exponential backoff and jitter.
- **Cryptographic Payload Signing**:
  - Payloads are signed using HMAC-SHA256 (`X-Carbon-Signature: sha256=<hex>`) using the tenant's webhook signing secret.
  - Payloads include Unix timestamp headers (`X-Carbon-Timestamp`) to prevent replay attacks.
- **Payload Redaction**:
  - Sensitive credential material (join tokens, private keys, API secrets) is systematically stripped before constructing the webhook JSON payload.

## Consequences

- Zero impact on control-plane RPC latency from slow or hanging tenant HTTP endpoints.
- Guaranteed at-least-once delivery of lifecycle events.
- Tenants can cryptographically verify that inbound webhooks originated from their Carbon Cloud instance.
