package engine

import (
	"context"
)

// BaseNode provides no-op defaults for the NodeInterface methods.
// It is intended to be embedded in custom node structs so they only
// need to implement the methods they require (usually Execute).
// It uses pointer receivers.
type BaseNode struct{}

// Compile-time assertion that *BaseNode implements NodeInterface.
var _ NodeInterface = (*BaseNode)(nil)

// OnInit provides a default no-op implementation returning nil.
func (b *BaseNode) OnInit(_ context.Context, _ NodeContext) error {
	return nil
}

// EvaluateReactivity provides a default implementation returning true, nil.
func (b *BaseNode) EvaluateReactivity(_ context.Context, _ NodeContext) (bool, error) {
	return true, nil
}

// Execute provides a default no-op implementation returning nil, nil.
func (b *BaseNode) Execute(_ context.Context, _ NodeContext) (*StateMutation, error) {
	return nil, nil
}

// OnSuccess provides a default no-op implementation returning nil.
func (b *BaseNode) OnSuccess(_ context.Context, _ NodeContext, _ *StateMutation) error {
	return nil
}

// OnFailure provides a default no-op implementation returning nil.
func (b *BaseNode) OnFailure(_ context.Context, _ NodeContext, _ error) error {
	return nil
}
