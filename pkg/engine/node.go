package engine

import (
	"errors"
)

// NodeID represents a unique identifier for a node within a graph.
type NodeID string

// Node represents a vertex in the execution graph.
// It is immutable after creation.
type Node struct {
	id       NodeID
	nodeType string
	config   map[string]any
}

// NewNode creates a new Node instance.
// It returns an error if id or nodeType is empty.
// A deep copy of the config map is made to ensure immutability.
func NewNode(id NodeID, nodeType string, config map[string]any) (*Node, error) {
	if id == "" {
		return nil, errors.New("node ID cannot be empty")
	}
	if nodeType == "" {
		return nil, errors.New("node type cannot be empty")
	}

	return &Node{
		id:       id,
		nodeType: nodeType,
		config:   deepCopyMap(config),
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
func (n *Node) Config() map[string]any {
	return deepCopyMap(n.config)
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
