package nodes

import (
	"context"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

// passthroughConfig acts as a schema for passthrough nodes.
// An empty struct ensures any unknown configuration keys are rejected.
type passthroughConfig struct{}

type passthroughNode struct {
	*engine.BaseNode
}

var _ engine.NodeInterface = (*passthroughNode)(nil)

// Execute deeply copies all inputs and returns them in a StateMutation.
func (n *passthroughNode) Execute(_ context.Context, nc engine.NodeContext) (*engine.StateMutation, error) {
	// nc.Inputs() returns a deep copy, but the user explicitly requested we guarantee
	// output values are not aliased. NewStateMutation does a deep copy internally,
	// satisfying the requirement.
	inputs := nc.Inputs()

	mut := engine.NewStateMutation(nc.NodeID(), nc.State().Version(), nil, inputs, nil, nil)
	return &mut, nil
}
