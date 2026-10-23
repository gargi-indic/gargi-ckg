# Gargi Flow Performance Baselines & Load Testing

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

---

## 4. Simulation Benchmarks

Recorded 2026-10-23 on:

- **Operating System:** Linux 6.18.54.1-microsoft-standard-WSL2
- **CPU:** AMD Ryzen 7 4800H with Radeon Graphics (8 cores / 16 threads, `GOMAXPROCS=16`)
- **Go Version:** go1.26.0 linux/amd64

Run via:
```bash
go test -run '^$' -bench 'BenchmarkLoan' -benchmem -count=3 ./internal/sim/experiment/...
```

Both benchmarks live in `internal/sim/experiment/golden_test.go` and use the
loan example (`examples/simulation/loan`) with the `recession-v1` policy in
the `recession` world:

- `BenchmarkLoanEpisode`: one `kernel.RunEpisode` with a Monte Carlo sampler
  (a fresh seed per iteration).
- `BenchmarkLoanExperiment`: `experiment.Run` with `evaluator: monte_carlo`,
  5,000 runs, 1 worker; `ns/episode` is the time per op divided by 5,000.

| Benchmark | Iterations | Latency (`ns/op`) | `ns/episode` | Memory (`B/op`) | Allocs (`allocs/op`) |
|:---|---:|---:|---:|---:|---:|
| `BenchmarkLoanEpisode-16` | 3,511 | 303,118 ns (0.30 ms) | - | 152,389 B | 1,707 |
| `BenchmarkLoanEpisode-16` | 4,027 | 309,696 ns (0.31 ms) | - | 152,442 B | 1,707 |
| `BenchmarkLoanEpisode-16` | 4,020 | 309,063 ns (0.31 ms) | - | 152,430 B | 1,707 |
| `BenchmarkLoanExperiment-16` | 1 | 1,588,639,957 ns (1.59 s) | 317,728 | 765,190,808 B | 8,563,061 |
| `BenchmarkLoanExperiment-16` | 1 | 1,551,504,229 ns (1.55 s) | 310,301 | 765,168,040 B | 8,562,875 |
| `BenchmarkLoanExperiment-16` | 1 | 1,553,312,596 ns (1.55 s) | 310,663 | 765,166,488 B | 8,562,866 |

### 4.1 Full loan experiment (P0 exit criterion)

`examples/simulation/loan/experiment.yaml` evaluates all 42 policies in 2
worlds with `evaluator: both` and 200,000 Monte Carlo runs per policy and
world (16.8 million episodes) plus the exact evaluator:

```bash
time go run ./cmd/gargi sim run examples/simulation/loan/experiment.yaml --local
```

| Workers | Wall time | User CPU | Sys CPU |
|---:|---:|---:|---:|
| 16 (`GOMAXPROCS`) | 13m21.7s | 152m6.0s | 16m42.5s |

(includes the `go run` compile; the exact evaluator alone takes under a
second, see the [simulation quickstart](simulation/quickstart.md)).

**Key Takeaways:**
- One loan episode (graph execution on the virtual clock, CEL expressions,
  behaviors, sampling) costs about **0.31 ms** and 1,707 allocations on one
  core; the experiment runner adds no measurable overhead per episode.
- The full 16.8 million episode run sustains about 21,000 episodes/s on 16
  threads; allocation volume (about 150 KB per episode) is the main cost to
  attack before larger P1 experiments.
- `TestLoanGolden` (exact values of all 84 policy/world pairs plus a 200k run
  Monte Carlo check of the top policy per world) takes about 40 s without the
  race detector.
