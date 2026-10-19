# Workflow Versioning and Replay Safety

## Status
Accepted

## Date
2026-10-18

## Context
Temporal executes workflows deterministically by recording history events and replaying those events when a workflow worker starts, recovers, or resumes execution. If workflow implementation code is changed directly (such as modifying activity invocation order, adding or removing activities, child workflows, timers, or side effects) without version gating, replaying existing in-flight workflow executions will produce non-deterministic errors (`workflow panic: nondeterministic error`).

In Gargi CKG, workflows represent long-running reactive knowledge graph orchestrations (e.g., entity extraction, ontology alignment, multi-source ingestion, human review signals, and nested child sub-graphs) that may stay active for minutes, hours, or days. Therefore, zero-downtime rolling upgrades and backward-compatible execution replay are strict operational requirements.

We need a standardized workflow versioning and testing strategy that:
- Ensures running workflows continue execution deterministically across code deployments.
- Clearly delineates when to use in-workflow patch versioning versus Worker Build IDs / version sets.
- Establishes automated replay test suites to catch non-determinism regressions in CI before deploying to production.

## Decision

We adopt a two-tier versioning architecture combined with mandatory replay testing in CI:

### 1. In-Workflow Patch Versioning (`workflow.GetVersion`)

For incremental logic changes, bug fixes, and feature gating within existing workflow definitions (such as `GraphExecutionWorkflow`):

- **API Usage:** Utilize Temporal's native `workflow.GetVersion(ctx, changeID, minSupportedVersion, maxSupportedVersion)`.
- **Change IDs:** Standardize on descriptive, kebab-case change identifiers defined as exported constants (e.g., `ChangeIDGraphWorkflowVersion = "gargi-graph-workflow-version"`).
- **Version Constants:** Define structured versions:
  - `VersionInitial = workflow.DefaultVersion` (`-1`): represents executions that started prior to the version marker introduction.
  - `VersionV1 = 1`: first versioned milestone.
  - `VersionV2 = 2`, etc.
- **Three-Phase Lifecycle:**
  1. **Phase 1 (Introduction):** Introduce new logic behind a version check:
     ```go
     v := workflow.GetVersion(ctx, ChangeIDGraphWorkflowVersion, VersionInitial, VersionV1)
     if v == VersionInitial {
         // Legacy behavior for in-flight workflows
     } else {
         // New behavior for workflows starting now
     }
     ```
  2. **Phase 2 (Deprecation):** After verifying that all in-flight executions recorded prior to V1 have finished, update `minSupportedVersion` to retire the legacy branch:
     ```go
     v := workflow.GetVersion(ctx, ChangeIDGraphWorkflowVersion, VersionV1, VersionV1)
     // New behavior only; attempts to replay pre-V1 histories will fail safely
     ```
  3. **Phase 3 (Subsequent Evolution):** When introducing V2, expand `maxSupportedVersion`:
     ```go
     v := workflow.GetVersion(ctx, ChangeIDGraphWorkflowVersion, VersionV1, VersionV2)
     if v == VersionV1 {
         // V1 behavior
     } else {
         // V2 behavior
     }
     ```

### 2. Worker Versioning via Worker Build IDs and Version Sets

For major structural migrations, workflow engine rewrites, or when maintaining multi-branch version gates inside workflow code becomes unmanageable:

- **Build ID Assignment:** Configure workers with a unique `BuildID` and enable `UseBuildIDForVersioning: true` in `worker.Options`.
- **Version Sets & Dispatch:** Register worker build IDs into version sets via the Temporal CLI or SDK client management APIs (`UpdateWorkerBuildIdCompatibility`).
- **Routing Guarantees:** Temporal Server routes new workflow executions to the default compatible version set while pinning in-flight workflows to their initial build ID (or compatible build IDs).
- **Selection Criteria:**
  - Choose `workflow.GetVersion` when modifying logic within a single workflow function, rolling out backward-compatible enhancements, or operating a unified worker fleet.
  - Choose Worker Build IDs when changing workflow interfaces, performing extensive orchestrator refactoring, or running blue/green deployments where old and new worker binaries coexist.

### 3. Automated Replay Testing in CI

To prevent non-deterministic regressions from reaching production:

- **Test Fixtures:** Store representative workflow execution histories serialized as JSON in `internal/orchestrator/testdata/`.
- **Replay Test Execution:** Use `worker.WorkflowReplayer` (`go.temporal.io/sdk/worker`) in unit tests (`replay_test.go`) to replay recorded histories against current workflow definitions:
  ```go
  replayer := worker.NewWorkflowReplayer()
  replayer.RegisterWorkflow(GraphExecutionWorkflow)
  err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, "testdata/workflow_history.json")
  require.NoError(t, err)
  ```
- **CI Gate:** Replay tests run as part of `go test -v -race ./internal/orchestrator/...` on every pull request.

## Consequences

- **Positive:** In-flight knowledge graph workflows survive deployments without downtime or non-deterministic panics.
- **Positive:** Clear separation between fine-grained in-code versioning and fleet-wide worker build ID routing.
- **Positive:** CI catches non-deterministic code modifications immediately via replayer unit tests.
- **Negative:** Developers must remember to use `workflow.GetVersion` when modifying workflow logic and clean up legacy branches once older executions finish.
- **Negative:** Representative execution histories must be maintained in `testdata/` as test fixtures.

## Alternatives Considered

- **Task Queue Versioning (e.g. `gargi_tasks_v2`):** Running different task queues for different versions. Rejected as a primary mechanism because it requires updating workflow starters/clients, prevents updating in-flight executions, and creates operational queue sprawl.
- **Unversioned In-Place Workflow Changes:** Direct modifications without version gating. Rejected because any change in command history breaks running workflows with non-deterministic errors.
- **Worker Build IDs Exclusively:** Relying solely on Worker Build IDs without `workflow.GetVersion`. Rejected because keeping separate worker fleets alive indefinitely for minor bug fixes or small feature additions introduces excessive operational overhead.
