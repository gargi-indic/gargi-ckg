// Package sim holds the public contracts of the simulation engine: models,
// behaviors, policies, experiments, run records and reports, plus the
// deterministic seeding helpers shared by every simulation run.
//
// It depends only on pkg/engine and must never import internal packages
// (ADR-0009).
package sim
