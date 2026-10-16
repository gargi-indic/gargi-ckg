package reactive

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/gargi-indic/gargi-ckg/internal/graph"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

// ConditionEvaluator evaluates CEL expressions for edge conditions.
type ConditionEvaluator interface {
	Evaluate(ctx context.Context, expression string, upstreamOutput map[string]any, graphState *engine.GraphState) (bool, error)
}

// CELConditionEvaluator implements ConditionEvaluator by delegating to internal/graph.EvaluateCondition.
type CELConditionEvaluator struct{}

// NewCELConditionEvaluator creates a new CELConditionEvaluator.
func NewCELConditionEvaluator() (*CELConditionEvaluator, error) {
	return &CELConditionEvaluator{}, nil
}

// Evaluate evaluates a CEL expression against upstreamOutput and graphState.
func (e *CELConditionEvaluator) Evaluate(
	_ context.Context,
	expression string,
	upstreamOutput map[string]any,
	graphState *engine.GraphState,
) (bool, error) {
	if strings.TrimSpace(expression) == "" {
		return true, nil
	}

	out := upstreamOutput
	if out == nil {
		out = make(map[string]any)
	}

	vars := map[string]any{
		"upstream": map[string]any{
			"output": out,
			"status": string(engine.StatusSucceeded),
		},
	}

	nodesMap := make(map[string]any)
	var nodeIDs []string

	if graphState != nil {
		for _, ns := range graphState.NodeStates() {
			nodeID := string(ns.NodeID())
			nodeMap := make(map[string]any)
			nodeMap["status"] = string(ns.Status())
			nodeMap["output"] = ns.Output()
			nodeMap["errorMessage"] = ns.ErrorMessage()
			nodeMap["version"] = ns.Version()

			// copy non-colliding top-level keys from output
			for k, v := range ns.Output() {
				if _, exists := nodeMap[k]; !exists {
					nodeMap[k] = v
				}
			}

			vars[nodeID] = nodeMap
			nodesMap[nodeID] = nodeMap
			nodeIDs = append(nodeIDs, nodeID)
		}
	}

	vars["nodes"] = nodesMap
	vars["state"] = nodesMap

	return graph.EvaluateCondition(expression, vars, nodeIDs...)
}

// PreconditionResult captures the evaluated resolution of a node's incoming edges.
type PreconditionResult struct {
	Ready   bool
	Skipped bool
	Inputs  map[string]any
}

// PreconditionResolver evaluates join modes and edge conditions for graph nodes.
type PreconditionResolver struct {
	evaluator ConditionEvaluator
}

// NewPreconditionResolver creates a PreconditionResolver.
// If evaluator is nil, it uses a default CELConditionEvaluator.
func NewPreconditionResolver(evaluator ConditionEvaluator) *PreconditionResolver {
	if evaluator == nil {
		evaluator, _ = NewCELConditionEvaluator()
	}
	return &PreconditionResolver{
		evaluator: evaluator,
	}
}

// ApplyDataMapping projects fields from upstreamOutput into targetInputs according to mapping.
func ApplyDataMapping(
	mapping map[string]string,
	upstreamOutput map[string]any,
	targetInputs map[string]any,
) error {
	if len(mapping) == 0 {
		return nil
	}
	if targetInputs == nil {
		return fmt.Errorf("targetInputs map is nil")
	}

	keys := make([]string, 0, len(mapping))
	for k := range mapping {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, targetKey := range keys {
		sourcePath := mapping[targetKey]

		if val, exists := upstreamOutput[sourcePath]; exists {
			targetInputs[targetKey] = deepCopyAny(val)
			continue
		}

		parts := strings.Split(sourcePath, ".")
		if len(parts) > 1 {
			// First attempt full parts from the root
			resolved, err := resolveNestedPath(upstreamOutput, parts)
			if err == nil {
				targetInputs[targetKey] = deepCopyAny(resolved)
				continue
			}

			if _, exists := upstreamOutput[parts[0]]; !exists {
				// try resolving parts[1:] in upstreamOutput
				resolved, err := resolveNestedPath(upstreamOutput, parts[1:])
				if err == nil {
					targetInputs[targetKey] = deepCopyAny(resolved)
					continue
				}
			}
		}

		return fmt.Errorf("failed to resolve source path %q in upstream output", sourcePath)
	}

	return nil
}

func resolveNestedPath(m map[string]any, parts []string) (any, error) {
	var current any = m
	for _, p := range parts {
		mmap, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("path not found")
		}
		val, exists := mmap[p]
		if !exists {
			return nil, fmt.Errorf("path not found")
		}
		current = val
	}
	return current, nil
}

// deepCopyAny creates a deep copy of a value to ensure immutability.
func deepCopyAny(v any) any {
	switch val := v.(type) {
	case map[string]any:
		cp := make(map[string]any, len(val))
		for k, v2 := range val {
			cp[k] = deepCopyAny(v2)
		}
		return cp
	case []any:
		cp := make([]any, len(val))
		for i, v2 := range val {
			cp[i] = deepCopyAny(v2)
		}
		return cp
	default:
		return val
	}
}

// ResolveNodePreconditions checks all incoming edges of targetNodeID in g against state,
// respecting targetNode.JoinMode() (JoinModeAllOf vs JoinModeAnyOf).
func (r *PreconditionResolver) ResolveNodePreconditions(
	ctx context.Context,
	targetNodeID engine.NodeID,
	g *engine.Graph,
	state *engine.GraphState,
) (*PreconditionResult, error) {
	if r == nil {
		return nil, fmt.Errorf("PreconditionResolver is nil")
	}
	if g == nil {
		return nil, fmt.Errorf("graph is nil")
	}
	if state == nil {
		return nil, fmt.Errorf("graph state is nil")
	}

	targetNode := g.Node(targetNodeID)
	if targetNode == nil {
		return nil, fmt.Errorf("unknown node ID: %q", targetNodeID)
	}

	incoming := g.IncomingEdges(targetNodeID)
	if len(incoming) == 0 {
		return &PreconditionResult{Ready: true, Skipped: false, Inputs: make(map[string]any)}, nil
	}

	sort.Slice(incoming, func(i, j int) bool {
		return incoming[i].From() < incoming[j].From()
	})

	type edgeEval struct {
		edge      *engine.Edge
		state     *engine.NodeState
		satisfied bool
		pending   bool
	}

	var evals []edgeEval

	for _, e := range incoming {
		upState := state.NodeState(e.From())
		if upState == nil {
			return nil, fmt.Errorf("missing node state for %q", e.From())
		}

		status := upState.Status()
		isPending := status == engine.StatusPending || status == engine.StatusReady || status == engine.StatusRunning

		if isPending {
			evals = append(evals, edgeEval{edge: e, state: upState, satisfied: false, pending: true})
			continue
		}

		if status == engine.StatusSkipped || status == engine.StatusFailed || status == engine.StatusCompensating || status == engine.StatusCompensated {
			evals = append(evals, edgeEval{edge: e, state: upState, satisfied: false, pending: false})
			continue
		}

		if status == engine.StatusSucceeded {
			cond := e.Condition()
			if cond == "" {
				evals = append(evals, edgeEval{edge: e, state: upState, satisfied: true, pending: false})
			} else {
				ok, err := r.evaluator.Evaluate(ctx, cond, upState.Output(), state)
				if err != nil {
					return nil, err
				}
				evals = append(evals, edgeEval{edge: e, state: upState, satisfied: ok, pending: false})
			}
		}
	}

	joinMode := targetNode.JoinMode()

	if joinMode == engine.JoinModeAllOf {
		var anyUnsatisfied bool
		var anyPending bool
		for _, ev := range evals {
			if !ev.satisfied && !ev.pending {
				anyUnsatisfied = true
			}
			if ev.pending {
				anyPending = true
			}
		}

		if anyUnsatisfied {
			return &PreconditionResult{Ready: false, Skipped: true}, nil
		}
		if anyPending {
			return &PreconditionResult{Ready: false, Skipped: false}, nil
		}

		mergedInputs := make(map[string]any)
		for _, ev := range evals {
			mapping := ev.edge.DataMapping()
			if len(mapping) > 0 {
				if err := ApplyDataMapping(mapping, ev.state.Output(), mergedInputs); err != nil {
					return nil, err
				}
			}
		}
		return &PreconditionResult{Ready: true, Skipped: false, Inputs: mergedInputs}, nil
	}

	if joinMode == engine.JoinModeAnyOf {
		var anySatisfied bool
		allUnsatisfied := true

		for _, ev := range evals {
			if ev.satisfied {
				anySatisfied = true
			}
			if ev.satisfied || ev.pending {
				allUnsatisfied = false
			}
		}

		if anySatisfied {
			mergedInputs := make(map[string]any)
			for _, ev := range evals {
				if ev.satisfied {
					mapping := ev.edge.DataMapping()
					if len(mapping) > 0 {
						if err := ApplyDataMapping(mapping, ev.state.Output(), mergedInputs); err != nil {
							return nil, err
						}
					}
				}
			}
			return &PreconditionResult{Ready: true, Skipped: false, Inputs: mergedInputs}, nil
		}

		if allUnsatisfied {
			return &PreconditionResult{Ready: false, Skipped: true}, nil
		}

		return &PreconditionResult{Ready: false, Skipped: false}, nil
	}

	return nil, fmt.Errorf("unsupported join mode: %q", joinMode)
}
