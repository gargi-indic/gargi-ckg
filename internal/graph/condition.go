package graph

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/cel-go/cel"
)

const (
	// DefaultConditionCostLimit limits CEL evaluation cost to ensure determinism and prevent infinite loops/hangs.
	DefaultConditionCostLimit uint64 = 1000
)

// CompiledCondition is an immutable, concurrently safe wrapper around a compiled CEL program.
type CompiledCondition struct {
	expr    string
	program cel.Program
}

func uniqueStrings(ss []string) []string {
	seen := make(map[string]struct{}, len(ss))
	var res []string
	for _, s := range ss {
		if s == "" || s == "upstream" || s == "nodes" || s == "state" {
			continue
		}
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			res = append(res, s)
		}
	}
	return res
}

func buildEnv(nodeIDs ...string) (*cel.Env, error) {
	opts := []cel.EnvOption{
		cel.Variable("upstream", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("nodes", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("state", cel.MapType(cel.StringType, cel.DynType)),
	}

	for _, id := range uniqueStrings(nodeIDs) {
		opts = append(opts, cel.Variable(id, cel.MapType(cel.StringType, cel.DynType)))
	}

	return cel.NewEnv(opts...)
}

// CompileCondition parses and type-checks the given CEL expression, expecting a boolean result.
func CompileCondition(expr string, nodeIDs ...string) (*CompiledCondition, error) {
	if strings.TrimSpace(expr) == "" {
		return nil, fmt.Errorf("condition cannot be empty")
	}
	if utf8.RuneCountInString(expr) > 4096 {
		return nil, fmt.Errorf("condition length exceeds maximum of 4096 runes")
	}

	env, err := buildEnv(nodeIDs...)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to compile condition: %w", issues.Err())
	}

	if !ast.OutputType().IsExactType(cel.BoolType) {
		return nil, fmt.Errorf("condition must evaluate to bool, got %v", ast.OutputType())
	}

	prg, err := env.Program(ast, cel.CostLimit(DefaultConditionCostLimit), cel.EvalOptions(cel.OptTrackCost))
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL program: %w", err)
	}

	return &CompiledCondition{
		expr:    expr,
		program: prg,
	}, nil
}

// ValidateCondition statically validates that a CEL condition compiles and type-checks.
func ValidateCondition(expr string, nodeIDs ...string) error {
	_, err := CompileCondition(expr, nodeIDs...)
	return err
}

func prepareVars(vars map[string]any) map[string]any {
	res := make(map[string]any, len(vars)+3)
	for k, v := range vars {
		res[k] = v
	}
	if _, ok := res["upstream"]; !ok {
		res["upstream"] = map[string]any{}
	}
	if _, ok := res["nodes"]; !ok {
		res["nodes"] = map[string]any{}
	}
	if _, ok := res["state"]; !ok {
		res["state"] = map[string]any{}
	}
	return res
}

// Evaluate runs the compiled condition against the given variables.
func (c *CompiledCondition) Evaluate(vars map[string]any) (bool, error) {
	if c == nil || c.program == nil {
		return false, fmt.Errorf("nil compiled condition")
	}

	prepared := prepareVars(vars)

	out, _, err := c.program.Eval(prepared)
	if err != nil {
		return false, fmt.Errorf("evaluation failed: %w", err)
	}

	val, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("condition evaluated to non-bool %T", out.Value())
	}

	return val, nil
}

// EvaluateCondition is a convenience method to compile and immediately evaluate a condition.
func EvaluateCondition(expr string, vars map[string]any, nodeIDs ...string) (bool, error) {
	c, err := CompileCondition(expr, nodeIDs...)
	if err != nil {
		return false, err
	}
	return c.Evaluate(vars)
}
