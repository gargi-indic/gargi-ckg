# Edge Condition Expression Language

## Status
Accepted

## Date
2026-10-16

## Context
The Pluggable Reactive Graph Engine Middleware needs to support expressions for edge preconditions and routing (e.g. `upstream.output.score > 0.8`). These expressions evaluate the data and state of previous node executions to determine if downstream nodes should be executed.
We need to choose an expression language that allows graph creators to define these conditions dynamically. The expressions must be:
- Type-checked at validation time (when the graph is defined, not at runtime).
- Deterministic and side-effect free, so they can be safely evaluated inside Temporal workflow code without breaking Temporal's strict determinism requirements.
- Expressive enough to allow checking variables like `upstream.output`, `nodes.<node_id>.status`, and `state`.

## Decision
We choose the **Common Expression Language (CEL)** via the `github.com/google/cel-go` library.

We will implement:
- **Compile-time parsing and static type-checking:** This will occur during graph structural validation (`internal/graph.ValidateStructure`), and we will require every edge condition to have an output type of `bool`.
- **Declared type environment:** The CEL environment will have `upstream`, `nodes`, and `state` declared as `map(string, dyn)`. In addition, each valid node ID in the graph will be declared as `map(string, dyn)` so expressions like `upstream.output.score > 0.8` and `<node_id>.status == 'reserved'` both type-check and compile correctly.
- **Deterministic, side-effect-free evaluation:** The CEL program will be restricted to pure standard library operators (no I/O, no clock, no random functions), and bounded execution cost will be enforced via `cel.CostLimit` (set to a default of 1000) to ensure safety inside Temporal workflows.

## Consequences
- **Positive:** CEL is designed explicitly for fast, safe, and deterministic execution, perfectly aligning with Temporal's requirements.
- **Positive:** CEL supports static type-checking, preventing runtime errors in graph execution by catching them at definition parsing time.
- **Positive:** The cost limit ensures that complex or malicious expressions cannot consume excessive CPU resources and block the orchestration loop.
- **Negative:** We introduce a new third-party dependency (`cel-go`) outside of the core standard library, although it is widely used and maintained by Google.

## Alternatives
- **Hand-written expression grammar:** Writing a custom lexer, parser, and evaluator for a simple expression syntax. This was rejected because it is error-prone, requires significant maintenance, and reinventing the wheel is unnecessary given mature solutions like CEL.
- **JSONPath predicates:** While good for selecting data, JSONPath lacks rich arithmetic and logical operators out of the box and is not a full expression language.
- **General-purpose embedded scripting languages (Lua/Starlark):** These are too powerful, heavier, harder to strictly sandbox for Temporal's deterministic guarantees, and typically lack the same level of static type-checking guarantees provided by CEL.
