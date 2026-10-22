package model

import (
	"errors"
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

// effectfulNodeTypes lists the node types whose execution is replaced by a
// behavior during simulation. Every node of one of these types must have a
// behavior in the sim/v1 document.
var effectfulNodeTypes = map[string]bool{
	"http_call":       true,
	"wait_for_signal": true,
	"sub_graph":       true,
}

// ValueKind identifies which form of a value spec a CompiledValue holds.
type ValueKind string

// Value spec kinds. Exactly one is set per CompiledValue.
const (
	KindFixed      ValueKind = "fixed"
	KindChoice     ValueKind = "choice"
	KindBernoulli  ValueKind = "bernoulli"
	KindNormal     ValueKind = "normal"
	KindLognormal  ValueKind = "lognormal"
	KindUniform    ValueKind = "uniform"
	KindTriangular ValueKind = "triangular"
	KindEmpirical  ValueKind = "empirical"
	KindExpr       ValueKind = "expr"
)

// Distribution parameter names used as keys of CompiledValue.Params.
const (
	ParamP     = "p"     // bernoulli
	ParamMean  = "mean"  // normal
	ParamSD    = "sd"    // normal
	ParamMu    = "mu"    // lognormal
	ParamSigma = "sigma" // lognormal
	ParamMin   = "min"   // uniform, triangular
	ParamMax   = "max"   // uniform, triangular
	ParamMode  = "mode"  // triangular
)

// CompiledParam is one numeric parameter of a value spec or behavior. A
// parameter written as a number in the document is a literal: Program is nil
// and Literal holds the value. A parameter written as a string is a CEL
// expression: Program holds the checked program (its output type is double,
// int or dyn) and Source holds the original text. Evaluators call IsLiteral
// to decide whether to evaluate Program against an activation.
type CompiledParam struct {
	Source  string
	Program cel.Program
	Literal float64
}

// IsLiteral reports whether the parameter is a numeric literal.
func (p CompiledParam) IsLiteral() bool { return p.Program == nil }

// CompiledChoiceOption is one outcome of a choice value spec and its
// (unnormalized) weight.
type CompiledChoiceOption struct {
	Value  string
	Weight CompiledParam
}

// CompiledValue is a value spec whose CEL parameters have been compiled once.
//
// The fields used depend on Kind:
//   - KindFixed: Fixed holds the literal value as written (number or string,
//     for example "48h" for a duration). Fixed values are never CEL.
//   - KindChoice: Choice lists the options sorted by Value, each with its
//     weight.
//   - KindBernoulli, KindNormal, KindLognormal, KindUniform, KindTriangular:
//     Params holds every parameter of the distribution keyed by the Param*
//     constants (for example Params[ParamMean] and Params[ParamSD]).
//   - KindEmpirical: Empirical holds the samples.
//   - KindExpr: Expr holds the compiled expression.
type CompiledValue struct {
	Kind      ValueKind
	Fixed     any
	Choice    []CompiledChoiceOption
	Params    map[string]CompiledParam
	Empirical []float64
	Expr      cel.Program
}

// CompiledBehavior is a behavior whose CEL parameters have been compiled
// against the behavior environment (world, latent, inputs). Optional parts
// that are absent in the document are nil.
type CompiledBehavior struct {
	// Cost is the cost charged when the node runs.
	Cost *CompiledParam
	// Duration is the node's duration. The string form ("48h") is stored as
	// a KindFixed value holding the original string, so both `duration: 48h`
	// and `duration: {fixed: 48h}` look the same to the evaluator.
	Duration *CompiledValue
	// AbandonP is the probability that the node is abandoned.
	AbandonP *CompiledParam
	// FailP is the probability that the node fails.
	FailP *CompiledParam
	// Outputs maps each output field to its compiled value spec.
	Outputs map[string]CompiledValue
}

// CompiledModel is a validated sim/v1 model with every CEL program compiled
// and type-checked once. It is immutable after Compile returns and safe to
// share between goroutines.
type CompiledModel struct {
	Def      *Sim
	Graph    *engine.GraphDefinition
	Registry *engine.Registry

	// LatentOrder lists latent names in the order they are sampled (sorted).
	LatentOrder []string
	// CompiledLatents maps latent name to its value spec, compiled in the
	// latent environment (world only).
	CompiledLatents map[string]CompiledValue
	// CompiledObjective is the objective program (world, latent, nodes, run).
	CompiledObjective cel.Program
	// CompiledConstraints maps constraint name to its bool program.
	CompiledConstraints map[string]cel.Program
	// CompiledBehaviors maps node id to its compiled behavior.
	CompiledBehaviors map[string]CompiledBehavior
}

var (
	numericTypes = []*cel.Type{cel.DoubleType, cel.IntType, cel.DynType}
	boolTypes    = []*cel.Type{cel.BoolType, cel.DynType}
)

func mapVar(name string) cel.EnvOption {
	return cel.Variable(name, cel.MapType(cel.StringType, cel.DynType))
}

func newEnv(names ...string) (*cel.Env, error) {
	opts := make([]cel.EnvOption, 0, len(names))
	for _, n := range names {
		opts = append(opts, mapVar(n))
	}
	return cel.NewEnv(opts...)
}

// compileCEL compiles and type-checks expr in env. It reports problems as
// invalid_expression at path and returns nil on failure.
func compileCEL(expr, path string, env *cel.Env, expectedTypes []*cel.Type, errs *engine.ValidationErrors) cel.Program {
	if strings.TrimSpace(expr) == "" {
		errs.Add(path, sim.CodeInvalidExpression, "expression must not be empty")
		return nil
	}
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		errs.Add(path, sim.CodeInvalidExpression, fmt.Sprintf("CEL compile error: %v", issues.Err()))
		return nil
	}

	outType := ast.OutputType()
	typeValid := false
	for _, t := range expectedTypes {
		if outType.IsExactType(t) {
			typeValid = true
			break
		}
	}
	if !typeValid {
		errs.Add(path, sim.CodeInvalidExpression, fmt.Sprintf("invalid output type: got %v", outType))
		return nil
	}

	prog, err := env.Program(ast, cel.CostLimit(graph.DefaultConditionCostLimit), cel.EvalOptions(cel.OptTrackCost))
	if err != nil {
		errs.Add(path, sim.CodeInvalidExpression, fmt.Sprintf("CEL program error: %v", err))
		return nil
	}
	return prog
}

// compileParam compiles one number-or-CEL parameter.
func compileParam(val any, path string, env *cel.Env, errs *engine.ValidationErrors) CompiledParam {
	switch v := val.(type) {
	case string:
		return CompiledParam{Source: v, Program: compileCEL(v, path, env, numericTypes, errs)}
	case float64:
		return CompiledParam{Literal: v}
	case int:
		return CompiledParam{Literal: float64(v)}
	case int64:
		return CompiledParam{Literal: float64(v)}
	case nil:
		errs.Add(path, engine.CodeMissingRequired, "missing required parameter")
	default:
		errs.Add(path, engine.CodeInvalidType, "expected number or string")
	}
	return CompiledParam{}
}

func compileParamPtr(val any, path string, env *cel.Env, errs *engine.ValidationErrors) *CompiledParam {
	p := compileParam(val, path, env, errs)
	return &p
}

// compileValue compiles every CEL parameter of spec in env.
func compileValue(spec ValueSpec, path engine.Path, env *cel.Env, errs *engine.ValidationErrors) CompiledValue {
	params := func(kind ValueKind, names []string, vals []any) CompiledValue {
		cv := CompiledValue{Kind: kind, Params: make(map[string]CompiledParam, len(names))}
		base := path.Field(string(kind))
		for i, n := range names {
			cv.Params[n] = compileParam(vals[i], base.Field(n).String(), env, errs)
		}
		return cv
	}

	switch {
	case spec.Fixed != nil:
		return CompiledValue{Kind: KindFixed, Fixed: spec.Fixed}
	case spec.Choice != nil:
		keys := make([]string, 0, len(spec.Choice))
		for k := range spec.Choice {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		cv := CompiledValue{Kind: KindChoice, Choice: make([]CompiledChoiceOption, 0, len(keys))}
		base := path.Field(string(KindChoice))
		for _, k := range keys {
			cv.Choice = append(cv.Choice, CompiledChoiceOption{
				Value:  k,
				Weight: compileParam(spec.Choice[k], base.Field(k).String(), env, errs),
			})
		}
		return cv
	case spec.Bernoulli != nil:
		return params(KindBernoulli, []string{ParamP}, []any{spec.Bernoulli.P})
	case spec.Normal != nil:
		return params(KindNormal, []string{ParamMean, ParamSD}, []any{spec.Normal.Mean, spec.Normal.SD})
	case spec.Lognormal != nil:
		return params(KindLognormal, []string{ParamMu, ParamSigma}, []any{spec.Lognormal.Mu, spec.Lognormal.Sigma})
	case spec.Uniform != nil:
		return params(KindUniform, []string{ParamMin, ParamMax}, []any{spec.Uniform.Min, spec.Uniform.Max})
	case spec.Triangular != nil:
		return params(KindTriangular, []string{ParamMin, ParamMode, ParamMax},
			[]any{spec.Triangular.Min, spec.Triangular.Mode, spec.Triangular.Max})
	case spec.Empirical != nil:
		return CompiledValue{Kind: KindEmpirical, Empirical: spec.Empirical}
	case spec.Expr != "":
		return CompiledValue{
			Kind: KindExpr,
			Expr: compileCEL(spec.Expr, path.Field(string(KindExpr)).String(), env, numericTypes, errs),
		}
	}
	errs.Add(path.String(), engine.CodeInvalidValue, "value spec must have exactly one key")
	return CompiledValue{}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedIssues returns errs with its issues sorted by path, code and message,
// so compilation output is deterministic.
func sortedIssues(errs *engine.ValidationErrors) *engine.ValidationErrors {
	issues := errs.Issues()
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Path != issues[j].Path {
			return issues[i].Path < issues[j].Path
		}
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		return issues[i].Message < issues[j].Message
	})
	out := &engine.ValidationErrors{}
	for _, is := range issues {
		out.AddIssue(is)
	}
	return out
}

func addGraphIssues(errs, src *engine.ValidationErrors) {
	if src == nil {
		return
	}
	for _, issue := range src.Issues() {
		errs.Add("$.graph:"+issue.Path, issue.Code, issue.Message)
	}
}

// loadGraph reads, parses and validates the graph referenced by def.
func loadGraph(def *Sim, baseDir string, reg *engine.Registry, errs *engine.ValidationErrors) *engine.GraphDefinition {
	graphPath := def.Graph
	if !filepath.IsAbs(graphPath) {
		graphPath = filepath.Join(baseDir, graphPath)
	}

	var format engine.Format
	switch strings.ToLower(filepath.Ext(graphPath)) {
	case ".yaml", ".yml":
		format = engine.FormatYAML
	case ".json":
		format = engine.FormatJSON
	default:
		errs.Add("$.graph", engine.CodeInvalidFormat, fmt.Sprintf("unsupported graph file extension %q", filepath.Ext(graphPath)))
		return nil
	}

	data, err := os.ReadFile(graphPath)
	if err != nil {
		errs.Add("$.graph", engine.CodeUnresolvedReference, fmt.Sprintf("failed to read graph file: %v", err))
		return nil
	}

	gDef, err := graph.NewParser().Parse(data, format)
	if err != nil {
		var ve *engine.ValidationErrors
		if errors.As(err, &ve) && ve != nil {
			addGraphIssues(errs, ve)
		} else {
			errs.Add("$.graph", engine.CodeInvalidFormat, err.Error())
		}
		return nil
	}

	addGraphIssues(errs, graph.ValidateStructure(gDef))
	addGraphIssues(errs, graph.ValidateSemantics(gDef, reg))
	return gDef
}

// Compile loads the graph referenced by def (relative to baseDir), validates
// it against reg, checks behaviors and decision observe paths, and compiles
// every CEL expression of the model. It returns nil errors on success; on
// failure it returns every problem found, sorted.
func Compile(def *Sim, baseDir string, reg *engine.Registry) (*CompiledModel, *engine.ValidationErrors) {
	errs := &engine.ValidationErrors{}
	if def == nil {
		errs.Add(engine.PathRoot.String(), engine.CodeMissingRequired, "simulation definition is nil")
		return nil, sortedIssues(errs)
	}

	gDef := loadGraph(def, baseDir, reg, errs)
	if gDef == nil {
		return nil, sortedIssues(errs)
	}

	nodeExists := make(map[string]bool, len(gDef.Nodes))
	var effectful []string
	for _, node := range gDef.Nodes {
		nodeExists[string(node.ID)] = true
		if effectfulNodeTypes[node.Type] {
			effectful = append(effectful, string(node.ID))
		}
	}
	sort.Strings(effectful)

	behaviorsPath := engine.PathRoot.Field("behaviors")
	for _, id := range sortedKeys(def.Behaviors) {
		if !nodeExists[id] {
			errs.Add(behaviorsPath.Field(id).String(), sim.CodeBehaviorUnknownNode, fmt.Sprintf("behavior for unknown node %q", id))
		}
	}
	for _, id := range effectful {
		if _, ok := def.Behaviors[id]; !ok {
			errs.Add(behaviorsPath.Field(id).String(), sim.CodeBehaviorMissing, fmt.Sprintf("effectful node %q requires a behavior", id))
		}
	}

	latentEnv, err := newEnv("world")
	if err != nil {
		errs.Add(engine.PathRoot.String(), engine.CodeInvalidValue, fmt.Sprintf("failed to create latent env: %v", err))
		return nil, sortedIssues(errs)
	}
	behaviorEnv, err := newEnv("world", "latent", "inputs")
	if err != nil {
		errs.Add(engine.PathRoot.String(), engine.CodeInvalidValue, fmt.Sprintf("failed to create behavior env: %v", err))
		return nil, sortedIssues(errs)
	}
	objectiveEnv, err := newEnv("world", "latent", "nodes", "run")
	if err != nil {
		errs.Add(engine.PathRoot.String(), engine.CodeInvalidValue, fmt.Sprintf("failed to create objective env: %v", err))
		return nil, sortedIssues(errs)
	}

	m := &CompiledModel{
		Def:                 def,
		Graph:               gDef,
		Registry:            reg,
		LatentOrder:         sortedKeys(def.Latents),
		CompiledLatents:     make(map[string]CompiledValue, len(def.Latents)),
		CompiledConstraints: make(map[string]cel.Program, len(def.Objective.Constraints)),
		CompiledBehaviors:   make(map[string]CompiledBehavior, len(def.Behaviors)),
	}

	latentsPath := engine.PathRoot.Field("latents")
	for _, name := range m.LatentOrder {
		m.CompiledLatents[name] = compileValue(def.Latents[name], latentsPath.Field(name), latentEnv, errs)
	}

	objPath := engine.PathRoot.Field("objective")
	m.CompiledObjective = compileCEL(def.Objective.Value, objPath.Field("value").String(), objectiveEnv, numericTypes, errs)
	for _, name := range sortedKeys(def.Objective.Constraints) {
		path := objPath.Field("constraints").Field(name).String()
		if prog := compileCEL(def.Objective.Constraints[name], path, objectiveEnv, boolTypes, errs); prog != nil {
			m.CompiledConstraints[name] = prog
		}
	}

	for _, id := range sortedKeys(def.Behaviors) {
		m.CompiledBehaviors[id] = compileBehavior(def.Behaviors[id], behaviorsPath.Field(id), behaviorEnv, errs)
	}

	ValidateDecisions(m, errs)
	if errs.Len() > 0 {
		return nil, sortedIssues(errs)
	}
	return m, nil
}

func compileBehavior(b Behavior, base engine.Path, env *cel.Env, errs *engine.ValidationErrors) CompiledBehavior {
	cb := CompiledBehavior{Outputs: make(map[string]CompiledValue, len(b.Output))}
	if b.Cost != nil {
		cb.Cost = compileParamPtr(b.Cost, base.Field("cost").String(), env, errs)
	}
	switch d := b.Duration.(type) {
	case nil:
	case string:
		cb.Duration = &CompiledValue{Kind: KindFixed, Fixed: d}
	case ValueSpec:
		cv := compileValue(d, base.Field("duration"), env, errs)
		cb.Duration = &cv
	default:
		errs.Add(base.Field("duration").String(), engine.CodeInvalidType, "expected duration string or value spec")
	}
	if b.Abandon != nil {
		cb.AbandonP = compileParamPtr(b.Abandon.P, base.Field("abandon").Field("p").String(), env, errs)
	}
	if b.Fail != nil {
		cb.FailP = compileParamPtr(b.Fail.P, base.Field("fail").Field("p").String(), env, errs)
	}
	for _, name := range sortedKeys(b.Output) {
		cb.Outputs[name] = compileValue(b.Output[name], base.Field("output").Field(name), env, errs)
	}
	return cb
}
