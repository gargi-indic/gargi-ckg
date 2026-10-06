package nodes

import (
	"context"
	"errors"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
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

	// We need to deep copy the static config values to prevent state aliasing.
	copiedSet := deepCopyMap(n.cfg.Set)
	for k, v := range copiedSet {
		outputs[k] = v
	}

	// NewStateMutation will deep copy the 'outputs' map again, preventing any aliasing with nc.Inputs()
	mut := engine.NewStateMutation(nc.NodeID(), nc.State().Version(), nil, outputs, nil, nil)
	return &mut, nil
}

// deepCopyMap creates a deep copy of a map[string]any.
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
		return val
	}
}
