package kernel

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/gargi-indic/gargi-flow/internal/graph"
	"github.com/gargi-indic/gargi-flow/internal/reactive"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

// conditionCache holds compiled edge conditions keyed by node ID set and
// expression. CEL programs are safe for concurrent use, so the cache is
// shared by every episode.
var conditionCache sync.Map // map[string]*graph.CompiledCondition

// cachedConditionEvaluator evaluates edge conditions exactly like
// reactive.CELConditionEvaluator (same variables, same environment) but
// compiles each expression once instead of on every evaluation, which
// otherwise dominates the cost of an episode.
type cachedConditionEvaluator struct{}

var _ reactive.ConditionEvaluator = cachedConditionEvaluator{}

func (cachedConditionEvaluator) Evaluate(
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
			nodeMap := map[string]any{
				"status":       string(ns.Status()),
				"output":       ns.Output(),
				"errorMessage": ns.ErrorMessage(),
				"version":      ns.Version(),
			}
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

	sorted := append([]string(nil), nodeIDs...)
	sort.Strings(sorted)
	key := strings.Join(sorted, ",") + "\x00" + expression
	if c, ok := conditionCache.Load(key); ok {
		return c.(*graph.CompiledCondition).Evaluate(vars)
	}
	c, err := graph.CompileCondition(expression, nodeIDs...)
	if err != nil {
		return false, err
	}
	conditionCache.Store(key, c)
	return c.Evaluate(vars)
}
