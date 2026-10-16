package engine

import (
	"errors"
)

import (
	"fmt"
)

// JoinMode defines the fan-in condition resolution strategy for nodes with multiple incoming edges.
type JoinMode string

const (
	// JoinModeAllOf requires all incoming edges to be satisfied (condition true and upstream succeeded).
	JoinModeAllOf JoinMode = "all_of"
	// JoinModeAnyOf requires at least one incoming edge to be satisfied (condition true and upstream succeeded).
	JoinModeAnyOf JoinMode = "any_of"
)

// Valid reports whether m is a supported JoinMode constant.
func (m JoinMode) Valid() bool {
	return m == JoinModeAllOf || m == JoinModeAnyOf
}

// ParseJoinMode parses string representations, accepting canonical snake_case ("all_of", "any_of")
// and kebab-case ("all-of", "any-of"). Empty string returns JoinModeAllOf.
func ParseJoinMode(s string) (JoinMode, error) {
	switch s {
	case "all_of", "all-of", "":
		return JoinModeAllOf, nil
	case "any_of", "any-of":
		return JoinModeAnyOf, nil
	default:
		return "", fmt.Errorf("invalid join mode: %q", s)
	}
}

// NodeID represents a unique identifier for a node within a graph.
type NodeID string

// Node represents a vertex in the execution graph.
// It is immutable after creation.
type Node struct {
	id       NodeID
	nodeType string
	config   map[string]any
	joinMode JoinMode
}

// NewNode creates a new Node instance.
// It returns an error if id or nodeType is empty.
// A deep copy of the config map is made to ensure immutability.
// Config is expected to hold JSON-like values (string, number, bool, nil, map[string]any, []any).
// Only map[string]any and []any are deep-copied. Other types are shared.
func NewNode(id NodeID, nodeType string, config map[string]any) (*Node, error) {
	return NewNodeWithJoinMode(id, nodeType, config, JoinModeAllOf)
}

// NewNodeWithJoinMode creates a new Node instance with a specific JoinMode.
func NewNodeWithJoinMode(id NodeID, nodeType string, config map[string]any, joinMode JoinMode) (*Node, error) {
	if id == "" {
		return nil, errors.New("node ID cannot be empty")
	}
	if nodeType == "" {
		return nil, errors.New("node type cannot be empty")
	}
	if joinMode == "" {
		joinMode = JoinModeAllOf
	}
	if !joinMode.Valid() {
		return nil, fmt.Errorf("invalid join mode: %q", joinMode)
	}

	return &Node{
		id:       id,
		nodeType: nodeType,
		config:   deepCopyMap(config),
		joinMode: joinMode,
	}, nil
}

// ID returns the Node's identifier.
func (n *Node) ID() NodeID {
	return n.id
}

// Type returns the type of the Node.
func (n *Node) Type() string {
	return n.nodeType
}

// Config returns a deep copy of the Node's configuration map.
// Config is expected to hold JSON-like values (string, number, bool, nil, map[string]any, []any).
// Only map[string]any and []any are deep-copied. Other types are shared.
func (n *Node) Config() map[string]any {
	return deepCopyMap(n.config)
}

// JoinMode returns the Node's join mode.
func (n *Node) JoinMode() JoinMode {
	if n == nil || n.joinMode == "" {
		return JoinModeAllOf
	}
	return n.joinMode
}

func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = deepCopyAny(v)
	}
	return cp
}

func deepCopySlice(s []any) []any {
	if s == nil {
		return nil
	}
	cp := make([]any, len(s))
	for i, v := range s {
		cp[i] = deepCopyAny(v)
	}
	return cp
}

func deepCopyAny(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return deepCopyMap(val)
	case []any:
		return deepCopySlice(val)
	default:
		// Primitive types and other types are shallow copied (assumed immutable or copied by value)
		return val
	}
}
