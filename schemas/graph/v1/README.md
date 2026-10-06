# Gargi CKG Graph Definition Schema (v1)

This directory contains the JSON Schema (`schema.json`) defining the structure of a `graph/v1` execution payload for the Gargi CKG Reactive Graph Engine.

## Compatibility and Versioning

The `apiVersion` field is strictly required. Any breaking changes to this schema will require bumping the major version to `graph/v2` and creating a new directory. Additive optional fields are allowed within `v1`. The schema uses the JSON Schema draft 2020-12 specification and operates under a strict rule where `additionalProperties: false` is enforced on every object to prevent unknown fields.

There is no production validation logic in this directory. The runtime engine validates node configurations dynamically per node type based on internal registries.

## Structure

A graph definition has the following top-level components:

* `apiVersion`: Must be `graph/v1`.
* `id`: Graph identifier. Must match `^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`.
* `name`: (Optional) Human-readable name.
* `description`: (Optional) Description.
* `nodes`: Array of nodes. At least one node is required.
* `edges`: (Optional) Array of edges between nodes.
* `retryPolicies`: (Optional) Map of retry policies.
* `compensations`: (Optional) Map of compensation handlers. Handlers run in reverse order for succeeded nodes after a critical failure (Saga pattern) and are **not** nodes of the DAG itself.
* `failurePolicy`: (Optional) Defines the fallback behavior on failure.

### Node Structure

Note: The node `id` strictly follows `^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`. This constraint is stricter than the engine's internal node `id` requirement (which only requires non-empty strings) for standardizing naming conventions.

* `id`: The node identifier.
* `type`: The registry name for the node type (e.g., `conditional_branch`). Must match `^[A-Za-z][A-Za-z0-9_.-]*$`.
* `config`: (Optional) A free-form object holding type-specific configuration, validated dynamically at runtime.
* `retryPolicy`: (Optional) The name of a retry policy defined in `retryPolicies`.
* `compensation`: (Optional) The name of a compensation handler defined in `compensations`.

### Edge Structure

* `from`: Origin node ID.
* `to`: Destination node ID.
* `condition`: (Optional) An opaque expression string. The expression language is currently undecided, so conditions are not evaluated or validated yet. An absent condition means "always proceed".
* `dataMapping`: (Optional) A map of target keys to opaque source path strings (e.g., `upstream_node.output_key`). Path grammar is undocumented for now and treated opaquely.

### Retry Policy defaults
* `maxAttempts`: (integer >= 1) Default: 1 (no retry).
* `initialIntervalMs`: (integer >= 1)
* `backoffCoefficient`: (number >= 1) Default: 2.
* `maxIntervalMs`: (integer >= 1)

### Failure Policy defaults
* `onNodeFailure`: (`fail_graph` | `continue`) Default: `fail_graph`. Determines what happens when a node fails with a non-retryable error or exhausts its retries.
* `onCriticalFailure`: (`compensate` | `fail_graph`) Default: `compensate`.

## Example

Below is an annotated example of a small checkout workflow (`example.yaml`).

```yaml
apiVersion: graph/v1
id: checkout-workflow
name: Checkout Graph
description: An example checkout graph processing an order.

nodes:
  # Initial passthrough node to start the process
  - id: start
    type: passthrough
    config: {}

  # Validates the order payload structure
  - id: validate-order
    type: transform
    config:
      schema: order_schema

  # Calls inventory service via HTTP
  - id: reserve-inventory
    type: http_call
    config:
      url: "https://inventory.example.com/reserve"
      method: "POST"
    retryPolicy: standard-retry
    compensation: cancel-inventory

  # Conditional branch to check if we should apply a discount
  - id: check-discount
    type: conditional_branch
    config: {}

  # Wait for an external payment signal
  - id: wait-for-payment
    type: wait_for_signal
    config:
      signalName: "payment_success"

edges:
  - from: start
    to: validate-order

  - from: validate-order
    to: reserve-inventory
    dataMapping:
      orderId: "start.orderId"

  - from: reserve-inventory
    to: check-discount

  # Evaluates an opaque condition to proceed to waiting for payment
  - from: check-discount
    to: wait-for-payment
    condition: "reserve-inventory.status == 'reserved'"

retryPolicies:
  standard-retry:
    maxAttempts: 3
    initialIntervalMs: 1000
    backoffCoefficient: 2
    maxIntervalMs: 10000

compensations:
  # This handler is executed if the workflow hits a critical failure after this step succeeds.
  # Note: Compensations are NOT DAG nodes.
  cancel-inventory:
    type: http_call
    config:
      url: "https://inventory.example.com/cancel"
      method: "POST"

failurePolicy:
  onNodeFailure: fail_graph
  onCriticalFailure: compensate
```
