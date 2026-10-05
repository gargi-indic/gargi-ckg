# Error Model / Error Classification

## Status
Accepted

## Date
2026-10-05

## Context
The Reactive Graph Engine executes DAGs asynchronously using Temporal as an underlying orchestrator. Because individual node executions map to Temporal Activities or child workflows, they are subject to distributed systems concerns like network partitions, temporary outages, and fundamental logic failures.

To implement proper fault tolerance, backoff retry strategies, and compensation logic (Saga pattern) as mandated by the requirements (SPEC.md), we need a standardized way to classify errors that occur during node execution, graph parsing, and state transitions.

## Decision
We classify all engine errors into three distinct classes:

1. **Retryable Errors:**
   Transient failures such as network timeouts, temporary unavailability of a downstream service, or rate limits. These errors should be subject to configured backoff retry strategies by the orchestrator.
2. **Non-Retryable Errors:**
   Deterministic failures such as validation errors, invalid graph definitions, or business logic rule violations. Retrying these will consistently yield the same failure, so execution of that node should be aborted immediately and the graph marked as failed.
3. **Critical Errors:**
   Severe failures indicating systemic issues or data corruption that invalidate the graph's overall state integrity. When a critical error is encountered, it must trigger compensation workflows (Saga pattern) to roll back any distributed state changes or side-effects caused by earlier successful nodes.

*(Note: The specific Go types and interfaces implementing this classification will be defined as part of the `NodeInterface` implementation.)*

## Consequences
- **Positive:** Temporal can automatically apply different retry policies based on the error class, optimizing execution time and preventing resource exhaustion from retrying deterministic errors.
- **Positive:** Provides a clear trigger mechanism for the Saga compensation workflows when encountering Critical errors.
- **Negative:** Node logic developers must explicitly think about and classify every error they return, adding some boilerplate compared to simply returning a standard `error`.

## Alternatives
- **A generic binary error model (Success/Fail):** This fails to leverage Temporal's retry capabilities for transient failures and lacks the necessary nuance to trigger Saga rollbacks securely.
- **Relying solely on Temporal's built-in error types:** This tightly couples the domain logic (`internal/graph` and `internal/reactive`) to the orchestrator (`internal/orchestrator`), violating our package boundaries and decoupling rules (ADR-002). We must define an orchestration-agnostic error classification model.