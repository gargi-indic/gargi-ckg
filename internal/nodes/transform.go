package nodes

import (
	"context"
	"errors"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

type transformConfig struct {
	Mapping map[string]string `json:"mapping"`
	Set     map[string]any    `json:"set"`
}

func (c *transformConfig) Validate() error {
	if len(c.Mapping) == 0 && len(c.Set) == 0 {
		return errors.New("at least one of Mapping or Set must be non-empty")
	}

	for k, v := range c.Mapping {
		if k == "" || v == "" {
			return errors.New("empty keys or values in Mapping are not allowed")
		}
	}

	for k := range c.Set {
		if k == "" {
			return errors.New("empty keys in Set are not allowed")
		}
		if _, ok := c.Mapping[k]; ok {
			return errors.New("key present in both Mapping and Set: " + k)
		}
	}

	return nil
}

type transformNode struct {
	*engine.BaseNode
	cfg transformConfig
}

var _ engine.NodeInterface = (*transformNode)(nil)

func (n *transformNode) Execute(_ context.Context, nc engine.NodeContext) (*engine.StateMutation, error) {
	inputs := nc.Inputs()
	outputs := make(map[string]any)

	for outKey, inKey := range n.cfg.Mapping {
		val, ok := inputs[inKey]
		if !ok {
			return nil, engine.NewNonRetryableError(errors.New("missing required input key: " + inKey))
		}
		outputs[outKey] = val
	}

	for k, v := range n.cfg.Set {
		outputs[k] = v
	}

	// NewStateMutation will deep copy the 'outputs' map again, preventing any aliasing with nc.Inputs()
	// We pass 0 for ExpectedVersion as the engine is responsible for stamping it.
	mut := engine.NewStateMutation(nc.NodeID(), 0, nil, outputs, nil, nil)
	return &mut, nil
}
