package engine

import "errors"

// Edge represents a directed connection between two nodes in the execution graph.
// It is immutable after creation.
type Edge struct {
	from        NodeID
	to          NodeID
	condition   string
	dataMapping map[string]string
}

// NewEdge creates a new Edge instance connecting two nodes.
// It returns an error if the endpoints are empty or if it represents a self-loop.
// A defensive copy of dataMapping is made to ensure immutability.
func NewEdge(from, to NodeID, condition string, dataMapping map[string]string) (*Edge, error) {
	if from == "" {
		return nil, errors.New("from NodeID cannot be empty")
	}
	if to == "" {
		return nil, errors.New("to NodeID cannot be empty")
	}
	if from == to {
		return nil, errors.New("self-loops are not allowed")
	}

	return &Edge{
		from:        from,
		to:          to,
		condition:   condition,
		dataMapping: copyStringMap(dataMapping),
	}, nil
}

// From returns the ID of the source node.
func (e *Edge) From() NodeID {
	return e.from
}

// To returns the ID of the destination node.
func (e *Edge) To() NodeID {
	return e.to
}

// Condition returns the opaque string expression evaluated to traverse this edge.
func (e *Edge) Condition() string {
	return e.condition
}

// DataMapping returns a copy of the mapping defining how data from the source is passed to the target.
func (e *Edge) DataMapping() map[string]string {
	return copyStringMap(e.dataMapping)
}

func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
