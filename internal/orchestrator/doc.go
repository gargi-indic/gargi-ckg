// Package orchestrator handles Temporal integration and workflow orchestration for Gargi Flow.
//
// # Workflow Execution
//
// The core orchestration engine is implemented in [GraphExecutionWorkflow], which
// executes reactive computational graphs defined by [engine.GraphDefinition]. Graph execution
// handles node scheduling, activity dispatching via [NodeActivities.ExecuteNodeActivity],
// child workflow execution, saga compensation on failure, and state transitions.
//
// # Versioning Strategy
//
// Workflow definitions must maintain deterministic execution across code deployments.
// Gargi uses a two-tier versioning strategy:
//
// 1. In-Workflow Patch Versioning:
// For incremental logic changes, activities, timers, or validation rules within existing workflows,
// use [GetWorkflowVersion] or [IsVersionAtLeast] with unique change IDs such as
// [ChangeIDGraphWorkflowVersion] or [ChangeIDPayloadValidation]. Version markers are recorded
// into workflow history on the first execution and preserved during replays:
//
//	v := orchestrator.GetWorkflowVersion(ctx, ChangeIDGraphWorkflowVersion, VersionInitial, VersionV1)
//	if v >= VersionV1 {
//	    checkPayloadSize(ctx, "input_definition", input.Definition)
//	}
//
// 2. Worker Build IDs & Version Sets:
// For major architectural updates, fleet routing, and canary deployments, workers are configured
// with a BuildID and UseBuildIDForVersioning in [WorkerManager]. Temporal Server routes tasks
// according to compatible version sets.
//
// 3. Safe Deprecation Lifecycle:
// To retire older version branches:
//   - Phase 1 (Baseline): Unversioned execution [VersionInitial].
//   - Phase 2 (Active Evolution): Gate new logic with [GetWorkflowVersion](ctx, id, VersionInitial, VersionV1).
//   - Phase 3 (Deprecation): Once all executions started prior to Phase 2 have completed,
//     bump minSupported to VersionV1 ([GetWorkflowVersion](ctx, id, VersionV1, VersionV1)) and remove legacy branch.
//   - Phase 4 (Retirement): Once retention expires for old histories, the version gate may be removed.
//
// # Replay Testing
//
// To prevent non-deterministic regressions, recorded workflow execution histories are stored in
// testdata/ (e.g. recorded_history_v1.json) and verified in replay_test.go using
// Temporal's [worker.WorkflowReplayer].
package orchestrator
