# PayFlow Engineering Trade-offs & Architecture Decisions

This document outlines key engineering decisions, architectural trade-offs, and design rationales made during the development of the **PayFlow** platform.

---

## 1. Modular Monolith vs. Microservices

### Decision: Modular Monolith
PayFlow is organized as a modular monolith with strict domain boundaries and layered separation:
- `internal/domain`: Pure entities, state machine transitions, monetary validation.
- `internal/repository`: PostgreSQL queries, row locking, and atomic transaction boundaries.
- `internal/service`: Orchestration of business invariants, ledger posting, and outbox emission.
- `internal/api`: HTTP routing, authentication, input validation, and structured error mapping.

### Rationale & Trade-offs
- **Microservices overhead avoided**: Microservices introduce distributed transactions, partial failures, network latency, and eventual consistency dilemmas that undermine double-entry accounting.
- **Atomic Transaction Guarantees**: By co-locating the payment state machine, double-entry ledger, idempotency records, and outbox events in a single PostgreSQL database, business actions and their financial entries are committed atomically within a single local transaction (`ACID`).
- **Simplicity**: Runnable locally on a single machine, with zero complex orchestrator requirements.

---

## 2. Explicit SQL (`pgx/v5`) vs. Heavy ORM (GORM)

### Decision: Explicit SQL with pgx
We chose `pgx/v5` with explicit parameterized queries, connection pooling (`pgxpool`), and direct transaction management instead of an ORM like GORM.

### Rationale & Trade-offs
- **Transaction Transparency**: In payment systems, hiding transaction boundaries behind an ORM leads to accidental auto-commits, N+1 queries, and race conditions.
- **Advanced Concurrency Primitives**: Capabilities like `SELECT ... FOR UPDATE` and `SELECT ... FOR UPDATE SKIP LOCKED` are first-class, explicit, and easy to review.
- **Immutability Enforcement**: Raw SQL schema migrations enforce database-level triggers that abort any `UPDATE` or `DELETE` on posted ledger records.

---

## 3. Integer Minor Units vs. Floating-Point / Decimal

### Decision: Strictly 64-bit Signed Integer Minor Units
Monetary values are represented strictly as integers representing minor currency units (cents in USD/EUR, pence in GBP).

### Rationale & Trade-offs
- **Precision Integrity**: Floating-point numbers (`float64`) are inherently lossy due to IEEE-754 binary fraction representation ($0.1 + 0.2 \ne 0.3$), causing fractional penny drift over millions of ledger entries.
- **Language Portability**: 64-bit integers (`int64`) map directly to PostgreSQL `BIGINT` with deterministic arithmetic and zero rounding ambiguity.
- **Safety Ceiling**: A maximum bound of 1,000,000,000 minor units ($10M) prevents integer overflow scenarios.

---

## 4. Pessimistic Row Locking vs. Optimistic Concurrency Control

### Decision: Pessimistic Row-Level Locking (`SELECT FOR UPDATE`)
All operations mutating payment status, captures, or refunds lock the payment record for the duration of the transaction.

### Rationale & Trade-offs
- **Pessimistic Locking**: When multiple concurrent requests attempt to capture or refund the same payment, serialized row locking guarantees that only the first request proceeds; subsequent requests immediately read the committed state upon unblocking and fail with clear validation errors (`payment_already_captured` or `exceeds_refundable_amount`).
- **Why not Optimistic Locking alone**: Under high contention on a single merchant or payment account, pure optimistic version checks produce high rollback rates, retry storms, and wasted CPU cycles.

---

## 5. Transactional Outbox Pattern vs. Dual Writes

### Decision: Transactional Outbox with `SKIP LOCKED` Background Worker
Events are inserted into an `outbox_events` table inside the exact same database transaction that updates payment state and writes ledger entries. A worker daemon periodically claims batches using `FOR UPDATE SKIP LOCKED`.

### Rationale & Trade-offs
- **Dual Write Hazard Avoided**: In a direct message broker architecture, if the database write succeeds but the broker publish fails, the event is permanently lost. Conversely, if the message publishes but the database rollback occurs, downstream systems process phantom payments.
- **At-Least-Once Delivery**: The outbox worker guarantees at-least-once delivery with exponential backoff and dead-letter queueing.
- **Consumer Deduplication**: Consumers use the `consumed_events` table to achieve effective exactly-once semantics.

---

## 6. Immutable Ledger vs. In-Place Balancing

### Decision: Strict Immutability with Compensating Transactions
Posted ledger entries and transactions cannot be updated or deleted. Reversals and refunds create new, independent double-entry transactions.

### Rationale & Trade-offs
- **Auditing & Compliance**: Erasing or editing financial history is a violation of GAAP/IFRS standards.
- **Tamper Evidence**: Immutability triggers guarantee historical integrity. Any adjustment must be posted as a fresh transaction referencing the original business entity.
