package engine

import (
	"errors"
	"strings"
)

// Format represents the expected data format of a graph definition.
type Format string

const (
	// FormatJSON indicates the definition is in JSON format.
	FormatJSON Format = "json"
	// FormatYAML indicates the definition is in YAML format.
	FormatYAML Format = "yaml"
)

// ParseFormat returns a Format from a string, accepting "json", "yaml", and "yml" (case-insensitive).
// It returns an error if the format is not supported.
func ParseFormat(f string) (Format, error) {
	switch strings.ToLower(f) {
	case "json":
		return FormatJSON, nil
	case "yaml", "yml":
		return FormatYAML, nil
	default:
		return "", errors.New("unsupported format, must be json or yaml")
	}
}

// GraphDefinition represents the unvalidated schema structure of a graph execution definition.
// Values inside this struct may be semantically invalid (e.g. duplicate IDs or unresolvable edges),
// and must be validated by higher layers.
type GraphDefinition struct {
	APIVersion    string                            `json:"apiVersion"`
	ID            string                            `json:"id"`
	Name          string                            `json:"name,omitempty"`
	Description   string                            `json:"description,omitempty"`
	Nodes         []NodeDefinition                  `json:"nodes"`
	Edges         []EdgeDefinition                  `json:"edges,omitempty"`
	RetryPolicies map[string]RetryPolicy            `json:"retryPolicies,omitempty"`
	Compensations map[string]CompensationDefinition `json:"compensations,omitempty"`
	FailurePolicy *FailurePolicy                    `json:"failurePolicy,omitempty"`
}

// NodeDefinition defines a node within the GraphDefinition.
type NodeDefinition struct {
	ID           NodeID         `json:"id"`
	Type         string         `json:"type"`
	Config       map[string]any `json:"config,omitempty"`
	RetryPolicy  string         `json:"retryPolicy,omitempty"`
	Compensation string         `json:"compensation,omitempty"`
}

// EdgeDefinition defines a directional edge between two nodes.
type EdgeDefinition struct {
	From        NodeID            `json:"from"`
	To          NodeID            `json:"to"`
	Condition   string            `json:"condition,omitempty"`
	DataMapping map[string]string `json:"dataMapping,omitempty"`
}

// RetryPolicy defines backoff retry settings for node execution.
type RetryPolicy struct {
	MaxAttempts        *int     `json:"maxAttempts,omitempty"`
	InitialIntervalMs  *int     `json:"initialIntervalMs,omitempty"`
	BackoffCoefficient *float64 `json:"backoffCoefficient,omitempty"`
	MaxIntervalMs      *int     `json:"maxIntervalMs,omitempty"`
}

// CompensationDefinition defines a compensation node used for Saga rollbacks.
type CompensationDefinition struct {
	Type   string         `json:"type"`
	Config map[string]any `json:"config,omitempty"`
}

// FailurePolicy defines how the graph should behave on failures.
type FailurePolicy struct {
	OnNodeFailure     string `json:"onNodeFailure,omitempty"`
	OnCriticalFailure string `json:"onCriticalFailure,omitempty"`
}

// DefinitionParser parses a raw byte slice into a GraphDefinition.
// If there are structural errors (e.g., syntax errors, unknown fields, incorrect types),
// it returns a *ValidationErrors error containing all encountered issues.
type DefinitionParser interface {
	Parse(data []byte, format Format) (*GraphDefinition, error)
}

// ToGraph converts a GraphDefinition into an immutable Graph instance.
// It creates Nodes and Edges using NewNode and NewEdge, and constructs the Graph using NewGraph.
// Note: This method completely ignores retry policies, compensations, and failure policies,
// as they are handled by higher-level orchestrators, not the core Graph struct.
// It fails if the receiver is nil or if any graph construction errors occur.
func (d *GraphDefinition) ToGraph() (*Graph, error) {
	if d == nil {
		return nil, errors.New("GraphDefinition is nil")
	}

	nodes := make([]*Node, 0, len(d.Nodes))
	for _, nDef := range d.Nodes {
		n, err := NewNode(nDef.ID, nDef.Type, nDef.Config)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}

	edges := make([]*Edge, 0, len(d.Edges))
	for _, eDef := range d.Edges {
		e, err := NewEdge(eDef.From, eDef.To, eDef.Condition, eDef.DataMapping)
		if err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}

	return NewGraph(d.ID, nodes, edges)
}
