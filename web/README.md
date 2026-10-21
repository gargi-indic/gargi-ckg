# gargi-flow Web UI — Visual DAG Canvas

A single-page React app for authoring gargi-flow graph definitions and watching
them execute live. It talks to the gargi-flow REST/SSE API exposed by
`cmd/server` (default `http://localhost:8080`).

## Stack

| Concern | Library |
|---------|---------|
| UI framework | React 19 |
| Build / dev server | Vite |
| Graph canvas | [`@xyflow/react`](https://reactflow.dev) (React Flow v12) |
| Auto-layout | [`dagre`](https://github.com/dagrejs/dagre) (top-to-bottom ranks) |
| API client | [`openapi-fetch`](https://openapi-ts.dev/openapi-fetch/) typed by `openapi-typescript` |
| Graph parsing | `JSON.parse`, falling back to `js-yaml` |
| E2E smoke tests | Playwright |

## Architecture

```
src/
├── main.tsx                 React entry point
├── App.tsx                  App shell: header, state, parsing, layout, execution lifecycle
├── api/
│   ├── client.ts            openapi-fetch client (base URL from VITE_API_BASE_URL)
│   └── schema.ts            generated types — do not edit by hand
└── components/
    ├── GraphCanvas.tsx      React Flow canvas (drop target, connect, delete)
    ├── CustomNode.tsx       Node card with a coloured status ring + status badge
    ├── NodePalette.tsx      Draggable node types for authoring
    ├── NodeDrawer.tsx       Node inspector: config editor, output/error, signal inspector
    ├── EdgeDrawer.tsx       Edge inspector: edit CEL `condition`
    ├── ExecutionsSidebar.tsx Recent executions (GET /v1/executions, refreshed every 5s)
    └── ExperimentsGallery.tsx Curated example cards ("Load Code" / "Load & Run")
```

### Data flow

1. **Editor text is the source of truth.** The right-hand textarea holds a
   `GraphDefinition` (JSON or YAML). `App.tsx` parses it, validates it on the
   client (unique node IDs, no dangling edges, no cycles), and turns it into
   React Flow nodes/edges.
2. **dagre auto-layout.** Parsed nodes are positioned by dagre
   (`rankdir: TB`) so any loaded or pasted graph renders readably.
3. **Authoring round-trips to text.** Dropping a node from the palette,
   connecting handles, deleting with <kbd>Backspace</kbd>, or editing a node /
   edge in a drawer re-serialises the canvas back into the editor (JSON if the
   editor held JSON, YAML otherwise). **Export JSON / Export YAML** download it.
4. **Live execution.** **Run** generates an execution ID, opens an
   `EventSource` on `GET /v1/executions/{id}/events` *before* submitting
   `POST /v1/executions`, and applies each SSE `nodeId → toStatus` transition
   to the canvas and the *Live Events* log. If the SSE stream errors the app
   falls back to polling `GET /v1/executions/{id}` every second until the
   execution reaches a terminal status. Selecting an execution in the sidebar
   loads its node states the same way.
5. **Status rendering.** Live statuses are overlaid onto the authored nodes at
   render time; `CustomNode` maps them to a ring colour and a badge
   (`RUNNING` pulses blue, `SUCCEEDED` green, `FAILED` red, `SKIPPED` dashed
   grey). Edges out of a succeeded node animate; edges out of a skipped node dim.
6. **Signal inspector (human-in-the-loop).** A `wait_for_signal` node that is
   `RUNNING` gets an amber ring and an *⏳ Awaiting signal* badge. Clicking it
   opens `NodeDrawer`, whose signal form (pre-filled with the node's
   `config.signal` name and a JSON payload) calls
   `POST /v1/executions/{executionId}/nodes/{nodeId}/signal`.

## Development

The easiest way to get a backend is the Docker-free script at the repo root:

```bash
./scripts/dev-up.sh    # miniredis + Temporal dev server + API server + worker + this UI
```

To run only the UI against an API that is already running:

```bash
npm install
npm run dev            # http://localhost:5173
```

| Env var | Default | Purpose |
|---------|---------|---------|
| `VITE_API_BASE_URL` | `http://localhost:8080` | Base URL of the gargi-flow REST/SSE API |

The API must allow the UI's origin via CORS, e.g.
`GARGI_HTTP_CORS_ALLOWED_ORIGINS=http://localhost:5173` (set automatically by
`dev-up.sh`).

### Scripts

| Script | Description |
|--------|-------------|
| `npm run dev` | Vite dev server with HMR |
| `npm run build` | Type-check (`tsc -b`) and production build to `dist/` |
| `npm run lint` | ESLint, zero warnings allowed |
| `npm run preview` | Serve the production build |
| `npm run test:e2e` | Playwright smoke tests (see below) |

### Smoke tests

`e2e/smoke.spec.ts` checks that the canvas loads, that an example experiment
can be selected and rendered, and that node status badges reflect an
execution's state. All API calls are mocked with `page.route`, so no backend is
needed; Playwright starts its own Vite server on port 4173.

Browsers are **not** downloaded by `npm install`, so `lint` / `build` in CI
never need them. Install one once before running the tests:

```bash
npx playwright install --with-deps chromium
npm run test:e2e
```

### Regenerating API types

`src/api/schema.ts` is generated from the OpenAPI spec produced by `make generate`:

```bash
npx openapi-typescript ../gen/openapiv2/gargi/v1/api.swagger.json --output src/api/schema.ts
```

Example graphs served by the dev server live in `public/examples/`.
