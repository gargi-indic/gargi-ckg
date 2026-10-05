# Language = Go

## Status
Accepted

## Date
2026-10-05

## Context
We are building a Pluggable Reactive Graph Engine Middleware. The system needs to be a modular framework to support asynchronous, stateful, event-driven DAG executions, strictly decoupling execution logic, state persistence, orchestration, and communication interfaces. It integrates with Temporal as the underlying distributed orchestration driver and supports pluggable state stores like memory, Redis, PostgreSQL, and Neo4j. The system requires exposing standardized contract interfaces (gRPC, REST, SSE, WebSockets) and maintaining a clean architectural separation.

We need to choose a programming language that excels in concurrency, builds robust modular systems, integrates well with Temporal, and provides excellent performance for a middleware framework.

## Decision
We will write the `gargi-ckg` project in Go, specifically using Go 1.23 or newer. The module path will be `github.com/gargi-indic/gargi-ckg`. We will enforce idiomatic Go code (gofmt/goimports clean).

## Consequences
- **Positive:** Go provides native primitives for concurrency, which aligns perfectly with building an asynchronous, event-driven reactive engine.
- **Positive:** Temporal has a mature and well-supported official Go SDK, ensuring smooth integration.
- **Positive:** The strongly typed nature and interface system of Go make it ideal for enforcing pluggability and standardized contract interfaces (like `NodeInterface`, `GraphDefinitionInterface`, and `StateStore`).
- **Positive:** Go's tooling supports standard gRPC/Protobuf generation, REST APIs, and simple CI/CD setups.
- **Negative:** We must be vigilant to keep code idiomatic and avoid global state, which requires discipline especially when wiring dependencies in a decoupled architecture.

## Alternatives
- **TypeScript / Node.js:** While excellent for event-driven asynchronous execution, Node.js does not provide the same level of raw performance or native concurrency primitives (goroutines) as Go for heavy middleware workloads. Go's typing and interface compliance are generally stricter for large-scale framework development.
- **Java:** Java has a strong Temporal SDK and is great for enterprise middleware, but it brings a heavier runtime footprint and slower startup times compared to Go.
- **Rust:** Rust offers exceptional performance and memory safety, but its Temporal SDK is less mature, and the learning curve and compilation times are higher, which could slow down iteration speed.