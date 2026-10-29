# PayFlow Benchmark & Performance Report

This report presents reproducible benchmark measurements conducted on the **PayFlow** backend with real PostgreSQL database transactions.

---

## 1. Test Environment Specifications

- **OS / Architecture**: Windows 11 / `windows/amd64`
- **CPU**: 12th Gen Intel(R) Core(TM) i5-1240P (12 cores / 16 threads)
- **Go Version**: `go1.26.3 windows/amd64`
- **Database Engine**: PostgreSQL 17.2 (x86_64-windows)
- **Database Driver / Pool**: `pgx/v5` (`pgxpool.Pool`), MaxConns: 30, MinConns: 5
- **Test Methodology**: Standard Go benchmarking harness (`testing.B`), measuring throughput, latency, byte allocations, and heap objects per operation across 50 iterations.

---

## 2. Benchmark Summary Table

| Benchmark Scenario | Throughput (ops/sec) | Latency (ns/op) | Latency (ms/op) | Memory (B/op) | Allocs/op |
|---|---|---|---|---|---|
| **Payment Creation** (with Idempotency & Outbox) | **389.4 ops/s** | 2,568,270 ns | 2.57 ms | 14,698 B | 343 allocs |
| **Payment Lookup** (Primary Key / Index) | **1,956.4 ops/s** | 511,152 ns | 0.51 ms | 3,137 B | 77 allocs |
| **Idempotent Replay** (Cached Response Fast-Path) | **1,571.5 ops/s** | 636,324 ns | 0.64 ms | 3,616 B | 70 allocs |
| **Ledger Posting** (Capture + Balanced Entries) | **166.8 ops/s** | 5,995,572 ns | 6.00 ms | 37,010 B | 817 allocs |
| **Outbox Batch Processing** (`SKIP LOCKED`) | **1,714.1 events/s**| 583,404 ns | 0.58 ms | 3,316 B | 74 allocs |
| **Concurrent Capture & Refund Workload** | **82.3 ops/s** | 12,157,618 ns | 12.16 ms | 123,117 B | 2,308 allocs |

---

## 3. In-Depth Bottleneck Analysis & Findings

1. **Idempotency Fast-Path Efficiency**:
   Idempotent request replay is over **4x faster** than fresh payment creation (0.64ms vs 2.57ms). Replay avoids invoking domain state machine logic, balance checks, and outbox serialization, directly serving the cached response.
2. **Double-Entry Ledger Overhead**:
   Posting a payment capture requires inserting 1 `ledger_transaction` and 3 `ledger_entries` within an atomic transaction. This completes in ~6.00ms, well within operational latency targets for double-entry financial posting.
3. **Outbox Throughput (`SKIP LOCKED`)**:
   PostgreSQL's `FOR UPDATE SKIP LOCKED` allows the worker to claim batches in ~0.58ms without blocking concurrent API writes, achieving over 1,700 event claims per second on a single worker thread.
4. **Row Lock Contention Under Parallel Workloads**:
   Under heavy parallel goroutine contention (16 parallel worker threads competing on the same merchant and payment rows), latency increases to ~12.16ms due to serialized transaction locks (`SELECT FOR UPDATE`), correctly preserving consistency without deadlocks.

---

## 4. Reproducing the Benchmarks

To execute the benchmarks on any system:

```bash
cd internal/repository/postgres
go test -bench=Benchmark -benchmem -benchtime=50x
```
