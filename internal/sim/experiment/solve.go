package experiment

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"

	"github.com/gargi-indic/gargi-flow/internal/sim/kernel"
	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

// SolverBackwardInduction is the algorithm reported in WorldSolution.Solver
// by ExactSource.
const SolverBackwardInduction = "backward_induction"

// TieTolerance is the tolerance of the exact solver's tie-breaking: an option
// replaces the best option found so far (in the decision node's options
// order) only when its value exceeds the best value by more than
// TieTolerance·max(1, |best value|). Options closer than that are ties and
// the option listed first in the decision node's options wins.
const TieTolerance = 1e-9

// Solver is a PolicySource that solves for the optimal policy of each world
// itself. Run calls Solve with the experiment's worlds instead of Policies,
// evaluates the solved policies (each distinct policy once) and reports each
// world's solution in WorldReport.Solution. ExactSource and *Solution
// implement it.
type Solver interface {
	PolicySource
	// Solve returns the optimal policy and the per-information-set action
	// values of every world, in worlds order.
	Solve(ctx context.Context, m *model.CompiledModel, worlds []sim.World) (*Solution, error)
}

// ExactSource is a Solver (policies: {search: exact}) that finds the optimal
// deterministic table policy of each world by backward induction over
// information sets, without enumerating policies.
//
// Information sets are those of ExhaustiveSource: a decision node together
// with the values of its observes paths. For each world the solver explores
// the tree of histories once, treating every chance outcome and every option
// of every decision as a branch point (kernel.RunEpisode is replayed once per
// leaf, like sampler.NewEnumerator does for chance alone). It records the
// chance probability P(h) of every history h reaching a decision, the
// information set of that decision and every leaf's objective. Information
// sets are then solved deepest first (an information set is processed after
// every information set that can follow it in an episode, which for a
// sequential graph is reverse topological order of the decision nodes): for
// each information set I it picks the option a maximizing
// Σ_{h∈I} P(h)·V(h,a), where V(h,a) is the value of history h after choosing
// a with every later information set already fixed. The cost is
// proportional to the number of histories, not to the number of policies.
//
// The induction is optimal under perfect recall: all histories of an
// information set passed through the same earlier information sets and
// chose the same options there. Solve checks this and rejects a model that
// violates it with *engine.ValidationErrors (sim.CodeNotPerfectRecall, one
// issue per decision node), as it rejects a model that is not discrete
// (sim.CodeModelNotDiscrete). max_policies does not apply.
//
// Ties between options are resolved by TieTolerance in favour of the option
// listed first. The solved policy of a world is a *TablePolicy over the
// information sets reachable under it (as reduced by ExhaustiveSource), so it
// has the same id and Describe as the equal policy found by exhaustive
// search.
type ExactSource struct {
	// Worlds are the worlds Policies solves. Empty means every world of the
	// model.
	Worlds []sim.World
}

var (
	_ Solver = ExactSource{}
	_ Solver = (*Solution)(nil)
)

// ExactSource returns an ExactSource over the experiment's worlds.
func (e *CompiledExperiment) ExactSource() ExactSource {
	return ExactSource{Worlds: e.Worlds}
}

// Policies solves every world of s (every model world when s.Worlds is
// empty) and returns the distinct optimal policies in world order.
func (s ExactSource) Policies(m *model.CompiledModel) ([]sim.Policy, error) {
	if m == nil || m.Graph == nil {
		return nil, errors.New("experiment: compiled model is nil")
	}
	worlds := s.Worlds
	if len(worlds) == 0 {
		worlds = modelWorlds(m)
	}
	sol, err := s.Solve(context.Background(), m, worlds)
	if err != nil {
		return nil, err
	}
	return sol.Policies(m)
}

// Solve solves every world of worlds (s.Worlds is ignored). A model that is
// not discrete or not perfect-recall is rejected with
// *engine.ValidationErrors listing every reason, sorted by path, code and
// message.
func (ExactSource) Solve(ctx context.Context, m *model.CompiledModel, worlds []sim.World) (*Solution, error) {
	if m == nil || m.Graph == nil {
		return nil, errors.New("experiment: compiled model is nil")
	}
	if err := requireDiscrete(m, "exact policy search"); err != nil {
		return nil, err
	}
	if len(worlds) == 0 {
		return nil, errors.New("experiment: exact policy search: no worlds")
	}

	sp := newSearchSpace(m)
	recall := &recallCheck{space: sp, byDecision: map[int]*recallViolation{}}
	trees := make([]*explorer, len(worlds))
	for i, w := range worlds {
		x := &explorer{space: sp, recall: recall, sets: map[string]*solveSet{}}
		if err := x.explore(ctx, w); err != nil {
			return nil, err
		}
		trees[i] = x
	}
	if err := recall.err(); err != nil {
		return nil, err
	}

	sol := &Solution{model: m, Worlds: make([]WorldSolution, len(worlds))}
	seen := map[string]bool{}
	for i, x := range trees {
		ws, err := x.solve()
		if err != nil {
			return nil, fmt.Errorf("experiment: exact policy search: world %q: %w", worlds[i].ID, err)
		}
		ws.World = worlds[i].ID
		sol.Worlds[i] = *ws
		if !seen[ws.PolicyID] {
			seen[ws.PolicyID] = true
			sol.policies = append(sol.policies, ws.policy)
		}
	}
	return sol, nil
}

// Solution is the result of ExactSource.Solve. It is itself a Solver over
// its fixed result, so a caller can solve once, keep the policies and pass
// the solution to Run.
type Solution struct {
	model *model.CompiledModel
	// Worlds holds one entry per solved world, in the order of the worlds
	// passed to Solve.
	Worlds   []WorldSolution
	policies []sim.Policy
}

// Policies returns the distinct solved policies (each a *TablePolicy) in
// world order. m must be the model the solution was computed for.
func (s *Solution) Policies(m *model.CompiledModel) ([]sim.Policy, error) {
	if m != s.model {
		return nil, errors.New("experiment: solution was computed for a different model")
	}
	return slices.Clone(s.policies), nil
}

// Solve returns s when m and worlds (by id, in order) are those s was
// computed for, and an error otherwise.
func (s *Solution) Solve(_ context.Context, m *model.CompiledModel, worlds []sim.World) (*Solution, error) {
	if m != s.model {
		return nil, errors.New("experiment: solution was computed for a different model")
	}
	if len(worlds) != len(s.Worlds) {
		return nil, fmt.Errorf("experiment: solution covers %d worlds, want %d", len(s.Worlds), len(worlds))
	}
	for i, w := range worlds {
		if w.ID != s.Worlds[i].World {
			return nil, fmt.Errorf("experiment: solution world %d is %q, want %q", i, s.Worlds[i].World, w.ID)
		}
	}
	return s, nil
}

// WorldSolution is the exact solution of one world: the optimal policy, its
// exact expected objective and the value of every option at every
// information set. Its JSON form is WorldReport.Solution.
type WorldSolution struct {
	// World is the solved world (reported by the enclosing WorldReport).
	World sim.WorldID `json:"-"`
	// Solver names the algorithm, SolverBackwardInduction.
	Solver string `json:"solver"`
	// PolicyID is the optimal policy's ID().
	PolicyID string `json:"policyId"`
	// Description is the optimal policy's Describe(): the chosen option of
	// every information set reachable under it.
	Description string `json:"description"`
	// EV is the exact expected objective of the optimal policy.
	EV float64 `json:"ev"`
	// Histories is the number of explored histories (leaves of the tree of
	// chance outcomes and decision options), the measure of the solver's
	// work.
	Histories int `json:"histories"`
	// InformationSets lists every information set the exploration reached,
	// on and off the optimal policy's path, ordered by decision node (graph
	// order) then label.
	InformationSets []InformationSetValue `json:"informationSets"`

	policy  *TablePolicy
	reduced *big.Int
}

// Policy returns the optimal policy.
func (w *WorldSolution) Policy() *TablePolicy { return w.policy }

// ReducedPolicies returns the number of reduced deterministic policies of
// the world (the number ExhaustiveSource would enumerate for this world
// alone), computed from the information-set tree without enumerating them.
func (w *WorldSolution) ReducedPolicies() *big.Int { return new(big.Int).Set(w.reduced) }

// InformationSetValue holds the solver's values at one information set.
type InformationSetValue struct {
	// InfoSet names the information set as in TablePolicy.Describe: the
	// decision id followed, when the decision observes anything, by the
	// observed values in observes order, e.g. "route[score,high]".
	InfoSet string `json:"infoSet"`
	// Decision is the decision node id.
	Decision string `json:"decision"`
	// Observed maps each observes path of the decision to its value; null
	// marks a node that did not run (or an output it did not produce).
	Observed map[string]any `json:"observed"`
	// Parent is the information set and option that immediately precede this
	// information set in every history (perfect recall); it is omitted for an
	// information set that is the first decision of its histories.
	Parent *InfoSetChoice `json:"parent,omitempty"`
	// ChanceProbability is the probability, over chance alone, of the
	// histories that reach the information set: the probability of reaching
	// it under any policy that makes the choices on its Parent chain.
	ChanceProbability float64 `json:"chanceProbability"`
	// ReachProbability is the probability of reaching the information set
	// under the optimal policy: ChanceProbability when the optimal policy
	// makes the choices on its Parent chain, otherwise 0. Information sets
	// with probability 0 are not part of the optimal policy's table.
	ReachProbability float64 `json:"reachProbability"`
	// Choice is the option the optimal policy picks here (also for an
	// information set it does not reach).
	Choice string `json:"choice"`
	// Actions lists every option of the decision, in the decision node's
	// options order, with its conditional value.
	Actions []ActionValue `json:"actions"`
}

// InfoSetChoice names an option chosen at an information set.
type InfoSetChoice struct {
	// InfoSet names the information set, as InformationSetValue.InfoSet.
	InfoSet string `json:"infoSet"`
	// Option is the option chosen there.
	Option string `json:"option"`
}

// ActionValue is the value of one option at an information set.
type ActionValue struct {
	// Option is the decision option.
	Option string `json:"option"`
	// Value is the conditional expected objective given that the
	// information set is reached, the option is chosen and every later
	// decision follows the optimal policy:
	// Σ_{h∈I} P(h)·V(h,a) / Σ_{h∈I} P(h).
	Value float64 `json:"value"`
}

// nodeKind is the kind of a history-tree node.
type nodeKind uint8

const (
	kindLeaf nodeKind = iota
	kindChance
	kindDecision
)

// tnode is a node of the history tree: a chance draw, a decision or the end
// of an episode.
type tnode struct {
	kind nodeKind
	// label is the sampler stream (chance) or decision id (decision); a
	// replayed episode must reach the same point.
	label string
	// probs are the normalized branch weights of a chance node.
	probs []float64
	// set is the information set of a decision node.
	set *solveSet
	// prob is the chance probability of reaching the node.
	prob float64
	// children are indexed by option; zero-weight chance branches stay nil.
	children []*tnode
	// value is the objective of a leaf, or the memoized value of an inner
	// node once every information set below it is solved.
	value float64
	done  bool
}

// nextOption returns the branch after choice, skipping zero-weight chance
// branches, or -1 when choice is the last one.
func (n *tnode) nextOption(choice int) int {
	for next := choice + 1; next < len(n.children); next++ {
		if n.kind == kindDecision || n.probs[next] > 0 {
			return next
		}
	}
	return -1
}

// branchProb is the probability of taking branch i of n: the chance weight
// for a chance node, 1 for a decision.
func (n *tnode) branchProb(i int) float64 {
	if n.kind == kindChance {
		return n.probs[i]
	}
	return 1
}

// setChoice is an option chosen at an information set.
type setChoice struct {
	set    *solveSet
	option int
}

// solveSet is an information set of the solver.
type solveSet struct {
	is infoSet
	// dec is the decision's index in searchSpace.decisions.
	dec int
	// name is the information set's name, e.g. "route[score,high]".
	name string
	// depth is the number of decisions before it in its histories.
	depth int
	// pred is the immediately preceding choice (nil for a first decision).
	pred *setChoice
	// histories are the decision nodes of the information set.
	histories []*tnode
	// children lists, per option, the information sets whose pred is that
	// option of this set.
	children [][]*solveSet

	choice    int
	values    []float64
	chance    float64
	reach     float64
	reachable bool
}

// recallViolation records the first perfect-recall violation of a decision
// (the information set, its two different preceding choices and the world)
// and every information set of that decision that violates it.
type recallViolation struct {
	set      *solveSet
	first    *setChoice
	other    *setChoice
	world    sim.WorldID
	affected map[string]bool
}

// recallCheck collects perfect-recall violations across worlds.
type recallCheck struct {
	space      *searchSpace
	byDecision map[int]*recallViolation
}

// observe checks that set is always preceded by the same choice.
func (r *recallCheck) observe(world sim.WorldID, set *solveSet, pred *setChoice) {
	if samePred(set.pred, pred) {
		return
	}
	v := r.byDecision[set.dec]
	if v == nil {
		v = &recallViolation{set: set, first: set.pred, other: pred, affected: map[string]bool{}, world: world}
		r.byDecision[set.dec] = v
	}
	v.affected[set.is.key] = true
}

func samePred(a, b *setChoice) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.set.is.key == b.set.is.key && a.option == b.option
}

// describeChoice renders a preceding choice for an error message.
func describeChoice(c *setChoice) string {
	if c == nil {
		return "no earlier decision"
	}
	return c.set.name + "=" + c.set.is.options[c.option]
}

// err returns the collected violations as *engine.ValidationErrors, one
// issue per decision at its observes list, or nil.
func (r *recallCheck) err() error {
	if len(r.byDecision) == 0 {
		return nil
	}
	errs := &engine.ValidationErrors{}
	modelAt := engine.PathRoot.Field("model").String()
	for dec, v := range r.byDecision {
		d := r.space.decisions[dec]
		at := modelAt + ":$.graph:" + engine.PathRoot.Field("nodes").Index(d.node).Field("config").Field("observes").String()
		errs.Add(at, sim.CodeNotPerfectRecall, fmt.Sprintf(
			"decision %q does not have perfect recall: in world %q its information set %s follows %s in one history and %s in another (%d information set(s) affected); exact policy search requires every decision to observe what earlier decisions observed and chose",
			d.id, v.world, v.set.name, describeChoice(v.first), describeChoice(v.other), len(v.affected)))
	}
	return sortIssues(errs)
}

// step is one branch point of the current history and the branch taken.
type step struct {
	node   *tnode
	choice int
}

// explorer builds the history tree of one world by replaying episodes. It is
// both the sampler (chance points) and the policy (decision points) of every
// episode it runs.
type explorer struct {
	space  *searchSpace
	recall *recallCheck
	world  sim.WorldID

	root      *tnode
	path      []step
	depth     int
	sets      map[string]*solveSet
	order     []*solveSet
	histories int
	err       error
}

// explorerSampler adapts an explorer to sim.Sampler.
type explorerSampler struct{ x *explorer }

// explorerPolicy adapts an explorer to sim.Policy.
type explorerPolicy struct{ x *explorer }

var (
	_ sim.Sampler = explorerSampler{}
	_ sim.Policy  = explorerPolicy{}
)

func (explorerPolicy) ID() string       { return "exact-solver" }
func (explorerPolicy) Describe() string { return "exact solver exploration" }

func (p explorerPolicy) Choose(_ context.Context, decision engine.NodeID, obs sim.Observation, options []string) (string, error) {
	x := p.x
	if x.depth < len(x.path) {
		st := x.path[x.depth]
		if st.node.kind != kindDecision || st.node.label != string(decision) || st.choice >= len(options) {
			return "", x.fail(fmt.Errorf("experiment: exact policy search: episode diverged at decision %q", decision))
		}
		x.depth++
		return options[st.choice], nil
	}
	if len(options) == 0 {
		return "", x.fail(fmt.Errorf("experiment: decision %q offers no options", decision))
	}
	is, err := x.space.infoSetOf(string(decision), obs, options)
	if err != nil {
		return "", x.fail(err)
	}
	var pred *setChoice
	depth := 0
	for _, st := range x.path {
		if st.node.kind == kindDecision {
			pred = &setChoice{set: st.node.set, option: st.choice}
			depth++
		}
	}
	set, ok := x.sets[is.key]
	if !ok {
		dec := x.space.index[is.decision]
		set = &solveSet{
			is:       is,
			dec:      dec,
			name:     is.decision + is.label,
			depth:    depth,
			pred:     pred,
			choice:   -1,
			children: make([][]*solveSet, len(options)),
		}
		x.sets[is.key] = set
		x.order = append(x.order, set)
	} else {
		x.recall.observe(x.world, set, pred)
	}
	n := &tnode{kind: kindDecision, label: string(decision), set: set, children: make([]*tnode, len(options))}
	x.attach(n)
	set.histories = append(set.histories, n)
	x.path = append(x.path, step{node: n})
	x.depth++
	return options[0], nil
}

func (s explorerSampler) Choose(stream string, weights []float64) (int, error) {
	x := s.x
	if x.depth < len(x.path) {
		st := x.path[x.depth]
		if st.node.kind != kindChance || st.node.label != stream || len(st.node.probs) != len(weights) {
			return 0, x.fail(fmt.Errorf("experiment: exact policy search: episode diverged at stream %q", stream))
		}
		x.depth++
		return st.choice, nil
	}
	if len(weights) == 0 {
		return 0, x.fail(errors.New("empty weights"))
	}
	sum := 0.0
	for _, w := range weights {
		if w < 0 || math.IsNaN(w) {
			return 0, x.fail(errors.New("negative weight"))
		}
		sum += w
	}
	if sum <= 0 || math.IsInf(sum, 0) {
		return 0, x.fail(errors.New("weights must have a positive finite sum"))
	}
	probs := make([]float64, len(weights))
	first := -1
	for i, w := range weights {
		probs[i] = w / sum
		if first < 0 && probs[i] > 0 {
			first = i
		}
	}
	if first < 0 {
		return 0, x.fail(errors.New("all weights zero"))
	}
	n := &tnode{kind: kindChance, label: stream, probs: probs, children: make([]*tnode, len(weights))}
	x.attach(n)
	x.path = append(x.path, step{node: n, choice: first})
	x.depth++
	return first, nil
}

// Float returns sim.ErrNotDiscrete: continuous draws cannot be enumerated.
func (explorerSampler) Float(string, sim.Distribution) (float64, error) {
	return 0, sim.ErrNotDiscrete
}

// fail records the first error of the current episode and returns err.
func (x *explorer) fail(err error) error {
	if x.err == nil {
		x.err = err
	}
	return err
}

// attach links a newly discovered node below the current path.
func (x *explorer) attach(n *tnode) {
	if len(x.path) == 0 {
		x.root = n
		n.prob = 1
		return
	}
	parent := x.path[len(x.path)-1]
	parent.node.children[parent.choice] = n
	n.prob = parent.node.prob * parent.node.branchProb(parent.choice)
}

// advance moves the path to the next unexplored branch, depth first (last
// branch point first). It reports false when every branch is explored.
func (x *explorer) advance() bool {
	for i := len(x.path) - 1; i >= 0; i-- {
		st := &x.path[i]
		if next := st.node.nextOption(st.choice); next >= 0 {
			st.choice = next
			x.path = x.path[:i+1]
			return true
		}
	}
	return false
}

// explore runs one episode per history of world w and builds the tree.
func (x *explorer) explore(ctx context.Context, w sim.World) error {
	x.world = w.ID
	policy, smp := explorerPolicy{x: x}, explorerSampler{x: x}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		x.depth, x.err = 0, nil
		rec, err := kernel.RunEpisode(ctx, x.space.m, w, policy, smp, kernel.Options{})
		if x.err != nil {
			return fmt.Errorf("experiment: exact policy search: world %q: %w", w.ID, x.err)
		}
		if err != nil {
			return fmt.Errorf("experiment: exact policy search: world %q: %w", w.ID, err)
		}
		if x.depth != len(x.path) {
			return fmt.Errorf("experiment: exact policy search: world %q: episode ended after %d of %d replayed branch points", w.ID, x.depth, len(x.path))
		}
		x.attach(&tnode{kind: kindLeaf, value: rec.Objective, done: true})
		x.histories++
		if !x.advance() {
			return nil
		}
	}
}

// eval returns the value of n under the solved choices of every information
// set below it, memoizing it.
func eval(n *tnode) (float64, error) {
	if n.done {
		return n.value, nil
	}
	v := 0.0
	switch n.kind {
	case kindChance:
		for i, c := range n.children {
			if c == nil {
				continue
			}
			cv, err := eval(c)
			if err != nil {
				return 0, err
			}
			v += n.probs[i] * cv
		}
	case kindDecision:
		if n.set.choice < 0 {
			return 0, fmt.Errorf("information set %s evaluated before it was solved", n.set.name)
		}
		cv, err := eval(n.children[n.set.choice])
		if err != nil {
			return 0, err
		}
		v = cv
	default:
		return 0, errors.New("leaf without value")
	}
	n.value, n.done = v, true
	return v, nil
}

// solve runs the backward induction over the explored tree and returns the
// world's solution.
func (x *explorer) solve() (*WorldSolution, error) {
	// Deepest information sets first: every information set that can follow
	// another one in an episode is deeper. Sets at equal depth never share a
	// history, so their order does not matter; it is made deterministic
	// (later decision nodes first, then by name and key).
	sets := slices.Clone(x.order)
	slices.SortStableFunc(sets, func(a, b *solveSet) int {
		return cmp.Or(
			cmp.Compare(b.depth, a.depth),
			cmp.Compare(b.dec, a.dec),
			cmp.Compare(a.name, b.name),
			cmp.Compare(a.is.key, b.is.key),
		)
	})
	for _, s := range sets {
		if err := solveSetChoice(s); err != nil {
			return nil, err
		}
	}

	ev, err := eval(x.root)
	if err != nil {
		return nil, err
	}
	markReach(x.root)

	assigned := map[string]string{}
	reachable := map[string]infoSet{}
	for _, s := range x.order {
		if s.reachable {
			assigned[s.is.key] = s.is.options[s.choice]
			reachable[s.is.key] = s.is
		}
		if s.pred != nil {
			p := s.pred
			p.set.children[p.option] = append(p.set.children[p.option], s)
		}
	}
	policy, err := x.space.tablePolicy(assigned, reachable)
	if err != nil {
		return nil, err
	}

	ws := &WorldSolution{
		Solver:      SolverBackwardInduction,
		PolicyID:    policy.ID(),
		Description: policy.Describe(),
		EV:          ev,
		Histories:   x.histories,
		policy:      policy,
		reduced:     x.reducedPolicies(),
	}
	listed := slices.Clone(x.order)
	slices.SortStableFunc(listed, func(a, b *solveSet) int {
		return cmp.Or(cmp.Compare(a.dec, b.dec), cmp.Compare(a.name, b.name), cmp.Compare(a.is.key, b.is.key))
	})
	for _, s := range listed {
		ws.InformationSets = append(ws.InformationSets, x.space.setValue(s))
	}
	return ws, nil
}

// solveSetChoice picks the option of s maximizing Σ_{h∈s} P(h)·V(h,a). Every
// information set that can follow s must already be solved.
func solveSetChoice(s *solveSet) error {
	s.chance = 0
	for _, h := range s.histories {
		s.chance += h.prob
	}
	s.values = make([]float64, len(s.is.options))
	for a := range s.is.options {
		sum := 0.0
		for _, h := range s.histories {
			v, err := eval(h.children[a])
			if err != nil {
				return err
			}
			sum += h.prob * v
		}
		if s.chance > 0 {
			sum /= s.chance
		}
		s.values[a] = sum
	}
	best := 0
	for a := 1; a < len(s.values); a++ {
		if s.values[a] > s.values[best]+TieTolerance*math.Max(1, math.Abs(s.values[best])) {
			best = a
		}
	}
	s.choice = best
	return nil
}

// markReach follows the solved choices from n and records the probability
// of reaching each information set.
func markReach(n *tnode) {
	switch n.kind {
	case kindChance:
		for _, c := range n.children {
			if c != nil {
				markReach(c)
			}
		}
	case kindDecision:
		n.set.reachable = true
		n.set.reach += n.prob
		markReach(n.children[n.set.choice])
	}
}

// reducedPolicies counts the reduced deterministic policies over the
// information-set tree: a set contributes Σ_a Π_{children of (set, a)} and
// the first information sets of the episodes multiply.
func (x *explorer) reducedPolicies() *big.Int {
	memo := map[*solveSet]*big.Int{}
	var count func(s *solveSet) *big.Int
	count = func(s *solveSet) *big.Int {
		if c, ok := memo[s]; ok {
			return c
		}
		total := new(big.Int)
		for _, kids := range s.children {
			prod := big.NewInt(1)
			for _, k := range kids {
				prod.Mul(prod, count(k))
			}
			total.Add(total, prod)
		}
		memo[s] = total
		return total
	}
	total := big.NewInt(1)
	for _, s := range x.order {
		if s.pred == nil {
			total.Mul(total, count(s))
		}
	}
	return total
}

// setValue renders the solver's values at s.
func (sp *searchSpace) setValue(s *solveSet) InformationSetValue {
	d := sp.decisions[s.dec]
	v := InformationSetValue{
		InfoSet:           s.name,
		Decision:          d.id,
		Observed:          make(map[string]any, len(d.observes)),
		ChanceProbability: s.chance,
		ReachProbability:  s.reach,
		Choice:            s.is.options[s.choice],
		Actions:           make([]ActionValue, len(s.is.options)),
	}
	for j, path := range d.observes {
		v.Observed[path] = s.is.values[j]
	}
	if s.pred != nil {
		v.Parent = &InfoSetChoice{InfoSet: s.pred.set.name, Option: s.pred.set.is.options[s.pred.option]}
	}
	for a, opt := range s.is.options {
		v.Actions[a] = ActionValue{Option: opt, Value: s.values[a]}
	}
	return v
}
