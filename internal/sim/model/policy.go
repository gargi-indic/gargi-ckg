package model

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/ast"
)

// runtimePolicy implements sim.Policy interface.
type runtimePolicy struct {
	id          string
	description string
	decisions   map[string]*compiledDecision
}

type compiledDecision struct {
	Options []string
	Rules   []compiledRule
	Table   []compiledTableRow
	Default string
	IsTable bool
}

type compiledRule struct {
	When   cel.Program
	Choose string
	Else   string
}

type compiledTableRow struct {
	Observed map[string]any
	Choose   string
}

func (p *runtimePolicy) ID() string {
	return p.id
}

func (p *runtimePolicy) Describe() string {
	return p.description
}

func (p *runtimePolicy) Choose(ctx context.Context, decision engine.NodeID, obs sim.Observation, _ []string) (string, error) {
	nodeID := string(decision)
	d, ok := p.decisions[nodeID]
	if !ok {
		return "", fmt.Errorf("no policy for decision %q", nodeID)
	}

	info := make(map[string]any)
	for k, v := range obs {
		parts := strings.Split(k, ".")
		current := info
		for i, part := range parts {
			if i == len(parts)-1 {
				current[part] = v
			} else {
				if _, exists := current[part]; !exists {
					current[part] = make(map[string]any)
				}
				if next, ok := current[part].(map[string]any); ok {
					current = next
				}
			}
		}
	}

	if d.IsTable {
		for _, row := range d.Table {
			match := true
			for k, v := range row.Observed {
				if infoVal, exists := obs[k]; exists {
					// Normalize numeric types to float64
					normInfoVal := infoVal
					normV := v

					if f, ok := infoVal.(float64); ok {
						normInfoVal = f
					} else if f, ok := infoVal.(float32); ok {
						normInfoVal = float64(f)
					} else if i, ok := infoVal.(int); ok {
						normInfoVal = float64(i)
					} else if i, ok := infoVal.(int64); ok {
						normInfoVal = float64(i)
					} else if i, ok := infoVal.(int32); ok {
						normInfoVal = float64(i)
					}

					if f, ok := v.(float64); ok {
						normV = f
					} else if f, ok := v.(float32); ok {
						normV = float64(f)
					} else if i, ok := v.(int); ok {
						normV = float64(i)
					} else if i, ok := v.(int64); ok {
						normV = float64(i)
					} else if i, ok := v.(int32); ok {
						normV = float64(i)
					}

					if normInfoVal != normV {
						match = false
						break
					}
				} else if v != nil {
					match = false
					break
				}
			}
			if match {
				return row.Choose, nil
			}
		}
		if d.Default != "" {
			return d.Default, nil
		}
		return "", fmt.Errorf("no matching table row and no default for %q", nodeID)
	}

	for _, rule := range d.Rules {
		if rule.When != nil {
			out, _, err := rule.When.ContextEval(ctx, info)
			if err != nil {
				return "", fmt.Errorf("failed to evaluate when: %w", err)
			}
			if val, ok := out.Value().(bool); ok && val {
				return rule.Choose, nil
			}
		}
		if rule.Else != "" {
			return rule.Else, nil
		}
	}
	return "", fmt.Errorf("no matching rule for %q", nodeID)
}

func getSelectPath(expr ast.Expr) string {
	if expr.Kind() == ast.IdentKind {
		return expr.AsIdent()
	}
	if expr.Kind() == ast.SelectKind {
		sel := expr.AsSelect()
		operandPath := getSelectPath(sel.Operand())
		if operandPath == "" {
			return ""
		}
		return operandPath + "." + sel.FieldName()
	}
	return ""
}

// CompilePolicy compiles the definitions rules for dynamic decision evaluations into an executable policy.
func CompilePolicy(p *Policy, m *CompiledModel) (sim.Policy, *engine.ValidationErrors) {
	errs := &engine.ValidationErrors{}
	if p == nil {
		return nil, nil
	}

	rp := &runtimePolicy{
		id:          fmt.Sprintf("%s@%s", p.ID, p.Version),
		description: fmt.Sprintf("Policy %s version %s", p.ID, p.Version),
		decisions:   make(map[string]*compiledDecision),
	}

	decisionNodes := make(map[string]*engine.NodeDefinition)
	for i := range m.Graph.Nodes {
		n := &m.Graph.Nodes[i]
		if n.Type == "decision" {
			decisionNodes[string(n.ID)] = n
		}
	}

	var dIDs []string
	for dID := range decisionNodes {
		dIDs = append(dIDs, dID)
	}
	sort.Strings(dIDs)
	for _, dID := range dIDs {
		if _, ok := p.Decisions[dID]; !ok {
			errs.Add(engine.PathRoot.Field("decisions").String(), sim.CodePolicyMissingDecision, fmt.Sprintf("missing decision for node %q", dID))
		}
	}

	var decisionIDs []string
	for id := range p.Decisions {
		decisionIDs = append(decisionIDs, id)
	}
	sort.Strings(decisionIDs)
	for _, id := range decisionIDs {
		pd := p.Decisions[id]
		path := engine.PathRoot.Field("decisions").Field(id)
		node, ok := decisionNodes[id]
		if !ok {
			errs.Add(path.String(), sim.CodePolicyUnknownDecision, fmt.Sprintf("unknown decision node %q", id))
			continue
		}

		optsRaw, _ := node.Config["options"].([]any)
		options := make(map[string]bool)
		for _, o := range optsRaw {
			if om, ok := o.(map[string]any); ok {
				if v, ok := om["value"].(string); ok {
					options[v] = true
				}
			} else if s, ok := o.(string); ok {
				options[s] = true
			}
		}

		observesRaw, _ := node.Config["observes"].([]any)
		observedPaths := make(map[string]bool)
		observedIDs := make(map[string]bool)
		for _, obs := range observesRaw {
			if s, ok := obs.(string); ok {
				observedPaths[s] = true
				parts := strings.Split(s, ".")
				if len(parts) > 0 {
					observedIDs[parts[0]] = true
				}
			}
		}

		cd := &compiledDecision{
			IsTable: len(pd.Table) > 0,
		}

		if cd.IsTable {
			if pd.Default != "" && !options[pd.Default] {
				errs.Add(path.Field("default").String(), sim.CodePolicyUnknownOption, fmt.Sprintf("unknown option %q", pd.Default))
			}
			cd.Default = pd.Default

			for i, row := range pd.Table {
				rowPath := path.Field("table").Index(i)
				if !options[row.Choose] {
					errs.Add(rowPath.Field("choose").String(), sim.CodePolicyUnknownOption, fmt.Sprintf("unknown option %q", row.Choose))
				}

				var obsKeys []string
				for k := range row.Observed {
					obsKeys = append(obsKeys, k)
				}
				sort.Strings(obsKeys)
				for _, k := range obsKeys {
					if !observedPaths[k] {
						errs.Add(rowPath.Field("observed").Field(k).String(), sim.CodePolicyUnobservedReference, fmt.Sprintf("unobserved reference %q", k))
					}
				}
				cd.Table = append(cd.Table, compiledTableRow(row))
			}
		} else {
			var oIDs []string
			for oID := range observedIDs {
				oIDs = append(oIDs, oID)
			}
			sort.Strings(oIDs)
			envOpts := []cel.EnvOption{}
			for _, oID := range oIDs {
				envOpts = append(envOpts, cel.Variable(oID, cel.MapType(cel.StringType, cel.DynType)))
			}

			env, err := cel.NewEnv(envOpts...)
			if err != nil {
				errs.Add(path.String(), engine.CodeInvalidValue, "failed to create env for decision")
				continue
			}

			for i, rule := range pd.Rules {
				rulePath := path.Field("rules").Index(i)
				cr := compiledRule{}

				if rule.When != "" {
					celAst, issues := env.Compile(rule.When)
					if issues != nil && issues.Err() != nil {
						errs.Add(rulePath.Field("when").String(), sim.CodeInvalidExpression, issues.Err().Error())
					} else {
						walkNativeAST(env, celAst, observedPaths, rulePath.Field("when").String(), errs)
					}

					if celAst != nil {
						prog, err := env.Program(celAst, cel.CostLimit(1000), cel.EvalOptions(cel.OptTrackCost))
						if err != nil {
							errs.Add(rulePath.Field("when").String(), sim.CodeInvalidExpression, err.Error())
						} else {
							cr.When = prog
						}
					}
				}

				if rule.Choose != "" {
					if !options[rule.Choose] {
						errs.Add(rulePath.Field("choose").String(), sim.CodePolicyUnknownOption, fmt.Sprintf("unknown option %q", rule.Choose))
					}
					cr.Choose = rule.Choose
				}
				if rule.Else != "" {
					if !options[rule.Else] {
						errs.Add(rulePath.Field("else").String(), sim.CodePolicyUnknownOption, fmt.Sprintf("unknown option %q", rule.Else))
					}
					cr.Else = rule.Else
				}

				cd.Rules = append(cd.Rules, cr)
			}
		}
		rp.decisions[id] = cd
	}

	if errs.Len() > 0 {
		return nil, errs
	}
	return rp, nil
}

func walkNativeAST(_ *cel.Env, celAst *cel.Ast, allowed map[string]bool, path string, errs *engine.ValidationErrors) {
	if celAst == nil {
		return
	}
	expr := celAst.NativeRep().Expr()
	walkAST(expr, allowed, path, errs)
}

func walkAST(expr ast.Expr, allowed map[string]bool, path string, errs *engine.ValidationErrors) {
	if expr == nil {
		return
	}

	switch expr.Kind() {
	case ast.SelectKind:
		p := getSelectPath(expr)
		if p != "" {
			valid := false
			for a := range allowed {
				if p == a || strings.HasPrefix(p, a+".") {
					valid = true
					break
				}
			}
			if !valid {
				errs.Add(path, sim.CodePolicyUnobservedReference, fmt.Sprintf("unobserved reference: %s", p))
			}
		} else {
			errs.Add(path, sim.CodePolicyUnobservedReference, "complex select or index access not allowed")
		}
	case ast.IdentKind:
		ident := expr.AsIdent()
		valid := false
		for a := range allowed {
			if ident == a || strings.HasPrefix(ident, a+".") {
				valid = true
				break
			}
		}
		if !valid {
			errs.Add(path, sim.CodePolicyUnobservedReference, fmt.Sprintf("unobserved reference: %s", ident))
		}
	case ast.CallKind:
		call := expr.AsCall()
		if call.Target() != nil {
			walkAST(call.Target(), allowed, path, errs)
		}
		for _, arg := range call.Args() {
			walkAST(arg, allowed, path, errs)
		}
	case ast.ListKind:
		list := expr.AsList()
		for _, e := range list.Elements() {
			walkAST(e, allowed, path, errs)
		}
	case ast.MapKind:
		m := expr.AsMap()
		for _, e := range m.Entries() {
			me := e.AsMapEntry()
			walkAST(me.Key(), allowed, path, errs)
			walkAST(me.Value(), allowed, path, errs)
		}
	case ast.ComprehensionKind:
		comp := expr.AsComprehension()
		walkAST(comp.IterRange(), allowed, path, errs)
		walkAST(comp.AccuInit(), allowed, path, errs)
		walkAST(comp.LoopCondition(), allowed, path, errs)
		walkAST(comp.LoopStep(), allowed, path, errs)
		walkAST(comp.Result(), allowed, path, errs)
	}
}
