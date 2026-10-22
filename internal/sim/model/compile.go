package model

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gargi-indic/gargi-flow/internal/graph"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/google/cel-go/cel"
)

// CompiledModel represents a validated and compiled simulation model.
type CompiledModel struct {
	Def      *Sim
	Graph    *engine.GraphDefinition
	Registry *engine.Registry

	CompiledLatents     map[string]cel.Program
	CompiledObjective   cel.Program
	CompiledConstraints map[string]cel.Program
	CompiledBehaviors   map[string]CompiledBehavior
}

// CompiledBehavior represents the evaluated conditions governing the execution constraints of a behavior action block.
// CompiledBehavior represents the evaluated conditions governing the execution constraints of a behavior action block.
type CompiledBehavior struct {
	Cost     cel.Program
	AbandonP cel.Program
	FailP    cel.Program
	Outputs  map[string]cel.Program
}

const conditionCostLimit uint64 = 1000

func compileCEL(expr string, path string, env *cel.Env, expectedTypes []*cel.Type, errs *engine.ValidationErrors) cel.Program {
	if expr == "" {
		return nil
	}
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		errs.Add(path, sim.CodeInvalidExpression, fmt.Sprintf("CEL compile error: %v", issues.Err()))
		return nil
	}

	typeValid := false
	outType := ast.OutputType()
	for _, t := range expectedTypes {
		if outType.IsExactType(t) || outType.IsExactType(cel.DynType) {
			typeValid = true
			break
		}
	}
	if !typeValid {
		errs.Add(path, sim.CodeInvalidExpression, fmt.Sprintf("invalid output type: got %v", outType))
		return nil
	}

	prog, err := env.Program(ast, cel.CostLimit(conditionCostLimit), cel.EvalOptions(cel.OptTrackCost))
	if err != nil {
		errs.Add(path, sim.CodeInvalidExpression, fmt.Sprintf("CEL program error: %v", err))
		return nil
	}

	return prog
}

// Compile loads and validates a simulation definition and its associated graph.
func Compile(def *Sim, baseDir string, reg *engine.Registry) (*CompiledModel, *engine.ValidationErrors) {
	errs := &engine.ValidationErrors{}
	if def == nil {
		return nil, nil
	}

	graphPath := filepath.Join(baseDir, def.Graph)
	graphData, err := os.ReadFile(graphPath)
	if err != nil {
		errs.Add("$.graph", engine.CodeUnresolvedReference, fmt.Sprintf("failed to read graph file: %v", err))
		return nil, errs
	}

	format := engine.FormatJSON
	if strings.HasSuffix(def.Graph, ".yaml") || strings.HasSuffix(def.Graph, ".yml") {
		format = engine.FormatYAML
	}

	gDef, parseErr := graph.NewParser().Parse(graphData, format)
	if parseErr != nil {
		if ve, ok := parseErr.(*engine.ValidationErrors); ok && ve != nil {
			for _, issue := range ve.Issues() {
				errs.Add("$.graph:"+issue.Path, issue.Code, issue.Message)
			}
		} else {
			errs.Add("$.graph", engine.CodeInvalidFormat, parseErr.Error())
		}
		return nil, errs
	}

	if gErrs := graph.ValidateStructure(gDef); gErrs != nil && gErrs.Len() > 0 {
		for _, issue := range gErrs.Issues() {
			errs.Add("$.graph:"+issue.Path, issue.Code, issue.Message)
		}
	}

	if gErrs := graph.ValidateSemantics(gDef, reg); gErrs != nil && gErrs.Len() > 0 {
		for _, issue := range gErrs.Issues() {
			errs.Add("$.graph:"+issue.Path, issue.Code, issue.Message)
		}
	}

	nodeExists := make(map[string]bool)
	effectfulNodes := make(map[string]bool)
	for _, node := range gDef.Nodes {
		nodeExists[string(node.ID)] = true
		if node.Type == "http_call" || node.Type == "wait_for_signal" || node.Type == "sub_graph" {
			effectfulNodes[string(node.ID)] = true
		}
	}

	var bhvKeys []string
	for id := range def.Behaviors {
		bhvKeys = append(bhvKeys, id)
	}
	sort.Strings(bhvKeys)
	for _, id := range bhvKeys {
		if !nodeExists[id] {
			errs.Add(engine.PathRoot.Field("behaviors").Field(id).String(), sim.CodeBehaviorUnknownNode, "behavior for unknown node")
		}
	}

	var effKeys []string
	for id := range effectfulNodes {
		effKeys = append(effKeys, id)
	}
	sort.Strings(effKeys)
	for _, id := range effKeys {
		if _, ok := def.Behaviors[id]; !ok {
			errs.Add(engine.PathRoot.Field("behaviors").Field(id).String(), sim.CodeBehaviorMissing, "effectful node requires a behavior")
		}
	}

	latentEnvOpts := []cel.EnvOption{
		cel.Variable("world", cel.MapType(cel.StringType, cel.DynType)),
	}
	latentEnv, err := cel.NewEnv(latentEnvOpts...)
	if err != nil {
		errs.Add(engine.PathRoot.String(), engine.CodeInvalidValue, "failed to create latent env")
		return nil, errs
	}

	behaviorEnvOpts := []cel.EnvOption{
		cel.Variable("world", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("latent", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("inputs", cel.MapType(cel.StringType, cel.DynType)),
	}
	behaviorEnv, err := cel.NewEnv(behaviorEnvOpts...)
	if err != nil {
		errs.Add(engine.PathRoot.String(), engine.CodeInvalidValue, "failed to create behavior env")
		return nil, errs
	}

	objectiveEnvOpts := []cel.EnvOption{
		cel.Variable("world", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("latent", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("nodes", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("run", cel.MapType(cel.StringType, cel.DynType)),
	}
	objectiveEnv, err := cel.NewEnv(objectiveEnvOpts...)
	if err != nil {
		errs.Add(engine.PathRoot.String(), engine.CodeInvalidValue, "failed to create objective env")
		return nil, errs
	}

	m := &CompiledModel{
		Def:                 def,
		Graph:               gDef,
		Registry:            reg,
		CompiledLatents:     make(map[string]cel.Program),
		CompiledConstraints: make(map[string]cel.Program),
		CompiledBehaviors:   make(map[string]CompiledBehavior),
	}

	var latentNames []string
	for name := range def.Latents {
		latentNames = append(latentNames, name)
	}
	sort.Strings(latentNames)
	for _, name := range latentNames {
		l := def.Latents[name]
		if l.Expr != "" {
			path := engine.PathRoot.Field("latents").Field(name).Field("expr").String()
			prog := compileCEL(l.Expr, path, latentEnv, []*cel.Type{cel.DynType, cel.DoubleType, cel.IntType}, errs)
			if prog != nil {
				m.CompiledLatents[name] = prog
			}
		}
	}

	if def.Objective.Value != "" {
		path := engine.PathRoot.Field("objective").Field("value").String()
		prog := compileCEL(def.Objective.Value, path, objectiveEnv, []*cel.Type{cel.DoubleType, cel.IntType, cel.DynType}, errs)
		if prog != nil {
			m.CompiledObjective = prog
		}
	}

	var constraintNames []string
	for name := range def.Objective.Constraints {
		constraintNames = append(constraintNames, name)
	}
	sort.Strings(constraintNames)
	for _, name := range constraintNames {
		constraint := def.Objective.Constraints[name]
		path := engine.PathRoot.Field("objective").Field("constraints").Field(name).String()
		prog := compileCEL(constraint, path, objectiveEnv, []*cel.Type{cel.BoolType, cel.DynType}, errs)
		if prog != nil {
			m.CompiledConstraints[name] = prog
		}
	}

	compileValueSpec := func(spec ValueSpec, fieldPath string, env *cel.Env) cel.Program {
		if spec.Expr != "" {
			return compileCEL(spec.Expr, fieldPath+".expr", env, []*cel.Type{cel.DoubleType, cel.IntType, cel.DynType}, errs)
		}
		return nil
	}

	compileAnyExpr := func(val any, fieldPath string, env *cel.Env) cel.Program {
		if s, ok := val.(string); ok && s != "" {
			return compileCEL(s, fieldPath, env, []*cel.Type{cel.DoubleType, cel.IntType, cel.DynType}, errs)
		}
		return nil
	}

	var behaviorIDs []string
	for id := range def.Behaviors {
		behaviorIDs = append(behaviorIDs, id)
	}
	sort.Strings(behaviorIDs)
	for _, id := range behaviorIDs {
		b := def.Behaviors[id]
		cb := CompiledBehavior{
			Outputs: make(map[string]cel.Program),
		}
		basePath := engine.PathRoot.Field("behaviors").Field(id)

		if b.Cost != nil {
			cb.Cost = compileAnyExpr(b.Cost, basePath.Field("cost").String(), behaviorEnv)
		}
		if b.Abandon != nil && b.Abandon.P != nil {
			cb.AbandonP = compileAnyExpr(b.Abandon.P, basePath.Field("abandon").Field("p").String(), behaviorEnv)
		}
		if b.Fail != nil && b.Fail.P != nil {
			cb.FailP = compileAnyExpr(b.Fail.P, basePath.Field("fail").Field("p").String(), behaviorEnv)
		}

		var outNames []string
		for outName := range b.Output {
			outNames = append(outNames, outName)
		}
		sort.Strings(outNames)
		for _, outName := range outNames {
			outSpec := b.Output[outName]
			p := compileValueSpec(outSpec, basePath.Field("output").Field(outName).String(), behaviorEnv)
			if p != nil {
				cb.Outputs[outName] = p
			}
		}

		m.CompiledBehaviors[id] = cb
	}

	ValidateDecisions(m, errs)
	if errs.Len() > 0 {
		return nil, errs
	}

	return m, nil
}
