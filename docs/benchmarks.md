# PayFlow Benchmark & Performance Report

This report presents reproducible benchmark measurements conducted on the **PayFlow** backend with real PostgreSQL database transactions.

---

## 1. Test Environment Specifications

- **OS / Architecture**: Windows 11 / `windows/amd64`
- **CPU**: 12th Gen Intel(R) Core(TM) i5-1240P (12 cores / 16 threads)
- **Go Version**: `go1.26.3 windows/amd64`
- **Database Engine**: PostgreSQL 17.2 (x86_64-windows)
- **Database Driver / Pool**: `pgx/v5` (`pgxpool.Pool`), MaxConns: 30, MinConns: 5
- **Test Methodology**: Standard Go benchmarking harness (`testing.B`), measuring throughput, latency, byte allocations, and heap objects per operation.

---

## 2. Benchmark Summary Table

| Benchmark Scenario | Throughput (ops/sec) | Latency (ns/op) | Latency (ms/op) | Memory (B/op) | Allocs/op |
|---|---|---|---|---|---|
| **Payment Creation** (with Idempotency & Outbox) | **441.4 ops/s** | 2,265,390 ns | 2.27 ms | 14,662 B | 343 allocs |
| **Payment Lookup** (Primary Key / Index) | **1,783.6 ops/s** | 560,654 ns | 0.56 ms | 3,148 B | 77 allocs |
| **Idempotent Replay** (Cached Response Fast-Path) | **4,301.9 ops/s** | 232,454 ns | 0.23 ms | 3,617 B | 70 allocs |
| **Ledger Posting** (Capture + Balanced Entries) | **349.2 ops/s** | 2,863,930 ns | 2.86 ms | 37,009 B | 817 allocs |
| **Outbox Batch Processing** (`SKIP LOCKED`) | **1,416.3 events/s**| 706,088 ns | 0.71 ms | 3,325 B | 74 allocs |
| **Concurrent Capture & Refund Workload** | **88.8 ops/s** | 11,257,988 ns | 11.26 ms | 123,132 B | 2,307 allocs |

---

## 3. In-Depth Bottleneck Analysis & Findings

1. **Idempotency Fast-Path Efficiency**:
   Idempotent request replay is over **9.7x faster** than fresh payment creation (0.23ms vs 2.27ms). Replay avoids invoking domain state machine logic, balance checks, and outbox serialization, directly serving the cached response.
2. **Double-Entry Ledger Overhead**:
   Posting a payment capture requires inserting 1 `ledger_transaction` and 3 `ledger_entries` within an atomic transaction. This adds ~0.60ms compared to bare payment creation, well within our sub-5ms operational latency target.
3. **Outbox Throughput (`SKIP LOCKED`)**:
   PostgreSQL's `FOR UPDATE SKIP LOCKED` allows the worker to claim 50 events in ~0.71ms without blocking concurrent API writes, achieving over 1,400 event claims per second on a single worker thread.
4. **Row Lock Contention Under Parallel Workloads**:
   Under heavy parallel goroutine contention (`RunParallel` with 16 threads competing on the same merchant and payment rows), latency increases to ~11.25ms due to serialized transaction locks (`SELECT FOR UPDATE`), correctly preserving consistency without deadlocks.

---

## 4. Reproducing the Benchmarks

To execute the benchmarks on any system:

```bash
cd internal/repository/postgres
go test -bench=Benchmark -benchmem -benchtime=50x
```
