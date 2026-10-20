# Project Rename: gargi-ckg → gargi-flow

## Status
Accepted

## Date
2026-10-20

## Context
The project started as `gargi-ckg` ("Computational Knowledge Graph"). In practice it has grown into a general-purpose middleware for **reactive and durable graph execution**: Temporal-orchestrated DAGs with conditional branching, sagas, signals, live SSE/WebSocket streaming and pluggable state stores (memory, Redis, PostgreSQL, Neo4j). Knowledge-graph construction is one use case, not the defining one, and the `ckg` acronym was opaque to new users.

A visual DAG canvas (React Flow based UI prototype) is also planned, and a name aligned with "flow" reads naturally for both the engine and the UI.

## Decision
- Project name: **Gargi Flow** (`gargi-flow`).
- Tagline: **"Reactive and durable graph execution."**
- Go module path: `github.com/gargi-indic/gargi-flow` (supersedes the path stated in ADR-0001).
- Proto `go_package`, buf plugin options, OpenTelemetry tracer/service names, JSON Schema `$id` (`urn:gargi-flow:schema:graph:v1`) and docs are updated accordingly.
- The GitHub repository is renamed to `gargi-indic/gargi-flow`; GitHub redirects the old URL.

## Consequences
- External importers must update imports from `github.com/gargi-indic/gargi-ckg/...` to `github.com/gargi-indic/gargi-flow/...`.
- Telemetry dashboards/alerts filtering on `service.name = gargi-ckg` must switch to `gargi-flow`.
- Already-applied PostgreSQL migrations are intentionally left unchanged (comment-only references) to keep migration history immutable.
- Proto package (`gargi.v1`) and wire format are unchanged; gRPC/REST clients are unaffected.
