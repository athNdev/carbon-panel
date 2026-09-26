# Billing Reconciliation & Quota Recovery Runbook

Procedures for diagnosing subscription status drift, repairing quota enforcement mismatches, and reconciling billing webhook events.

---

## 1. Problem Overview

Tenant resource creation (nodes, workloads, memory allocations) is strictly bound by commercial tier quotas (`free`, `pro`, `team`). 
Discrepancies can occur if:
1. Inbound payment gateway webhooks (e.g. Stripe `customer.subscription.updated` or `invoice.payment_succeeded`) failed to deliver or timed out.
2. In-flight provisioning jobs completed after an out-of-band downgrade.
3. Database transactional rollbacks resulted in stale cached capacity counters.

---

## 2. Quota Drift Diagnosis

When a tenant reports unexpected `ErrQuotaExceeded` or inability to place workloads despite having sufficient plan allowances:

```sh
# 1. Inspect current tenant subscription record in the control-plane database
# (Verify plan tier, current status, and validity period)
docker exec -it $(docker ps -q -f name=postgres) psql -U carbon -d carboncloud -c \
  "SELECT id, org_id, plan_id, status, current_period_end FROM subscriptions WHERE org_id = '<target-org-id>';"

# 2. Inspect active resource usage for the tenant
docker exec -it $(docker ps -q -f name=postgres) psql -U carbon -d carboncloud -c \
  "SELECT COUNT(*) AS active_nodes FROM nodes WHERE org_id = '<target-org-id>' AND status != 'deleted';
   SELECT COUNT(*) AS active_workloads, SUM(memory_mb) AS allocated_ram FROM workloads WHERE org_id = '<target-org-id>' AND status = 'running';"
```

---

## 3. Stripe Webhook Replay & Manual Reconciliation

If a subscription status is out of sync with Stripe:

1. **Check Webhook Delivery Status**:
   Log in to Stripe Dashboard $\to$ Developers $\to$ Webhooks $\to$ Select Carbon Cloud Endpoint.
   Filter for `invoice.payment_failed`, `customer.subscription.updated`, or `customer.subscription.deleted`.

2. **Replay Missing Webhook**:
   Click **Resend** on the failed webhook event.
   Confirm controld logs receive and process the webhook signature:
   ```sh
   docker compose -f cloud/deploy/compose/docker-compose.yml logs controld | grep "webhook.stripe"
   ```

3. **Emergency Manual Plan Resync**:
   If the payment gateway is unreachable and service restoration is urgent:
   ```sql
   UPDATE subscriptions 
   SET status = 'active', plan_id = 'pro', updated_at = NOW() 
   WHERE org_id = '<target-org-id>';
   ```
   *Note*: Ensure this manual change is recorded in the audit trail with the operator's admin principal.

---

## 4. Post-Reconciliation Verification

Verify that the tenant can allocate workloads again without hitting capacity boundaries:
```sh
cloudctl workload create --name "validation-ping" --node "<node-id>" --memory 512
cloudctl workload delete "<workload-id>"
```
