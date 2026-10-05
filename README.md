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

## Makefile Targets

Common development tasks are managed via `make`:

- `make build`: Build the project binaries.
- `make test`: Run unit tests with race detection.
- `make test-integration`: Run integration tests.
- `make lint`: Run golangci-lint (automatically installed via `make tools`).
- `make fmt`: Format source code using `go fmt` and `gofumpt`.
- `make generate`: Run code generation.
- `make up`: Start local infrastructure via docker-compose (arrives with #4).
- `make down`: Stop local infrastructure.
- `make tools`: Install necessary development tools (e.g., golangci-lint).

## Code Generation

- **Protobuf**: Protobuf generation via `buf` arrives with #49.
- **Mocks**: Mock generation gets added alongside the interfaces (#10, #40).

## CI

The CI pipeline runs on GitHub Actions. The following required status checks must pass for a PR to be merged:
- `Lint and Unit Tests` (`lint-and-unit` job)
- `Integration Tests` (`integration` job)

Branch protection rules are configured by the repository owner to enforce these checks.
