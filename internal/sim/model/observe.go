package model

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gargi-indic/gargi-flow/internal/graph"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

// ValidateDecisions checks the observes list of every decision node. Each
// path must be exactly <id>.output.<field> or <id>.status
// (invalid_observe_path), name an existing node (unresolved_reference) and
// name a strict ancestor of the decision (observes_non_ancestor).
func ValidateDecisions(m *CompiledModel, errs *engine.ValidationErrors) {
	if m == nil || m.Graph == nil {
		return
	}
	nodes := make([]*engine.Node, 0, len(m.Graph.Nodes))
	exists := make(map[string]bool, len(m.Graph.Nodes))
	for _, nDef := range m.Graph.Nodes {
		exists[string(nDef.ID)] = true
		if n, err := engine.NewNode(nDef.ID, nDef.Type, nDef.Config); err == nil && n != nil {
			nodes = append(nodes, n)
		}
	}
	edges := make([]*engine.Edge, 0, len(m.Graph.Edges))
	for _, eDef := range m.Graph.Edges {
		if e, err := engine.NewEdge(eDef.From, eDef.To, eDef.Condition, eDef.DataMapping); err == nil && e != nil {
			edges = append(edges, e)
		}
	}
	// A graph that cannot be built has already been reported by graph
	// validation; the ancestor check is skipped in that case.
	g, gErr := engine.NewGraph("sim", nodes, edges)

	for i, node := range m.Graph.Nodes {
		if node.Type != "decision" {
			continue
		}

		var ancestors map[string]bool
		if gErr == nil && g != nil {
			if up, err := graph.Upstream(g, node.ID); err == nil {
				ancestors = make(map[string]bool, len(up))
				for _, u := range up {
					if u != node.ID {
						ancestors[string(u)] = true
					}
				}
			}
		}

		path := engine.Path("$.graph:" + engine.PathRoot.Field("nodes").Index(i).Field("config").Field("observes").String())
		observesList, _ := node.Config["observes"].([]any)
		for j, obsRaw := range observesList {
			obsPath := path.Index(j).String()
			obs, ok := obsRaw.(string)
			if !ok {
				errs.Add(obsPath, sim.CodeInvalidObservePath, "observe path must be a string")
				continue
			}

			id, ok := parseObservePath(obs)
			if !ok {
				errs.Add(obsPath, sim.CodeInvalidObservePath,
					fmt.Sprintf("invalid observe path %q: must be <id>.output.<field> or <id>.status", obs))
				continue
			}
			if !exists[id] {
				errs.Add(obsPath, engine.CodeUnresolvedReference, fmt.Sprintf("node %q does not exist", id))
				continue
			}
			if ancestors != nil && !ancestors[id] {
				errs.Add(obsPath, sim.CodeObservesNonAncestor, fmt.Sprintf("observed node %q is not a strict ancestor of %q", id, node.ID))
			}
		}
	}
}

// parseObservePath checks the observe-path grammar and returns the node id.
func parseObservePath(obs string) (string, bool) {
	parts := strings.Split(obs, ".")
	if parts[0] == "" {
		return "", false
	}
	switch len(parts) {
	case 2:
		return parts[0], parts[1] == "status"
	case 3:
		return parts[0], parts[1] == "output" && parts[2] != ""
	}
	return "", false
}

// Discrete reports whether the exact evaluator can run on the model. When it
// cannot, it returns the sorted reasons: continuous distributions, and expr
// outputs that feed an observed path.
func (m *CompiledModel) Discrete() (bool, []string) {
	var reasons []string

	var latentNames []string
	for name := range m.Def.Latents {
		latentNames = append(latentNames, name)
	}
	sort.Strings(latentNames)
	for _, name := range latentNames {
		l := m.Def.Latents[name]
		path := engine.PathRoot.Field("latents").Field(name)
		if l.Normal != nil {
			reasons = append(reasons, fmt.Sprintf("%s: normal distribution is continuous", path.Field("normal").String()))
		}
		if l.Lognormal != nil {
			reasons = append(reasons, fmt.Sprintf("%s: lognormal distribution is continuous", path.Field("lognormal").String()))
		}
		if l.Uniform != nil {
			reasons = append(reasons, fmt.Sprintf("%s: uniform distribution is continuous", path.Field("uniform").String()))
		}
		if l.Triangular != nil {
			reasons = append(reasons, fmt.Sprintf("%s: triangular distribution is continuous", path.Field("triangular").String()))
		}
		if len(l.Empirical) > 0 {
			reasons = append(reasons, fmt.Sprintf("%s: empirical distribution is continuous", path.Field("empirical").String()))
		}
	}

	checkValueSpec := func(spec ValueSpec, p engine.Path) {
		if spec.Normal != nil {
			reasons = append(reasons, fmt.Sprintf("%s: normal distribution is continuous", p.Field("normal").String()))
		}
		if spec.Lognormal != nil {
			reasons = append(reasons, fmt.Sprintf("%s: lognormal distribution is continuous", p.Field("lognormal").String()))
		}
		if spec.Uniform != nil {
			reasons = append(reasons, fmt.Sprintf("%s: uniform distribution is continuous", p.Field("uniform").String()))
		}
		if spec.Triangular != nil {
			reasons = append(reasons, fmt.Sprintf("%s: triangular distribution is continuous", p.Field("triangular").String()))
		}
		if len(spec.Empirical) > 0 {
			reasons = append(reasons, fmt.Sprintf("%s: empirical distribution is continuous", p.Field("empirical").String()))
		}
	}

	var bhvKeys []string
	for id := range m.Def.Behaviors {
		bhvKeys = append(bhvKeys, id)
	}
	sort.Strings(bhvKeys)
	for _, id := range bhvKeys {
		b := m.Def.Behaviors[id]
		path := engine.PathRoot.Field("behaviors").Field(id)

		// The string form ("48h") is a fixed duration and stays discrete; the
		// value-spec form is bound by the parser as a ValueSpec.
		if vs, ok := b.Duration.(ValueSpec); ok {
			checkValueSpec(vs, path.Field("duration"))
		}

		var outNames []string
		for outName := range b.Output {
			outNames = append(outNames, outName)
		}
		sort.Strings(outNames)
		for _, outName := range outNames {
			spec := b.Output[outName]
			outPath := path.Field("output").Field(outName)
			checkValueSpec(spec, outPath)
			if spec.Expr != "" {
				isObserved := false
				for _, n := range m.Graph.Nodes {
					if n.Type == "decision" {
						if obsRaw, ok := n.Config["observes"].([]any); ok {
							for _, obs := range obsRaw {
								if obsStr, ok := obs.(string); ok {
									if obsStr == fmt.Sprintf("%s.output.%s", id, outName) {
										isObserved = true
										break
									}
								}
							}
						}
					}
				}
				if isObserved {
					reasons = append(reasons, fmt.Sprintf("%s: expr output feeds an observed path", outPath.Field("expr").String()))
				}
			}
		}
	}

	if len(reasons) > 0 {
		sort.Strings(reasons)
		return false, reasons
	}
	return true, nil
}

func (m *CompiledModel) observedSupportHelper(keys []string) []any {
	sort.Strings(keys)
	res := make([]any, len(keys))
	for i, k := range keys {
		res[i] = k
	}
	return res
}

// ObservedSupport returns the discrete support of an observed path, taken from
// choice, bernoulli and fixed behavior outputs, the options of upstream
// decision nodes, and node statuses. It reports false when the support is not
// known to be discrete.
func (m *CompiledModel) ObservedSupport(path string) ([]any, bool) {
	parts := strings.Split(path, ".")
	if len(parts) < 2 {
		return nil, false
	}

	id := parts[0]
	if parts[1] == "status" {
		return []any{"Skipped", "Succeeded"}, true
	}

	if len(parts) == 3 && parts[1] == "output" {
		field := parts[2]

		for _, node := range m.Graph.Nodes {
			if string(node.ID) == id && node.Type == "decision" && field == "choice" {
				if opts, ok := node.Config["options"].([]any); ok {
					var res []any
					for _, opt := range opts {
						if valMap, ok := opt.(map[string]any); ok {
							res = append(res, valMap["value"])
						} else {
							res = append(res, opt)
						}
					}
					return res, true
				}
			}
		}

		if b, ok := m.Def.Behaviors[id]; ok {
			if out, ok := b.Output[field]; ok {
				if out.Choice != nil {
					var keys []string
					for k := range out.Choice {
						keys = append(keys, k)
					}
					return m.observedSupportHelper(keys), true
				}
				if out.Bernoulli != nil {
					return []any{false, true}, true
				}
				if out.Fixed != nil {
					return []any{out.Fixed}, true
				}
			}
		}
	}

	return nil, false
}
