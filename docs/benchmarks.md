# Gargi CKG Performance Baselines & Load Testing

This document establishes performance baselines and documents benchmarks for the Gargi Reactive Graph Engine, covering the reactive scheduler, state store adapters, and load testing scenarios.

---

## 1. Environment & Hardware

- **Operating System:** Linux 5.15.167.4-microsoft-standard-WSL2 (Ubuntu 24.04 LTS)
- **CPU:** AMD Ryzen 7 4800H with Radeon Graphics (16 vCPUs)
- **Go Version:** Go 1.23.2 linux/amd64

---

## 2. Go Microbenchmarks

Run via:
```bash
go test -bench=. -benchmem -run=^$ ./internal/reactive/... ./internal/store/...
```

### 2.1 Reactive Scheduler Benchmarks

The scheduler evaluates execution readiness and dependency graphs under both wide fan-out and deep serial pipeline topologies:

| Benchmark | Iterations | Latency (`ns/op`) | Memory (`B/op`) | Allocs (`allocs/op`) |
|:---|---:|---:|---:|---:|
| `BenchmarkScheduler_EvaluateStep_WideFanout/nodes_10` | 57,997 | 20,068 ns (20 µs) | 14,144 B | 182 |
| `BenchmarkScheduler_EvaluateStep_WideFanout/nodes_100` | 1,670 | 714,645 ns (0.71 ms) | 605,781 B | 1,634 |
| `BenchmarkScheduler_EvaluateStep_WideFanout/nodes_500` | 68 | 16,904,519 ns (16.9 ms) | 18,144,503 B | 8,045 |
| `BenchmarkScheduler_EvaluateStep_DeepPipeline/depth_10` | 263,206 | 4,540 ns (4.5 µs) | 2,664 B | 57 |
| `BenchmarkScheduler_EvaluateStep_DeepPipeline/depth_50` | 62,145 | 19,309 ns (19.3 µs) | 8,696 B | 179 |
| `BenchmarkScheduler_EvaluateStep_DeepPipeline/depth_200` | 16,392 | 74,077 ns (74 µs) | 31,736 B | 631 |
| `BenchmarkGraphState_ApplyMutation` | 787,784 | 1,287 ns (1.2 µs) | 1,128 B | 9 |

**Key Takeaways:**
- Single immutable graph state mutations execute in **1.28 µs** with minimal allocations (9 allocs).
- Deep serial pipelines scale linearly: 200 serialized nodes are evaluated in **74 µs** (~370 ns per node).
- Wide fan-out across 500 parallel nodes is evaluated in under **17 ms**.

---

### 2.2 State Store Adapter Benchmarks

State store adapters benchmarked for atomic `SetState`, `GetState`, and optimistic concurrency CAS `PatchState`:

| Adapter | Operation | Iterations | Latency (`ns/op`) | Memory (`B/op`) | Allocs (`allocs/op`) |
|:---|:---|---:|---:|---:|---:|
| **In-Memory** | `SetState` | 70,200 | 15,901 ns (15.9 µs) | 4,280 B | 78 |
| **In-Memory** | `GetState` | 82,784 | 13,405 ns (13.4 µs) | 3,664 B | 71 |
| **In-Memory** | `PatchState` (CAS) | 24,886 | 48,277 ns (48.2 µs) | 13,813 B | 249 |
| **Redis** | `SetState` | 1,764 | 657,347 ns (0.65 ms) | 202,453 B | 817 |
| **Redis** | `GetState` | 2,643 | 463,124 ns (0.46 ms) | 2,260 B | 54 |
| **Redis** | `PatchState` (CAS) | 612 | 1,902,520 ns (1.90 ms) | 413,104 B | 1,810 |

**Key Takeaways:**
- In-memory store operations operate in the low tens of microseconds (**13–48 µs**).
- Redis store operations complete in **0.46–1.9 ms** including serialization and network round-trip emulation.

---

## 3. Load Testing Scenarios

### 3.1 HTTP/REST Load Test (`k6`)

Location: `benchmarks/k6/load_test.js`

Features:
- Stages: Ramp-up from 0 to 50 concurrent VUs, sustain, spike to 100 VUs, ramp-down.
- Validates `/healthz`, `/readyz`, `/metrics`, and `/v1/executions` POST with idempotency keys.
- Defined SLA thresholds:
  - `http_req_duration`: p(95) < 100ms
  - `http_req_failed`: rate < 0.01 (99% success rate)

Run command:
```bash
k6 run benchmarks/k6/load_test.js
```

### 3.2 gRPC Load Test (`ghz`)

Location: `benchmarks/ghz/grpc_load_test.sh`

Features:
- 10,000 total requests across 50 concurrent workers against `gargi.v1.GargiService`.
- Tests query throughput and connection multiplexing under high load.

Run command:
```bash
bash benchmarks/ghz/grpc_load_test.sh
```
