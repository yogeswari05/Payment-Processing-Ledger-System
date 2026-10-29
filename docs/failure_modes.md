# PayFlow Failure Modes & Recovery Strategies

This document provides a fault analysis of the **PayFlow** system across distributed failures, crashes, database network partitions, and concurrent races.

---

## 1. Crash During Payment Capture

### Scenario
An API worker receives a `POST /v1/payments/{id}/capture` request. It updates the payment status to `captured`, generates the double-entry ledger entries, and inserts the outbox events. The process crashes or network disconnects before HTTP response transmission.

### Guarantees & Recovery
1. **Case A (Crash before database commit)**:
   The database transaction rolls back completely. The payment remains in `authorized` status, no ledger entries exist, no outbox events exist, and the idempotency record is wiped. When the client retries with the same `Idempotency-Key`, the capture executes cleanly.
2. **Case B (Crash after database commit, before HTTP 200 delivered)**:
   The database committed atomically. The idempotency record contains `status = 'completed'` with the HTTP 200 payload. When the client retries with the same `Idempotency-Key`, the API server intercepts the key and immediately replays the original 200 OK response without executing business logic or double-posting to the ledger.

---

## 2. Worker Crash During Outbox Processing

### Scenario
The background outbox worker claims a batch of 20 pending events using `SELECT ... FOR UPDATE SKIP LOCKED` and sets their status to `PROCESSING` with `locked_at = NOW()`. The worker process crashes mid-batch (e.g. OOM, SIGKILL, server reboot).

### Guarantees & Recovery
- Events are leased with a lock timeout (default: 60 seconds).
- The query specifically reclaims expired leases:
  ```sql
  WHERE status = 'PENDING'
     OR (status = 'PROCESSING' AND locked_at < NOW() - INTERVAL '60 seconds')
  ```
- Surviving or newly booted workers automatically reclaim the abandoned events after 60 seconds.
- Downstream consumers remain protected from duplicate processing through the `consumed_events` table (idempotent consumer deduplication).

---

## 3. Poison Pill / Repeated Delivery Failures (Dead Letter Queue)

### Scenario
A downstream consumer consistently rejects an event due to a malformed payload or permanent failure.

### Guarantees & Recovery
1. **Exponential Backoff**:
   Retry delays grow exponentially:
   $$\text{delay} = \min(2^{\text{attempts}} \text{ seconds}, 300 \text{ seconds})$$
2. **Bounded Attempts**:
   Each event has `max_attempts = 5`.
3. **Dead Letter Queue (DLQ)**:
   When `attempts >= max_attempts`, the status transitions to `DEAD_LETTER`.
   The worker ceases retrying the event, preventing queue blockage.
4. **Inspection & Alerting**:
   Dead-letter events are tracked via the Prometheus metric `payflow_outbox_events_total{status="DEAD_LETTER"}` and can be inspected via database queries.

---

## 4. Concurrent Competing Refunds

### Scenario
Two simultaneous requests attempt to refund $60 each on a $100 payment.

### Guarantees & Recovery
1. Both requests invoke `paymentRepo.GetByIDForUpdate(ctx, tx, merchantID, paymentID)`.
2. PostgreSQL row-level lock ensures Request 1 proceeds while Request 2 blocks.
3. Request 1 checks $60 \le 100$, succeeds, updates `refunded_amount = 60`, posts a $60 ledger refund, and commits.
4. Request 2 unblocks, reads the freshly updated row: `captured_amount = 100`, `refunded_amount = 60`, `available = 40`.
5. Request 2 checks $60 \le 40$, fails with `domain.ErrExceedsRefundableAmount`, rolls back, and returns `422 Unprocessable Entity`.
6. Under no circumstance can cumulative refunds exceed captured funds.

---

## 5. Independent Reconciliation Inconsistency Detection

### Scenario
An operator or faulty script attempts a direct database manual update that introduces an inconsistency.

### Invariant Audit Checks
PayFlow's `ReconciliationService` independently checks 7 critical vectors:
1. `UNBALANCED_LEDGER_TRANSACTION`: Identifies any transaction where debits $\ne$ credits.
2. `MISSING_CAPTURE_LEDGER_POSTING`: Detects captured payments without capture ledger postings.
3. `DUPLICATE_CAPTURE_POSTING`: Detects duplicate ledger postings for the same business reference.
4. `MISSING_REFUND_LEDGER_POSTING`: Detects successful refunds without refund ledger postings.
5. `REFUND_EXCEEDS_CAPTURE`: Detects payments where recorded refund totals exceed captured totals.
6. `PAYMENT_STATUS_AMOUNT_MISMATCH`: Detects state inconsistencies (e.g. captured payment with zero captured amount).
7. `ORPHANED_LEDGER_TRANSACTION`: Detects ledger transactions referencing deleted or non-existent payments.

Findings are persisted to `reconciliation_findings` without destructive automatic repair, ensuring audit transparency.
