# gargi-ckg — Pluggable Reactive Graph Engine

A modular Go middleware framework for asynchronous, stateful, event-driven DAG execution,
orchestrated by Temporal, with pluggable state stores (in-memory, Redis, PostgreSQL, Neo4j)
and gRPC / REST / SSE / WebSocket client APIs.

- Requirements: [docs/SPEC.md](docs/SPEC.md)
- Backlog: GitHub issues labelled `epic` and their sub-issues
- Orchestration log: the `Orchestrator Logs` issue

## Directory Layout & Dependency Rules

The repository is structured to separate public contracts from internal implementation details:

- **`pkg/engine/`**: Holds public contracts and interfaces (e.g., `NodeInterface`, `StateStore`, `GraphDefinition`). This package acts as the API surface for the engine.
- **`cmd/server/`**: Entry point for the server application. Wires dependencies and configuration.
- **`cmd/worker/`**: Entry point for the Temporal worker application. Wires dependencies and configuration.
- **`internal/`**: Contains private application and library code.
  - **`internal/graph/`**: Handles graph parsing, validation, and structure.
  - **`internal/reactive/`**: Handles state transitions, events, and reactivity.
  - **`internal/orchestrator/`**: Temporal integration and workflow orchestration.
  - **`internal/store/`**: State persistence adapters (e.g., in-memory, Redis, PostgreSQL).
  - **`internal/api/`**: External communication interfaces (gRPC, REST, SSE).

### Dependency Rules

To maintain a clean architecture, the following dependency rules must be strictly followed:
1. `pkg/engine` holds **only** public contracts and must not depend on `internal/*`.
2. Packages in `internal/*` may depend on `pkg/engine`.
3. Domain packages (`internal/graph` and `internal/reactive`) **must never import** `internal/api`, `internal/orchestrator`, or `internal/store`.
4. Only entry points in `cmd/*` are permitted to wire these components together.
