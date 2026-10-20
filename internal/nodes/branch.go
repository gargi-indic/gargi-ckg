package nodes

import (
	"context"
	"errors"
	"reflect"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

type branchConfig struct {
	Input    string `json:"input"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}

func (c *branchConfig) Validate() error {
	if c.Input == "" {
		return errors.New("input key is required")
	}

	if c.Operator == "" {
		c.Operator = "truthy"
	}

	switch c.Operator {
	case "eq", "ne", "gt", "gte", "lt", "lte":
		if c.Value == nil {
			return errors.New("value is required for operator " + c.Operator)
		}
	case "exists", "truthy":
		// value not required
	default:
		return errors.New("unknown operator: " + c.Operator)
	}

	return nil
}

type branchNode struct {
	*engine.BaseNode
	cfg branchConfig
}

var _ engine.NodeInterface = (*branchNode)(nil)

func (n *branchNode) Execute(_ context.Context, nc engine.NodeContext) (*engine.StateMutation, error) {
	inputs := nc.Inputs()
	val, exists := inputs[n.cfg.Input]

	op := n.cfg.Operator
	if op == "" {
		op = "truthy"
	}

	var result bool
	var err error

	switch op {
	case "exists":
		result = exists
	case "truthy":
		result = isTruthy(val)
	case "eq":
		result = isEqual(val, n.cfg.Value)
	case "ne":
		result = !isEqual(val, n.cfg.Value)
	case "gt", "gte", "lt", "lte":
		if !exists {
			return nil, engine.NewNonRetryableError(errors.New("missing input key for comparison: " + n.cfg.Input))
		}
		result, err = compare(val, n.cfg.Value, op)
		if err != nil {
			return nil, engine.NewNonRetryableError(err)
		}
	default:
		return nil, engine.NewNonRetryableError(errors.New("unknown operator: " + op))
	}

	branch := "false"
	if result {
		branch = "true"
	}

	// We pass 0 for ExpectedVersion as the engine is responsible for stamping it.
	mut := engine.NewStateMutation(nc.NodeID(), 0, nil, map[string]any{
		"result": result,
		"branch": branch,
	}, nil, nil)

	return &mut, nil
}

func isTruthy(val any) bool {
	if val == nil {
		return false
	}
	v := reflect.ValueOf(val)
	switch v.Kind() {
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() != 0
	case reflect.Float32, reflect.Float64:
		return v.Float() != 0
	case reflect.Complex64, reflect.Complex128:
		return v.Complex() != 0
	case reflect.String:
		return v.String() != ""
	case reflect.Array, reflect.Map, reflect.Slice, reflect.Chan:
		return v.Len() > 0
	default:
		return true // Other types (structs, pointers to non-nil) are truthy if not nil
	}
}

func isEqual(a, b any) bool {
	// Normalize numbers for comparison
	if isNumeric(a) && isNumeric(b) {
		fa, _ := toFloat(a)
		fb, _ := toFloat(b)
		return fa == fb
	}
	return reflect.DeepEqual(a, b)
}

func compare(a, b any, op string) (bool, error) {
	if isNumeric(a) && isNumeric(b) {
		fa, _ := toFloat(a)
		fb, _ := toFloat(b)
		switch op {
		case "gt":
			return fa > fb, nil
		case "gte":
			return fa >= fb, nil
		case "lt":
			return fa < fb, nil
		case "lte":
			return fa <= fb, nil
		}
	}

	sa, oka := a.(string)
	sb, okb := b.(string)
	if oka && okb {
		switch op {
		case "gt":
			return sa > sb, nil
		case "gte":
			return sa >= sb, nil
		case "lt":
			return sa < sb, nil
		case "lte":
			return sa <= sb, nil
		}
	}

	return false, errors.New("mismatched kinds or unsupported types for inequality operator")
}

func isNumeric(v any) bool {
	if v == nil {
		return false
	}
	switch reflect.TypeOf(v).Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func toFloat(v any) (float64, bool) {
	val := reflect.ValueOf(v)
	switch val.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(val.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(val.Uint()), true
	case reflect.Float32, reflect.Float64:
		return val.Float(), true
	default:
		return 0, false
	}
}
