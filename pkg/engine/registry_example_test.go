package engine_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

// MyConfig represents the configuration for our custom node.
type MyConfig struct {
	MaxRetries int `json:"max_retries"`
}

// Validate implements the validation interface.
func (c MyConfig) Validate() error {
	if c.MaxRetries < 0 {
		return errors.New("max_retries cannot be negative")
	}
	return nil
}

// MyNode is a custom node type.
type MyNode struct {
	*engine.BaseNode
	cfg MyConfig
}

// Execute performs the node's custom logic.
func (n *MyNode) Execute(_ context.Context, _ engine.NodeContext) (*engine.StateMutation, error) {
	fmt.Printf("Executing MyNode with MaxRetries = %d\n", n.cfg.MaxRetries)
	return nil, nil
}

// ExampleRegistry demonstrates registering and instantiating a custom node.
func ExampleRegistry() {
	// 1. Create a new registry.
	reg := engine.NewRegistry()

	// 2. Register a node factory.
	err := reg.Register("my_node", func(cfg any) (engine.NodeInterface, error) {
		return &MyNode{
			BaseNode: &engine.BaseNode{},
			cfg:      cfg.(MyConfig),
		}, nil
	}, MyConfig{})
	if err != nil {
		fmt.Printf("Registration failed: %v\n", err)
		return
	}

	// 3. Define the config (as it would be parsed from JSON).
	config := map[string]any{
		"max_retries": float64(3), // JSON numbers arrive as float64
	}

	// 4. Instantiate the node.
	node, err := reg.Instantiate("my_node", config)
	if err != nil {
		fmt.Printf("Instantiation failed: %v\n", err)
		return
	}

	// 5. Execute the node.
	myNode := node.(*MyNode)
	_, _ = myNode.Execute(context.Background(), nil)

	// Output:
	// Executing MyNode with MaxRetries = 3
}
