// Package jobs runs simulation experiments as asynchronous jobs: a bounded
// in-process queue feeds a pool of workers that compile the experiment
// against a stored model bundle, run it with experiment.Run, and persist
// progress and the report through a sim.ExperimentStore.
//
// Model bundles are compiled from memory: the bundle's documents are served
// as an fs.FS (see BundleFS) to experiment.CompileFS, so nothing touches the
// disk. Single runs of a finished experiment can be replayed as the engine
// event stream a production execution would emit (Replay).
//
// Like the rest of internal/sim this is a domain package: it never imports
// internal/api, internal/orchestrator, internal/store or Temporal
// (ADR-0009). Stores are injected by cmd/* (ADR-0004).
package jobs
