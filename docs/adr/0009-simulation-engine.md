# Simulation Engine Boundaries

## Status
Accepted

## Date
2026-10-22

## Context
We are adding a simulation engine (Epic #124, [docs/simulation/ARCHITECTURE.md](../simulation/ARCHITECTURE.md)) that runs a decision graph across many uncertain worlds in virtual time, searches for the best decision policy, and hands the winning policy to the existing Temporal-backed engine. The core identity is **Outcome = f(World, Policy, Seed)**.

The simulator must run millions of runs locally, with no side effects and no external services. It must also stay faithful to production: the graph we simulate must be the graph we run. Without explicit rules, simulation code would tend to fork `graph/v1`, reach into Temporal or the stores, leak hidden ground truth into policies, or produce results that cannot be reproduced. This ADR fixes those boundaries before any simulation code lands, and extends ADR-0002.

## Decision

### 1. Documents
- `graph/v1` is **unchanged** apart from additive node types (the `decision` node). Simulation is described by **overlay documents** that reference a graph and never copy or fork it:
  - `sim/v1`: worlds, latent variables, behaviors for effectful nodes, resources, arrivals, objective.
  - `policy/v1`: what each decision node chooses, given what it observed.
  - `experiment/v1`: worlds × policies × replications, solver, seed and reports.
- All documents are YAML/JSON validated by JSON Schema (draft 2020-12, `additionalProperties: false`), like `schemas/graph/v1`. Expressions are CEL (ADR-0005), type-checked at validation time.

### 2. Packages
- `pkg/sim/`: public contracts (model, behavior, policy, experiment, run record, report). It depends only on `pkg/engine` and must never import `internal/*`.
- `internal/sim/...`: the simulation domain (model parsing and validation, kernel, behaviors, policies, solvers, optimizers, metrics, calibration). Like `internal/graph` and `internal/reactive`, it is a **domain package**: it may import `pkg/*`, `internal/graph` and `internal/reactive`, and **must never import** `internal/api`, `internal/orchestrator`, `internal/store` or `go.temporal.io`. Data from the stores (e.g. executions for calibration) is reached through interfaces injected by `cmd/*` (ADR-0004).
- Both rules are enforced by `depguard` in `.golangci.yml` (rules `simulation` and `sim_contracts`).

### 3. No Temporal in simulation
The kernel drives `reactive.Scheduler.EvaluateStep` directly with a virtual clock and an event heap. Temporal stays for production execution, and for an optional **fidelity check** that replays a few scenarios through the real workflow in the Temporal test environment before a policy is deployed. Individual simulated runs never become Temporal workflows.

### 4. Latent variables live in `sim/v1`
Hidden ground truth (e.g. "is this applicant actually bad?") is a **latent** variable declared in `sim/v1` and sampled once per run. It is never part of `graph/v1` or node outputs, so production never sees it. In production, latent truths arrive later through outcome ingestion.

### 5. Information sets
A `decision` node declares `observes`, the upstream values its policy may read. Policy CEL expressions are type-checked against an environment that declares **only** those variables, so a policy (or an optimizer) that reads a latent or an unobserved node fails validation instead of silently cheating. The same decision-node code runs in simulation and production.

### 6. Determinism
- RNG: `math/rand/v2` PCG streams only. No `math/rand` global state, no wall clock, no map-iteration-order dependence in `internal/sim`.
- Per-replication seed: `sim.Seed(experimentSeed, world, replication)`, a stable hash in `pkg/sim`.
- **Common random numbers:** replication `r` of world `w` uses the same seed for every policy, so policy comparisons are paired.
- Each consumer draws from its own stream, split from the replication seed by **stream name** (e.g. a node ID, or `latent.<name>`), so adding a node or latent does not perturb other draws.
- The scheduler's no-wall-clock audit (`TestScheduler_NoWallClockAudit`) gets a matching audit over `internal/sim`.

### 7. P0 scope
- The `decision` node is registered in the simulation registry only in P0; production registration follows with policy deployment.
- P0 supports mode A (independent episodes) only; modes B (coupled timeline) and C (horizon) come later.
- P0 policies are `rules` and `table`. `params` and `agent` policies come later.
- P0 evaluators are Monte Carlo and exact enumeration over discrete models, driven by `exhaustive` policy search. The backward-induction `exact` solver is P1.
- P0 core behaviors: `fixed`, `choice`, `bernoulli`, `normal`, `lognormal`, `triangular`, `empirical`.
- The P0 entry point is `gargi sim run --local`: no server, no Docker, no database.
- The P0 exit criterion is a golden test reproducing case 1 (loan underwriting) from the architecture document.

## Consequences
- **Positive:** One graph, one scheduler and one decision node serve both simulation and production, so simulation results transfer to deployed behavior.
- **Positive:** Simulation runs locally at in-process speed, without Temporal, Docker or a database.
- **Positive:** Information-set type-checking makes "the optimizer peeked at hidden truth" a validation error rather than a subtle modelling bug.
- **Positive:** Seeded PCG streams with common random numbers make every run reproducible and policy comparisons paired, needing fewer runs for the same confidence.
- **Positive:** `depguard` keeps the simulation domain free of infrastructure imports in CI.
- **Negative:** Effectful nodes need a `sim/v1` behavior to be simulated, and validation must enforce this.
- **Negative:** Simulation does not exercise Temporal-specific behavior (retries, saga compensation, signals) directly. The optional fidelity check covers that gap.
- **Negative:** Calibration needs store data through injected interfaces, which adds wiring in `cmd/*`.

## Alternatives
- **Embedding simulation config in `graph/v1`:** Rejected. It would mix assumptions that never reach production into the production schema and couple graph versioning (ADR-0006) to modelling changes.
- **Running simulations as Temporal workflows (e.g. with the test environment's time skipping):** Rejected for individual runs. It is orders of magnitude slower, needs a Temporal dependency for local use, and adds no fidelity for pure nodes. Kept only as an optional fidelity check, and for durability of long experiment *jobs* in the hosted edition.
- **Latent variables as hidden graph nodes:** Rejected. Hidden truth would be visible to production and to policies, and the information-set guarantee would rely on convention instead of type checking.
- **One global RNG per run, or `math/rand` v1:** Rejected. Draw order would depend on scheduling, so adding a node would change every other result, and policies could not share random numbers.
