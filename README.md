# gargi-flow

> **Reactive and durable graph execution.**

A modular Go middleware framework for asynchronous, stateful, event-driven DAG execution,
orchestrated by Temporal, with pluggable state stores (in-memory, Redis, PostgreSQL, Neo4j)
and gRPC / REST / SSE / WebSocket client APIs.

- Requirements: [docs/SPEC.md](docs/SPEC.md)
- Architecture Decisions: [docs/adr/README.md](docs/adr/README.md)
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
  - **`internal/nodes/`**: Reference node implementations built on pkg/engine contracts.
  - **`internal/orchestrator/`**: Temporal integration and workflow orchestration.
  - **`internal/store/`**: State persistence adapters (e.g., in-memory, Redis, PostgreSQL).
  - **`internal/api/`**: External communication interfaces (gRPC, REST, SSE).

### Dependency Rules

To maintain a clean architecture, the following dependency rules must be strictly followed:
1. `pkg/engine` holds **only** public contracts and must not depend on `internal/*`.
2. Packages in `internal/*` may depend on `pkg/engine`.
3. Domain packages (`internal/graph` and `internal/reactive`) **must never import** `internal/api`, `internal/orchestrator`, or `internal/store`.
4. Only entry points in `cmd/*` are permitted to wire these components together.

## Local development stack

A local development stack can be brought up using Docker Compose. This includes Temporal (Server and UI), Redis, Postgres, and Neo4j.

To start the local stack:
```bash
make up
```

To stop the local stack:
```bash
make down
```

### Credentials

Default credentials for local development are stored in `.env.example`. The `make up` command will automatically copy this to `.env` if it doesn't exist. **These credentials are for LOCAL DEV ONLY.**

### Service Ports

| Service | Port Mapping | Purpose |
|---------|--------------|---------|
| Postgres | `127.0.0.1:5432` | Relational storage (app and Temporal) |
| Redis | `127.0.0.1:6379` | Caching and pub/sub |
| Neo4j | `127.0.0.1:7474` | Graph database (HTTP) |
| Neo4j | `127.0.0.1:7687` | Graph database (Bolt) |
| Temporal | `127.0.0.1:7233` | Orchestration server |
| Temporal UI | `127.0.0.1:8233` | Web dashboard |

### Docker-free stack

`scripts/dev-up.sh` runs everything needed for local development as plain
processes, with no Docker:

```bash
./scripts/dev-up.sh
```

| Process | Address |
|---------|---------|
| miniredis (`go run ./scripts/redis_dev.go`) | `127.0.0.1:6379` |
| Temporal dev server (`temporal server start-dev`) | `127.0.0.1:7233`, UI on `http://localhost:8233` |
| API server (`go run ./cmd/server/main.go`) | HTTP `:8080`, gRPC `:9090` |
| Worker (`go run ./cmd/worker/main.go`) | — |
| Web UI (`npm run dev` in `web/`) | `http://localhost:5173` |

Prerequisites: `go`, `node`, `npm`, and the [Temporal CLI](https://docs.temporal.io/cli)
(`brew install temporal` or `curl -sSf https://temporal.download/cli.sh | sh`).
The script sets `GARGI_STORE_DRIVER=redis` so the server and worker share state, and
allows the Vite origin through CORS. Per-service logs go to `.dev-logs/`.
<kbd>Ctrl</kbd>+<kbd>C</kbd> stops every process it started.

Optional knobs: `GARGI_DEV_SKIP_TEMPORAL=1` (use a Temporal you already run),
`GARGI_DEV_SKIP_WEB=1` (backend only), `GARGI_DEV_LOG_DIR=<path>`.

## Web UI / Visual DAG Canvas

`web/` contains a React + Vite app for authoring graphs and watching them execute
live. See [web/README.md](web/README.md) for its architecture.

### Running it

```bash
./scripts/dev-up.sh          # full stack, then open http://localhost:5173
# or, against an API you already run:
make ui-install && make ui-dev
```

The UI calls `http://localhost:8080` by default (override with `VITE_API_BASE_URL`).
The API only answers browser requests from allowed origins, so set
`GARGI_HTTP_CORS_ALLOWED_ORIGINS=http://localhost:5173` when you don't use `dev-up.sh`.

### Loading experiments

- Use the **Load Example…** dropdown in the header to load any graph from
  `web/public/examples/` (linear pipeline, fan-out/fan-in, conditional branch, saga
  compensation, wait-for-signal) into the editor and canvas. Uploading a `.json` or
  `.yaml` file and pasting into the editor work too.
- The **Experiments Gallery** (shown when you open an execution and the editor has no
  graph) offers **Load Code** and **Load & Run** for curated examples.
- Press **Run** to submit the graph. Node rings and status badges update live over
  SSE (falling back to polling), and the **Live Events** panel logs every transition.
  **Recent Executions** in the left sidebar lets you reopen past runs.

### Authoring DAGs

- Drag node types from the **Node Palette** onto the canvas.
- Drag from a node's bottom handle to another node's top handle to connect them.
- Select a node or edge and press <kbd>Backspace</kbd> to delete it.
- Click a node to edit its config in the drawer; click an edge to edit its CEL `condition`.
- Every change is written back to the editor, which runs client-side validation
  (unique IDs, no dangling edges, no cycles). **Export JSON** / **Export YAML** download it.

### Human-in-the-loop signals

1. Load **Wait Signal (YAML)** and press **Run**.
2. When `await_manager_approval` is reached it shows an amber ring and an
   **⏳ Awaiting signal** badge.
3. Click the node. The signal inspector is pre-filled with the node's signal name
   (`manager_approval`) and a JSON payload; edit it and press **Send**. The UI calls
   `POST /v1/executions/{executionId}/nodes/{nodeId}/signal`, and the execution
   continues to `dispatch_order`.

### UI checks

```bash
cd web
npm run lint && npm run build
npx playwright install --with-deps chromium   # once
npm run test:e2e                              # mocked-API smoke tests
```

## gargi CLI

`gargi` is a developer binary that validates simulation models and runs
simulation experiments in-process (no server, store or Temporal needed).

```bash
make build                      # also builds bin/gargi (or: go build -o bin/gargi ./cmd/gargi)
bin/gargi version

# Validate a sim/v1 model: prints OK, or one "path: code: message" line per error (exit 1).
bin/gargi model validate examples/simulation/loan/sim.yaml
bin/gargi model validate --json examples/simulation/loan/sim.yaml   # {"errors":[...]}

# Run an experiment locally (server mode arrives in P1, so --local is required).
bin/gargi sim run examples/simulation/loan/experiment.yaml --local \
  [--workers N] [--top K] [--json] [--out report.json] [--emit-best DIR]
```

For every world, `sim run` prints a table of the top K policies (MEAN, CI95,
P95 DURATION and the EXACT value when the exact evaluator ran) followed by
the best policy. For the loan example it ends with:

```text
Best policy: screen=score; route[score,high]=reject; route[score,low]=approve
Exact EV: 460.0
...
Best policy: screen=score; route[score,high]=reject; route[score,low]=review; final[score,low,fail]=reject; final[score,low,pass]=approve
Exact EV: 202.5
```

The loan experiment uses `evaluator: both` with 200,000 Monte Carlo runs per
policy and world, so it takes a while; a copy with `evaluator: exact` returns
the same best policies in about a second. `--json` prints the runner's
`Report` JSON, `--out` writes it to a file, and `--emit-best DIR` writes each
world's best policy as `DIR/<world>.policy.yaml` (`policy/v1`; exhaustive
search only).

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

## Configuration

The application is configured using a YAML file (by default `config/config.example.yaml`) and can be overridden by environment variables with the `GARGI_` prefix.

Examples of environment overrides:
- `GARGI_STORE_DRIVER=redis`
- `GARGI_STORE_REDIS_ADDR=localhost:6379`
- `GARGI_TEMPORAL_HOST_PORT=remote:7233`
