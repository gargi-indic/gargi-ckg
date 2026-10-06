package engine

import (
	"context"
	"testing"
)

// customNode embeds BaseNode and overrides only Execute.
type customNode struct {
	BaseNode
}

// Execute overrides the BaseNode implementation.
func (c *customNode) Execute(_ context.Context, nc NodeContext) (*StateMutation, error) {
	mut := NewStateMutation(
		nc.NodeID(),
		nc.State().Version(),
		nil,
		map[string]any{"executed": true},
		nil,
		nil,
	)
	return &mut, nil
}

func TestBaseNodeSatisfiesInterface(t *testing.T) {
	// Assert it can be assigned
	var node NodeInterface = &BaseNode{}

	ctx := context.Background()
	nc := &fakeNodeContext{}

	// Test defaults
	if err := node.OnInit(ctx, nc); err != nil {
		t.Errorf("expected OnInit to return nil, got %v", err)
	}

	ready, err := node.EvaluateReactivity(ctx, nc)
	if err != nil || !ready {
		t.Errorf("expected EvaluateReactivity to return (true, nil), got (%v, %v)", ready, err)
	}

	mut, err := node.Execute(ctx, nc)
	if err != nil || mut != nil {
		t.Errorf("expected Execute to return (nil, nil), got (%v, %v)", mut, err)
	}

	if err := node.OnSuccess(ctx, nc, nil); err != nil {
		t.Errorf("expected OnSuccess to return nil, got %v", err)
	}

	if err := node.OnFailure(ctx, nc, nil); err != nil {
		t.Errorf("expected OnFailure to return nil, got %v", err)
	}
}

func TestEmbeddedBaseNode(t *testing.T) {
	// Assert the embedded struct satisfies the interface
	var node NodeInterface = &customNode{}

	gdef, _ := NewGraph("graph-1", []*Node{}, []*Edge{})
	gstate, _ := NewGraphState("exec-1", gdef)

	ctx := context.Background()
	nc := &fakeNodeContext{
		nodeID: "test-node",
		state:  gstate,
	}

	// Should still use default for OnInit
	if err := node.OnInit(ctx, nc); err != nil {
		t.Errorf("expected OnInit to return nil, got %v", err)
	}

	// Should use overridden Execute
	mut, err := node.Execute(ctx, nc)
	if err != nil {
		t.Errorf("expected no error from Execute, got %v", err)
	}
	if mut == nil {
		t.Fatal("expected non-nil mutation")
	}

	if val, ok := mut.Set["executed"]; !ok || val != true {
		t.Errorf("expected mutation to set 'executed' to true, got %v", val)
	}
}
