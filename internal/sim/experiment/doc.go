// Package experiment runs simulation experiments: every policy of a policy
// source is evaluated in every world of an experiment/v1 document over many
// replications, and the per-episode run records are aggregated into
// deterministic estimates and a JSON-stable Report.
//
// # Determinism
//
// Replication r of world w uses a Monte Carlo sampler seeded with
// sim.Seed(experiment seed, w, r). A fresh sampler with that seed is created
// for every policy, so all policies see identical random streams (common
// random numbers) and differences between their estimates come from the
// policies alone.
//
// Replications are grouped into fixed-size chunks (ChunkSize, independent of
// the worker count). Each chunk is evaluated sequentially by one worker and
// summarized with Welford's algorithm; chunk summaries are then merged with
// Chan's parallel update in chunk-index order. The report is therefore
// byte-identical for any number of workers.
//
// # Percentiles and the retention cap
//
// P50 and P95 are exact nearest-rank percentiles of the retained values.
// Values are retained for the first MaxRetained replications of each
// (world, policy) pair; mean, standard deviation, confidence interval, min,
// max and constraint probabilities always use every replication. When an
// experiment has more than MaxRetained runs the percentiles are therefore
// computed from the first MaxRetained replications only, which is still
// deterministic.
//
// # Exact evaluation
//
// The exact evaluator (evaluator: exact) enumerates every chance branch of
// an episode with sampler.NewEnumerator, re-running kernel.RunEpisode once
// per branch. Policies are deterministic, so the chance branches are the
// only source of uncertainty. Each branch is weighted by its probability;
// the weights of one (world, policy) pair must sum to 1 within 1e-12. The
// estimate's mean, population standard deviation, min, max, constraint
// probabilities and weighted nearest-rank P50/P95 are exact, its confidence
// interval collapses to the mean and Estimate.Exact is true. Pairs are
// evaluated in parallel and stored by index, so the report does not depend
// on the worker count. A model whose CompiledModel.Discrete is false is
// rejected with every reason. Evaluator "both" reports the exact estimates
// in PolicyResult.Objective/Duration and the Monte Carlo ones next to them
// in PolicyResult.MonteCarlo.
//
// # Exhaustive search
//
// ExhaustiveSource (policies: {search: exhaustive}) enumerates every reduced
// deterministic policy: one option per reachable information set (a decision
// and the values of its observes paths), counting policies that differ only
// on unreachable information sets once. Each policy is a *TablePolicy with a
// stable id and Describe; EmitPolicy writes it as a policy/v1 document. More
// than max_policies (DefaultMaxPolicies) policies is an error that points to
// the P1 backward-induction solver.
package experiment
