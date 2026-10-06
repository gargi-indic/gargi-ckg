# Package Boundaries & Decoupling Rules

## Status
Accepted

## Date
2026-10-05

## Context
The `gargi-ckg` project is a modular middleware framework. According to the specification, AI agents implementing this architecture must strictly decouple execution logic, state persistence, orchestration, and communication interfaces. We need to define clear directory structures and strict dependency rules to prevent circular dependencies, ensure clean architecture, and enforce that plugins (storage adapters, orchestrators, transports) are properly isolated from the core domain logic.

## Decision
We establish the following directory layout and dependency rules, which will be strictly enforced programmatically via `depguard` in our `.golangci.yml`.

### Directory Layout
- `pkg/engine/`: Holds public contracts and interfaces (e.g., `NodeInterface`, `StateStore`, `GraphDefinition`). This is the API surface for the engine.
- `cmd/server/`: Entry point for the server application. Wires dependencies and configuration.
- `cmd/worker/`: Entry point for the Temporal worker application. Wires dependencies and configuration.
- `internal/`: Contains private application and library code.
  - `internal/graph/`: Handles graph parsing, validation, and structure.
  - `internal/reactive/`: Handles state transitions, events, and reactivity.
  - `internal/nodes/`: Reference node implementations built on pkg/engine contracts.
  - `internal/orchestrator/`: Temporal integration and workflow orchestration.
  - `internal/store/`: State persistence adapters (e.g., in-memory, Redis, PostgreSQL).
  - `internal/api/`: External communication interfaces (gRPC, REST, SSE).

### Dependency Rules
1. `pkg/engine` holds **only** public contracts and must not depend on `internal/*`.
2. Packages in `internal/*` may depend on `pkg/engine`.
3. Domain packages (`internal/graph` and `internal/reactive`) **must never import** `internal/api`, `internal/orchestrator`, or `internal/store`.
4. Only entry points in `cmd/*` are permitted to wire these components together.

## Consequences
- **Positive:** Domain logic (`internal/graph` and `internal/reactive`) is completely isolated from infrastructure concerns (storage, orchestration, API).
- **Positive:** Interfaces are centralized in `pkg/engine/`, making it easy for external consumers and internal components to rely on stable contracts.
- **Positive:** Using `depguard` guarantees these architectural rules are validated continuously in CI, preventing drift over time.
- **Negative:** Enforcing these strict boundaries requires careful planning of interfaces. Data must be passed using interfaces or primitive models rather than sharing implementation-specific types between layers.

## Alternatives
- **A flat internal structure (`internal/...` without strict sub-rules):** This would lead to a "big ball of mud" architecture over time as boundaries blur between orchestration, domain, and storage layers, making the engine impossible to plug into different backends as requested in the SPEC.
- **Placing contracts inside internal packages:** Doing this would make it impossible to expose public contracts cleanly to external users who need to build custom `NodeInterface` implementations, as they cannot import `internal`.