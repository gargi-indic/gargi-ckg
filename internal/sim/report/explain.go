package report

import (
	"fmt"

	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

// This file holds the JSON types of the reports that explain why a policy
// wins: voi, flip_point, path_heatmap and policy_tree. They are computed by
// internal/sim/experiment, which runs the exact solver and the kernel; the
// policy tree is assembled here from the solver's information sets.

// PolicyValue is a policy and its exact expected objective.
type PolicyValue struct {
	// PolicyID is the policy's ID().
	PolicyID string `json:"policyId"`
	// EV is the policy's exact expected objective.
	EV float64 `json:"ev"`
}

// VOI is the value of observing a node in one world: the optimal EV when
// the decisions observe the node minus the optimal EV when no decision
// observes it and running it is free.
type VOI struct {
	// World is the world id.
	World sim.WorldID `json:"world"`
	// Node is the observed node.
	Node string `json:"node"`
	// WithObservation is the optimal policy of the model as written.
	WithObservation PolicyValue `json:"withObservation"`
	// WithoutObservation is the optimal policy of the model without the
	// node's observes paths and without its behavior cost. Its policy id
	// belongs to that modified model and is not listed in the worlds'
	// policy results.
	WithoutObservation PolicyValue `json:"withoutObservation"`
	// VOI is WithObservation.EV minus WithoutObservation.EV.
	VOI float64 `json:"voi"`
}

// FlipSample is the optimal policy at one value of a swept param.
type FlipSample struct {
	// Value is the param value.
	Value float64 `json:"value"`
	// PolicyID is the id of the optimal policy at Value.
	PolicyID string `json:"policyId"`
	// EV is the optimal policy's exact expected objective at Value.
	EV float64 `json:"ev"`
}

// Flip is one change of the optimal policy along a sweep: Left and Right
// bracket the change point within the bisection tolerance.
type Flip struct {
	// From describes the optimal policy left of the flip (its Describe()).
	From string `json:"from"`
	// To describes the optimal policy right of the flip (its Describe()).
	To string `json:"to"`
	// Left is the largest value found where From is optimal.
	Left FlipSample `json:"left"`
	// Right is the smallest value found where To is optimal.
	Right FlipSample `json:"right"`
}

// FlipPoint is a flip_point report: where the optimal policy of the base
// world changes as one numeric world param is swept over [Lo, Hi], every
// other param keeping the base world's value.
type FlipPoint struct {
	// Param is the swept world param.
	Param string `json:"param"`
	// BaseWorld is the world whose other params are kept (the experiment's
	// first world).
	BaseWorld sim.WorldID `json:"baseWorld"`
	// Lo and Hi are the sweep range.
	Lo float64 `json:"lo"`
	Hi float64 `json:"hi"`
	// Sweep lists the optimal policy at each of the evenly spaced sweep
	// points, from Lo to Hi inclusive (the data of a flip-point chart).
	Sweep []FlipSample `json:"sweep"`
	// Flips lists every change of the optimal policy found, in increasing
	// param order. It is empty when the optimal policy is the same at every
	// sweep point.
	Flips []Flip `json:"flips"`
}

// NodeMass is the probability that a node runs.
type NodeMass struct {
	// ID is the node id.
	ID string `json:"id"`
	// Mass is the probability (exact) or the fraction of runs (Monte
	// Carlo) in which the node started.
	Mass float64 `json:"mass"`
}

// EdgeMass is the probability that an edge is traversed.
type EdgeMass struct {
	// From and To are the edge's endpoints.
	From string `json:"from"`
	To   string `json:"to"`
	// Index is the edge's index in the graph's edges list.
	Index int `json:"index"`
	// Mass is the probability (exact) or the fraction of runs (Monte
	// Carlo) in which the target node was started through this edge: the
	// source succeeded and the edge's condition held when the target was
	// scheduled.
	Mass float64 `json:"mass"`
}

// PathHeatmap is the path_heatmap report of one world: how often each node
// runs and each edge is taken under the world's best policy.
type PathHeatmap struct {
	// World is the world id.
	World sim.WorldID `json:"world"`
	// PolicyID is the world's best policy: the first entry of its ranking
	// (the best feasible policy when there is one).
	PolicyID string `json:"policyId"`
	// Nodes lists every graph node in graph order.
	Nodes []NodeMass `json:"nodes"`
	// Edges lists every graph edge in graph order.
	Edges []EdgeMass `json:"edges"`
}

// TreeAction is one option of an information set of the policy tree.
type TreeAction struct {
	// Option is the decision option.
	Option string `json:"option"`
	// Value is the conditional expected objective of choosing Option at
	// the information set, every later decision following the optimal
	// policy.
	Value float64 `json:"value"`
	// Children are the information sets that immediately follow this
	// option (their parent is this information set and option), in graph
	// order of their decisions then label. Empty when no decision follows.
	Children []TreeNode `json:"children"`
}

// TreeNode is one information set of the policy tree.
type TreeNode struct {
	// InfoSet names the information set, e.g. "route[score,high]".
	InfoSet string `json:"infoSet"`
	// Decision is the decision node id.
	Decision string `json:"decision"`
	// Observed maps each observes path of the decision to its value; null
	// marks a node that did not run.
	Observed map[string]any `json:"observed"`
	// ReachProbability is the probability of reaching the information set
	// under the optimal policy (0 off its path).
	ReachProbability float64 `json:"reachProbability"`
	// OnPath reports whether the optimal policy reaches the information
	// set: it is a root, or its parent is on the path and the optimal
	// choice there is the option it follows.
	OnPath bool `json:"onPath"`
	// Choice is the option the optimal policy picks here.
	Choice string `json:"choice"`
	// Actions lists every option in the decision's options order.
	Actions []TreeAction `json:"actions"`
}

// PolicyTree is the policy_tree report of one world: the optimal table
// policy as a tree of information sets, decision → information set →
// option → value, including information sets that are reached only off the
// optimal path.
type PolicyTree struct {
	// World is the world id.
	World sim.WorldID `json:"world"`
	// PolicyID is the optimal policy's ID().
	PolicyID string `json:"policyId"`
	// EV is the optimal policy's exact expected objective.
	EV float64 `json:"ev"`
	// Roots are the information sets that start their histories (no
	// earlier decision), in graph order of their decisions then label.
	Roots []TreeNode `json:"roots"`
}

// TreeSet is one information set of an exact solution, the input of
// NewPolicyTree.
type TreeSet struct {
	// InfoSet, Decision, Observed, ReachProbability and Choice are copied
	// into the TreeNode.
	InfoSet          string
	Decision         string
	Observed         map[string]any
	ReachProbability float64
	Choice           string
	// HasParent reports whether an earlier decision precedes the set; then
	// ParentInfoSet and ParentOption name it and the option chosen there.
	HasParent     bool
	ParentInfoSet string
	ParentOption  string
	// Options and Values are the decision's options and their conditional
	// values, in options order.
	Options []string
	Values  []float64
}

// NewPolicyTree assembles the policy tree of one world from the solver's
// information sets. Sets keep their input order among siblings, which for
// the solver's list is graph order of decisions then label. Every parent
// must be one of sets and offer the option its children follow.
func NewPolicyTree(world sim.WorldID, policyID string, ev float64, sets []TreeSet) (*PolicyTree, error) {
	index := make(map[string]int, len(sets))
	for i, s := range sets {
		if _, dup := index[s.InfoSet]; dup {
			return nil, fmt.Errorf("report: policy tree: duplicate information set %q", s.InfoSet)
		}
		if len(s.Options) != len(s.Values) {
			return nil, fmt.Errorf("report: policy tree: information set %q has %d options and %d values", s.InfoSet, len(s.Options), len(s.Values))
		}
		index[s.InfoSet] = i
	}
	type edge struct {
		set    int
		option string
	}
	children := map[edge][]int{}
	var roots []int
	for i, s := range sets {
		if !s.HasParent {
			roots = append(roots, i)
			continue
		}
		p, ok := index[s.ParentInfoSet]
		if !ok {
			return nil, fmt.Errorf("report: policy tree: information set %q follows unknown information set %q", s.InfoSet, s.ParentInfoSet)
		}
		found := false
		for _, o := range sets[p].Options {
			found = found || o == s.ParentOption
		}
		if !found {
			return nil, fmt.Errorf("report: policy tree: information set %q follows unknown option %q of %q", s.InfoSet, s.ParentOption, s.ParentInfoSet)
		}
		k := edge{p, s.ParentOption}
		children[k] = append(children[k], i)
	}

	// Every set has at most one parent, so the walk from the roots visits
	// each set at most once; sets on a parent cycle are never reached and
	// are reported below.
	placed := 0
	var build func(i int, onPath bool) TreeNode
	build = func(i int, onPath bool) TreeNode {
		placed++
		s := sets[i]
		n := TreeNode{
			InfoSet:          s.InfoSet,
			Decision:         s.Decision,
			Observed:         s.Observed,
			ReachProbability: s.ReachProbability,
			OnPath:           onPath,
			Choice:           s.Choice,
			Actions:          make([]TreeAction, len(s.Options)),
		}
		for a, opt := range s.Options {
			act := TreeAction{Option: opt, Value: s.Values[a], Children: []TreeNode{}}
			for _, c := range children[edge{i, opt}] {
				act.Children = append(act.Children, build(c, onPath && opt == s.Choice))
			}
			n.Actions[a] = act
		}
		return n
	}
	tree := &PolicyTree{World: world, PolicyID: policyID, EV: ev, Roots: make([]TreeNode, 0, len(roots))}
	for _, r := range roots {
		tree.Roots = append(tree.Roots, build(r, true))
	}
	if placed != len(sets) {
		return nil, fmt.Errorf("report: policy tree: %d of %d information sets are not reachable from a root", len(sets)-placed, len(sets))
	}
	return tree, nil
}
