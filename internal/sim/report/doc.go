// Package report computes the decision-quality reports of an experiment
// from the estimates of every policy in every world: the cross-world regret
// matrix, the robust picks (expected value under a world prior, maximin and
// CVaR) and per-world Pareto frontiers of two metrics.
//
// The functions are pure: they read a Table (policy ids, world ids, the
// objective mean and feasibility of every (policy, world), metric means and
// lower objective tails) and return JSON-stable values. The package does not
// depend on the experiment runner; internal/sim/experiment builds the Table
// from its estimates and calls these functions. ParseSpec parses the entries
// of experiment/v1 reports; new report kinds plug into it and into
// experiment.ReportSet.
//
// The reports that explain why a policy wins (voi, flip_point, path_heatmap
// and policy_tree) need the exact solver or the kernel, so
// internal/sim/experiment computes them; this package holds their JSON
// types (VOI, FlipPoint, PathHeatmap, PolicyTree) and assembles the policy
// tree from the solver's information sets (NewPolicyTree).
//
// Every result is deterministic: worlds keep experiment order, ties between
// policies go to the lower policy id, and Pareto points are sorted best X
// first, then by policy id.
package report
