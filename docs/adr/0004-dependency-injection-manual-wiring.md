# Dependency Injection & Manual Wiring

## Status
Accepted

## Date
2026-10-05

## Context
As a modular middleware framework (`gargi-flow`), the application is composed of various loosely coupled components: domain logic, state storage adapters, orchestration integrations, and external APIs. These components are strictly isolated via interfaces (ADR-002). We need a mechanism to instantiate these components and wire them together securely and transparently.

## Decision
We will use manual constructor wiring (e.g., `NewService(store, orchestrator)`) in the composition roots located exclusively in `cmd/*` (e.g., `cmd/server/`, `cmd/worker/`).

- **No DI Frameworks:** We will not use automated dependency injection frameworks like `wire`, `fx`, or `dig`.
- **No Global State:** We will not use package-level globals for configuration or dependencies.
- **Adapter Selection:** The selection of specific implementations (adapters like `store.driver` memory, redis, postgres, or neo4j) will occur only within `cmd/*`.

## Consequences
- **Positive:** Manual wiring is highly transparent. It is trivial to trace the initialization sequence and understand exactly how the application is assembled by reading the `main.go` and associated setup files in `cmd/*`.
- **Positive:** No magic or reflection-based errors at runtime that often plague automated DI frameworks. Compile-time safety is guaranteed.
- **Positive:** Testing is simplified, as unit tests can easily instantiate components with mock dependencies via standard constructors without DI framework overhead.
- **Positive:** Enforces strict adherence to ADR-002, as `cmd/*` is uniquely positioned to import both the interfaces and the concrete implementations without causing circular dependencies.
- **Negative:** As the application grows, the `cmd/*` initialization code can become lengthy and verbose, requiring careful organization.

## Alternatives
- **Google Wire:** Code generation for DI provides compile-time safety and reduces boilerplate. However, it adds a dependency on code generation tooling, increases the learning curve for new developers, and can still obfuscate the raw initialization logic.
- **Uber Fx or Dig:** Reflection-based runtime DI frameworks provide highly modular setups but introduce the risk of runtime panics if dependencies are misconfigured, making debugging significantly harder.
- **Service Locator / Globals:** Using singletons or a global registry is an anti-pattern that hides dependencies, breaks parallel unit testing, and violates our architectural rule against global state.