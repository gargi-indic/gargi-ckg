# Guide: Authoring Custom Nodes

This guide walks through creating custom nodes in the Gargi Reactive Graph Engine.

---

## 1. Overview of Node Lifecycle

All nodes in Gargi implement the `engine.NodeInterface`:

```go
type NodeInterface interface {
    OnInit(ctx context.Context, nc NodeContext) error
    EvaluateReactivity(ctx context.Context, nc NodeContext) (bool, error)
    Execute(ctx context.Context, nc NodeContext) (*StateMutation, error)
    OnSuccess(ctx context.Context, nc NodeContext, result *StateMutation) error
    OnFailure(ctx context.Context, nc NodeContext, cause error) error
}
```

By embedding `engine.BaseNode`, you only need to override the hooks relevant to your node (typically `Execute`).

---

## 2. Implementing a Custom Node

Below is a complete example of a custom currency converter node that reads upstream inputs, performs a conversion, and emits a state mutation.

```go
package custom

import (
    "context"
    "errors"
    "fmt"

    "github.com/gargi-indic/gargi-ckg/pkg/engine"
)

// ConverterConfig defines configuration fields declared in graph definitions.
type ConverterConfig struct {
    TargetCurrency string  `json:"target_currency"`
    ExchangeRate   float64 `json:"exchange_rate"`
}

// CurrencyConverterNode performs currency conversions.
type CurrencyConverterNode struct {
    engine.BaseNode
    cfg ConverterConfig
}

// NewCurrencyConverterNode constructs the node instance from raw JSON/YAML configuration.
func NewCurrencyConverterNode(rawConfig any) (engine.NodeInterface, error) {
    var cfg ConverterConfig
    if rawConfig != nil {
        cfgMap, ok := rawConfig.(map[string]any)
        if !ok {
            return nil, errors.New("invalid config format: expected object")
        }
        if curr, ok := cfgMap["target_currency"].(string); ok {
            cfg.TargetCurrency = curr
        }
        if rate, ok := cfgMap["exchange_rate"].(float64); ok {
            cfg.ExchangeRate = rate
        }
    }

    if cfg.ExchangeRate <= 0 {
        return nil, errors.New("exchange_rate must be greater than zero")
    }

    return &CurrencyConverterNode{cfg: cfg}, nil
}

// Execute performs the conversion logic.
func (n *CurrencyConverterNode) Execute(ctx context.Context, nc engine.NodeContext) (*engine.StateMutation, error) {
    // 1. Read input variables resolved from upstream edges
    amountVal, ok := nc.GetInput("amount")
    if !ok {
        return nil, engine.NewNonRetryableError(errors.New("missing required input: amount"))
    }

    amount, ok := amountVal.(float64)
    if !ok {
        return nil, engine.NewNonRetryableError(errors.New("amount must be a numeric value"))
    }

    // 2. Perform business logic
    converted := amount * n.cfg.ExchangeRate

    // 3. Emit output mutation
    status := engine.StatusSucceeded
    mutation := engine.NewStateMutation(
        nc.NodeID(),
        nc.Version(),
        &status,
        map[string]any{
            "converted_amount": converted,
            "currency":         n.cfg.TargetCurrency,
        },
        nil,
        nil,
    )

    return &mutation, nil
}
```

---

## 3. Error Handling Categories

Gargi recognizes three error classifications during node execution:

1. **Retryable Errors (`engine.NewRetryableError(err)`)**:
   Transient errors (e.g., temporary network blip, rate limit 429). Temporal automatically retries the node according to its configured `RetryPolicy`.

2. **Non-Retryable Errors (`engine.NewNonRetryableError(err)`)**:
   Permanent errors (e.g., malformed input, invalid credentials). Halts the node immediately without wasting retry attempts.

3. **Critical Errors (`engine.NewCriticalError(err)`)**:
   Fatal failures that trigger Saga compensation policies (`OnCriticalFailure: "compensate"`), rolling back preceding successful nodes in reverse topological order.

---

## 4. Registering Custom Nodes

Register your custom node factory with `engine.Registry` during application startup:

```go
reg := engine.NewRegistry()

// Register standard nodes (transform, passthrough, wait_for_signal, etc.)
nodes.RegisterDefaults(reg, nodes.Dependencies{
    Signals: orchestrator.NewTemporalSignalSource(),
})

// Register custom node type
err := reg.Register("currency_converter", NewCurrencyConverterNode, nil)
if err != nil {
    log.Fatalf("failed to register custom node: %v", err)
}
```

### Wiring into Temporal Worker & Server
Pass the registry to `orchestrator.NewNodeActivities(reg)` in `cmd/worker/main.go` and `cmd/server/main.go`:

```go
na := orchestrator.NewNodeActivities(reg)
workerManager.RegisterActivityWithOptions(na.ExecuteNodeActivity, activity.RegisterOptions{
    Name: orchestrator.ExecuteNodeActivityName,
})
```

---

## 5. Using the Custom Node in Graph Definitions

Declare the custom node type in your graph definitions:

```yaml
apiVersion: graph/v1
id: checkout-pipeline
nodes:
  - id: calculate_cart
    type: transform
    config:
      set:
        amount: 150.00
  - id: convert_to_eur
    type: currency_converter
    config:
      target_currency: "EUR"
      exchange_rate: 0.92
    retryPolicy: standard_retry
edges:
  - from: calculate_cart
    to: convert_to_eur
retryPolicies:
  standard_retry:
    maxAttempts: 3
    initialIntervalMs: 200
```
