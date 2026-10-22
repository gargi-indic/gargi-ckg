// Package sim is the root of the simulation domain: parsing and validating
// sim/v1, policy/v1 and experiment/v1 documents, the virtual-time kernel that
// drives the reactive scheduler, behaviors, policy evaluation, solvers and
// metrics.
//
// Like internal/graph and internal/reactive it is a domain package. It must
// never import internal/api, internal/orchestrator, internal/store or
// Temporal (ADR-0009).
package sim
