package model

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gargi-indic/gargi-flow/internal/graph"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

func ValidateDecisions(m *CompiledModel, errs *engine.ValidationErrors) {
	var nodes []*engine.Node
	var edges []*engine.Edge
	if m.Graph != nil {
		for _, nDef := range m.Graph.Nodes {
			n, _ := engine.NewNode(nDef.ID, nDef.Type, nDef.Config)
			if n != nil {
				nodes = append(nodes, n)
			}
		}
		for _, eDef := range m.Graph.Edges {
			e, _ := engine.NewEdge(eDef.From, eDef.To, eDef.Condition, eDef.DataMapping)
			if e != nil {
				edges = append(edges, e)
			}
		}
	}
	g, _ := engine.NewGraph("tmp", nodes, edges)

	for i, node := range m.Graph.Nodes {
		if node.Type != "decision" {
			continue
		}

		path := engine.PathRoot.Field("nodes").Index(i).Field("config").Field("observes")

		observesList, _ := node.Config["observes"].([]any)
		for j, obsRaw := range observesList {
			obsPath := path.Index(j).String()
			obs, ok := obsRaw.(string)
			if !ok {
				errs.Add(obsPath, engine.CodeInvalidType, "observe path must be a string")
				continue
			}

			parts := strings.Split(obs, ".")
			if len(parts) == 0 {
				errs.Add(obsPath, sim.CodeInvalidObservePath, "empty observe path")
				continue
			}

			id := parts[0]
			if id == "" {
				errs.Add(obsPath, sim.CodeInvalidObservePath, "empty node id in observe path")
				continue
			}

			if len(parts) == 2 {
				if parts[1] != "status" {
					errs.Add(obsPath, sim.CodeInvalidObservePath, "invalid observe path format: if 2 segments, second must be 'status'")
					continue
				}
			} else if len(parts) == 3 {
				if parts[1] != "output" || parts[2] == "" {
					errs.Add(obsPath, sim.CodeInvalidObservePath, "invalid observe path format: if 3 segments, must be <id>.output.<field> and field cannot be empty")
					continue
				}
			} else {
				errs.Add(obsPath, sim.CodeInvalidObservePath, "invalid observe path format: must be <id>.status or <id>.output.<field>")
				continue
			}

			exists := false
			for _, n := range m.Graph.Nodes {
				if string(n.ID) == id {
					exists = true
					break
				}
			}

			if !exists {
				errs.Add(obsPath, engine.CodeUnresolvedReference, fmt.Sprintf("node %q does not exist", id))
				continue
			}

			if g != nil {
				isUpstream := false
				upstreamNodes, err := graph.Upstream(g, node.ID)
				if err == nil {
					for _, upNode := range upstreamNodes {
						if string(upNode) == id {
							isUpstream = true
							break
						}
					}
				}

				if !isUpstream {
					errs.Add(obsPath, sim.CodeObservesNonAncestor, fmt.Sprintf("observed node %q is not a strict ancestor of %q", id, node.ID))
				}
			}
		}
	}
}

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

		if durMap, ok := b.Duration.(map[string]any); ok {
			if durMap["normal"] != nil {
				reasons = append(reasons, fmt.Sprintf("%s: normal distribution is continuous", path.Field("duration").Field("normal").String()))
			}
			if durMap["lognormal"] != nil {
				reasons = append(reasons, fmt.Sprintf("%s: lognormal distribution is continuous", path.Field("duration").Field("lognormal").String()))
			}
			if durMap["uniform"] != nil {
				reasons = append(reasons, fmt.Sprintf("%s: uniform distribution is continuous", path.Field("duration").Field("uniform").String()))
			}
			if durMap["triangular"] != nil {
				reasons = append(reasons, fmt.Sprintf("%s: triangular distribution is continuous", path.Field("duration").Field("triangular").String()))
			}
			if emp, ok := durMap["empirical"].([]any); ok && len(emp) > 0 {
				reasons = append(reasons, fmt.Sprintf("%s: empirical distribution is continuous", path.Field("duration").Field("empirical").String()))
			}
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
