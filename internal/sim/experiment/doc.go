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
package experiment
