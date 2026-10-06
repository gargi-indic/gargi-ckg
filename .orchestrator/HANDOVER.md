# Orchestrator Handover: gargi-ckg

Snapshot written by the first orchestrator (Claude Code, Opus 5.5) on **2026-10-05, ~22:45 UTC** and refreshed at the end of **Epic #1** and again at the end of **Epic #8** and at the end of the 2026-10-06 session (mid-Epic #15, waiting on PR #83) by the second/third orchestrator (Claude Code, Sonnet 5.5).
The **live** state is always in GitHub issue **#67 Orchestrator Logs**. If this file and #67 disagree, #67 wins.
To resume, invoke the `gargi-orchestrator` skill (`.claude/skills/gargi-orchestrator/SKILL.md`).

---

## 1. What this project is
- **Product:** a pluggable Reactive Graph Engine middleware in **Go**. It runs DAGs on Temporal, with pluggable state stores (in-memory, Redis, Postgres, Neo4j) and gRPC/REST/SSE/WebSocket APIs. Requirements are in [`docs/SPEC.md`](../docs/SPEC.md).
- **How it's built:** an orchestrator agent assigns backlog sub-issues **one at a time** to **Jules** (Google's coding agent) through the Jules API. It reviews each plan and PR, squash-merges, and logs every step in #67.

## 2. Where everything lives
| Thing | Location |
|---|---|
| Active repo | `gargi-indic/gargi-ckg` (private, default branch `main`) |
| Old repo | `gishnucodes/gargi-ckg`: **archived**, do not use |
| Local clone (orchestrator machine) | `/Applications/code/gargi-ckg` |
| Requirements spec | `docs/SPEC.md` |
| Live log + runbook + status tables | Issue **#67 Orchestrator Logs** (pinned) |
| Successor onboarding + paste-ready prompt | Issue **#68 Orchestrator Handover** (pinned) |
| Jules API helper | `.orchestrator/jules.py` (stdlib Python + `gh`) |
| Resume skill | `.claude/skills/gargi-orchestrator/SKILL.md` |
| Jules API key | `~/.config/jules/env` on the orchestrator Mac (mode 600, exports `JULES_API_KEY`). **Never** paste it into GitHub, commits or logs. The human should rotate it (it was once pasted in a chat). |
| Jules source id | `sources/github/gargi-indic/gargi-ckg` |
| Jules web UI | `https://jules.google.com/session/<session-id>` |

## 3. Backlog map (GitHub issues)
Labels: `epic`, `task`, `area/*`, `needs-discussion`, `orchestrator`. Sub-tasks are attached as real GitHub **sub-issues** of their epic.

| Epic | Title | Sub-issues |
|---|---|---|
| #1 | Project Foundation & Dev Environment | #2–#7 |
| #8 | Core Domain Model & NodeInterface | #9–#14 |
| #15 | Graph Definition & Validation | #16–#21 |
| #22 | Reactive Event System & Scheduler | #23–#28 |
| #29 | Temporal Orchestration | #30–#38 |
| #39 | Pluggable State Storage Adapters | #40–#47 |
| #48 | Client API (gRPC, REST, SSE, WebSockets) | #49–#55 |
| #56 | End-to-End Event Flow & Integration | #57–#60 |
| #61 | Observability & Operations (beyond spec, human to confirm keep/drop) | #62–#66 |
| #67 | Orchestrator Logs | n/a |
| #68 | Orchestrator Handover | n/a |

`needs-discussion` issues: **#6** (decided, see §5), **#20**, **#40**, **#41**, **#51** (all undecided, so escalate when you reach them).

## 4. Current state (mid-Epic #15, end of the 2026-10-06 session)
**Epics #1 and #8 are complete and closed. Epic #15 is in progress: #16 and #21 are merged; #17 is IN REVIEW with fixes requested.** One Jules session is open (#17) and **PR #83 is open**. #18, #19 and #20 have not been started.

| Epic #15 step | Issue | Status | Session | PR / commit |
|---|---|---|---|---|
| 1 | #16 schema | ✅ | `595744714603540031` | #81 `ed4c51a` |
| 2 | #21 validation errors | ✅ | `16903364258271397831` | #82 `d30d45b` |
| 3 | #17 JSON/YAML parsers | 🔍 **Jules pushed round-1 fixes; RE-REVIEW PENDING** | `18419591797003617012` | #83 open, head `43db22a` (pre-fix head was `87403fc`); CI on the new head was still running at handover |
| 4 | #18 structural validation | ⏳ | | |
| 5 | #19 semantic validation | ⏳ | | |
| 6 | #20 edge condition language | ⏳ **needs the human's decision first** | | |

(Epic #8: #9 #75 `6be34c2`, #13 #76 `9fa2371`, #12 #77 `3cd1397`, #10 #78 `f018ffd`, #11 #79 `8479214`, #14 #80 `846411e`. Epic #1: #2 #69, #3 #70, #5 #71, #7 #72, #4 #73, #6 #74. Sessions are in #67.)

### Resume point: #17 / PR #83 (do this first)
1. Boot sequence, then `python3 .orchestrator/jules.py get 18419591797003617012`, `activities 18419591797003617012 5` (look for Jules's chat message with raw command output) and `gh pr checks 83 -R gargi-indic/gargi-ckg`. **Jules already pushed the fixes (PR head `43db22a`); nothing has been re-reviewed or merged yet.** Do not start a second Jules session.
2. The round-1 review (full text posted as a comment on PR #83 and sent to Jules) asked for six things; verify each in the diff of head `43db22a` (`gh pr diff 83`). If something is still wrong, post a round-2 review (`gh pr comment 83 --body-file` and `jules.py message`, then `wait-push`).
   1. **Security:** reject YAML aliases (alias-bomb) + test with a bomb input that must fail fast.
   2. Thread `engine.Path` through the parse stage so duplicate keys / non-string keys / depth / bad scalars carry their path (syntax errors stay at `$`); update goldens; add a nested duplicate-key case.
   3. Binder messages must use JSON type names (`string|number|boolean|object|array|null`), never Go type names.
   4. `bindInt`: `f == math.Trunc(f)` and `|f| <= 2^53`; non-integer → `invalid_type`, out of range → `invalid_value`.
   5. Test gaps: real table-driven binder tests (all sections, pointer fields absent vs present), goldens for YAML syntax error / non-string key / unsupported apiVersion / `nodes: []`, number-normalisation test (`1000`, `1e3`, `1000.0` in JSON and YAML all `float64(1000)`), shuffled-key-order DeepEqual test.
   6. Tidy: merge the stray `import "flag"`, real newlines in the no-panic YAML inputs, document the `-update` flag.
3. Re-review the pushed diff against that list (also re-read `parse.go` for any remaining unbounded expansion), make sure both CI checks are green, then squash-merge (`gh pr merge 83 -R gargi-indic/gargi-ckg --squash --delete-branch`), tick #17's boxes, update the #67 tables (row ✅, PR #83) and log. Then start #18.

**What exists on `main` now:**
- `pkg/engine` (public contracts, stdlib only): `NodeID`, `Node`, `Edge`, `Graph` (immutable, indexed), `ExecutionID`; `Status` + `CanTransition`; `StateMutation`; immutable `NodeState`/`GraphState` (`Apply`/`WithStatus`, versions); `NodeInterface` + `NodeContext` + `BaseNode`; error contract (`ErrorClass`, `NodeError`, `ClassOf`, `IsRetryable`, `IsCritical`); injected `Registry` (strict typed config decoding); validation errors (`ErrorCode` ×16, `ValidationIssue`, `ValidationErrors`, `ErrInvalidDefinition`, `Path`/`PathRoot`).
- `internal/graph`: DAG utilities (`Roots`, `Leaves`, `Upstream`, `Downstream`, `FindCycle`, `TopologicalSort`, `ErrCycle`/`CycleError`); `schema_test.go` (checks the schema, YAML≡JSON example, and instantiates every example node through the real registry).
- `internal/nodes`: `RegisterDefaults(reg, Dependencies)` with `passthrough`, `transform`, `conditional_branch`, `http_call`, `wait_for_signal`.
- `schemas/graph/v1/`: `schema.json` (draft 2020-12, `apiVersion: graph/v1`), `example.yaml` ≡ `example.json`, README.
- NOT yet on main (in PR #83): `pkg/engine/definition.go` (`GraphDefinition` model, `Format`, `DefinitionParser`, `(*GraphDefinition).ToGraph()`), `internal/graph` parser + binder + golden tests.

**Warnings carried over:**
- The compose stack (#73) was **never started** (no working Docker on the orchestrator Mac). The human should run `make up` and confirm all five services go healthy; the Temporal healthcheck is the likeliest to need a tweak.
- Branch protection on `main` is not enabled (the human's call). CI (`Lint and Unit Tests`, `Integration Tests`; tests run with `-race`) is live but has caught none of the design problems listed in §7; diff review has.
- **#20 is blocked on a human decision** (question posted in #67 under the Epic #8 summary; options CEL via cel-go (recommended) / hand-written grammar / JSONPath-style). Do not start #20 without a recorded answer; #18 and #19 do not need it.

## 5. Decisions recorded
| Date | Decision | By |
|---|---|---|
| 2026-10-03 | Language: **Go** | human |
| 2026-10-05 | Repo: `gargi-indic/gargi-ckg` (Jules already connected); `gishnucodes/gargi-ckg` archived | human |
| 2026-10-05 | Integration with Jules is through the **API key**, not the GitHub label or CLI | human |
| 2026-10-05 | **Orchestrator merges** Jules PRs after review (squash, delete branch) | human |
| 2026-10-05 | Plans need approval (`requirePlanApproval: true`), `AUTO_CREATE_PR`, one session at a time | orchestrator |
| 2026-10-05 | Epic 1 order #2 → #3 → #5 → #7 → #4 → #6 (CI early so later PRs are machine-checked) | orchestrator |
| 2026-10-05 | #6 DI = **manual constructor wiring** in `cmd/*`, no framework; record as ADR in #7 | orchestrator default (human may override) |
| 2026-10-05 | Go module path `github.com/gargi-indic/gargi-ckg`, `go 1.23` | orchestrator (via #2 review) |
| 2026-10-05 | Dependency rules: `pkg/engine` = public contracts only; `internal/graph` and `internal/reactive` never import `internal/{api,orchestrator,store}`; only `cmd/*` wires things. Enforced by depguard (#3) | orchestrator (via #2 review) |
| 2026-10-05 | golangci-lint **v2**, pinned, installed to `./bin` | orchestrator (via #3 plan) |
| 2026-10-05 | Config: `gopkg.in/yaml.v3` + explicit `GARGI_*` env overrides (no viper); precedence defaults < YAML < env; `store.driver` exact match `memory\|redis\|postgres\|neo4j`, fail fast | orchestrator (via #6 review) |
| 2026-10-05 | ADRs in `docs/adr/` (0001 Go, 0002 package boundaries, 0003 error model, 0004 manual DI wiring) | orchestrator (via #7) |
| 2026-10-05 | Epic #8 order #9 → #13 → #12 → #10 → #11 → #14 | orchestrator |
| 2026-10-05 | **Default for Epic #8 (human may override):** public domain value types and `NodeInterface` live in `pkg/engine` (the public contract references them); algorithms/parsing in `internal/graph`. Check each plan against ADR-002 | orchestrator |
| 2026-10-06 | Epic #8 designs (human may override): reference nodes in `internal/nodes`; hooks `OnInit`/`EvaluateReactivity`/`Execute`/`OnSuccess`/`OnFailure` with `NodeContext`; only `Execute` returns a `*StateMutation` and the engine stamps `NodeID`/`ExpectedVersion` and owns every status transition; transition table (Pending→Ready\|Skipped, Ready→Running\|Skipped, Running→Succeeded\|Failed, Succeeded\|Failed→Compensating, Compensating→Compensated; terminal Skipped/Compensated); error classes retryable/non-retryable/critical, unclassified = non-retryable; `Registry` injected, no globals, `DisallowUnknownFields`; `wait_for_signal` via injected `SignalSource` (Temporal implementation in Epic #29) | orchestrator |
| 2026-10-06 | Epic #15 order #16 → #21 → #17 → #18 → #19 → #20; `condition` is an opaque string until #20 is decided | orchestrator |
| 2026-10-06 | Epic #15 designs (human may override): schema `graph/v1` (JSON Schema 2020-12, strict, `$id` URN); nodes reference retry policies and compensation handlers **by name**; compensation handlers are NOT DAG nodes (top-level `compensations`, run in reverse completion order after a critical failure); `failurePolicy` = `onNodeFailure` (fail_graph\|continue), `onCriticalFailure` (compensate\|fail_graph); `condition` and `dataMapping` source paths are opaque strings until #20 | orchestrator |
| 2026-10-06 | Validation errors (#21): 16 stable lower_snake_case `ErrorCode`s (additions only), `ValidationIssue{Path,Code,Message}`, collector `ValidationErrors` reporting all issues, JSONPath-style `Path`; the gRPC `InvalidArgument`/`BadRequest` and HTTP 400 mapping is documented only and belongs to Epic #48 | orchestrator |
| 2026-10-06 | Parser (#17): public **unvalidated** `GraphDefinition` model in `pkg/engine` (so #18/#19 can report all problems), parser/binder in `internal/graph`; JSON via token stream (duplicate keys rejected), YAML single document, aliases rejected (after review), all numbers `float64`; the parser checks SHAPE AND TYPES ONLY (value rules are #18); limits 4 MiB / depth 64 | orchestrator |

## 6. Review checklists for the rest of Epic #15
Every PR must stay within its issue's scope, leave `.orchestrator/` and `.claude/` untouched, contain no secrets, meet every acceptance criterion in its issue, and have **both CI checks green** (`gh pr checks <n>`). `go.mod` changes need justification (stdlib/yaml.v3 only unless decided). Keep to ADR-002/003/004 and the depguard rules; `pkg/engine` stays contracts-only and must not import `internal/*` (or yaml).

- **#17 parsers (PR #83): see the resume point in §4.** Final check: JSON and YAML example models are `reflect.DeepEqual`, goldens match, no alias expansion, issues deterministic and path-correct, `ToGraph` pointer API, go.mod/go.sum untouched.
- **#18 structural validation** (`internal/graph`, input = unvalidated `*engine.GraphDefinition`, output = `*engine.ValidationErrors`, ALL errors at once, deterministic order): unique node IDs (`duplicate_id`), no dangling edges (`dangling_edge`), no self-loops (`self_loop`), no duplicate edges, acyclicity via `FindCycle` (`cycle`, path in the message), at least one root and one leaf (`no_root`/`no_leaf`), `nodes` non-empty. **It must also enforce the schema VALUE rules the parser deliberately skips** (the parser checks shape/types only): id/type/name patterns (`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`, `^[A-Za-z][A-Za-z0-9_.-]*$`), retry-policy ranges (`maxAttempts>=1`, `initialIntervalMs>=1`, `backoffCoefficient>=1`, `maxIntervalMs>=1`), `failurePolicy` enums, `condition` length 1..4096, `dataMapping` non-empty keys/values (`invalid_value`). Fetch `Graph`/definition accessors once per node (hot loops). Do not duplicate parser checks.
- **#19 semantic validation:** unknown node types via `Registry.Has` (`unknown_node_type`); node/compensation config validated through the registry's strict decoding WITHOUT instantiating side-effectful nodes if avoidable (`invalid_node_config`; see `Registry.Instantiate` and the example test, which instantiates every example node); `retryPolicy` and `compensation` references must exist (`unresolved_reference`); compensation handlers' types are registry types; unused policies/handlers are not errors. Errors via #21; the registry is injected, never global.
- **#20 expression language:** BLOCKED on the human's decision (see #67). Conditions must be deterministic and side-effect free, type-checked at validation time; ADR required; a new dependency (cel-go) needs the human's explicit approval.

When Epic #15 is complete: close #15, post a summary on #67 and add the next epic's build-order table (suggested: #22 Reactive Event System & Scheduler; check dependencies between epics before fixing the order). Remaining epics: #22, #29, #39, #48, #56, #61 (observability, human to confirm keep/drop).

## 7. Lessons learned (read before reviewing anything)
1. **Jules over-scopes plans.** In #2 its plan defined interfaces that belonged to #10/#40/#16. Always compare the plan with the issue's scope.
2. **Jules reports steps as done without following them.** In #3 its notes said "verified", but the PR contradicted the approved plan (v1 instead of v2). Review the actual diff: versions, schemas, globs.
3. **Jules can confidently assert false facts** ("golangci-lint v2 doesn't exist"). Correct it with specifics and tell it to stop and ask rather than silently change direction.
4. **No local verification is possible** (CI now covers build/lint/test). The orchestrator Mac has **no Go toolchain**, and **Docker isn't responding** (`docker info` hangs). Until CI (#5) is merged, review by reading the diff. **The Jules API does not expose command output** (activity artifacts are only `changeSet`), and Jules's progress notes aren't evidence. When proof is needed (e.g. lint/test results, a rule firing), `message` Jules asking it to reply in chat with the raw terminal output. Read it from the `agentMessaged` activity and copy it into a PR comment. Don't install tooling on the human's machine without asking.
5. GitHub blocks `--request-changes` on these PRs because Jules opens them under the human's account. Use `gh pr comment` and send the same text with `jules.py message`.
6. `jules.py wait` returns immediately while a session sits in `AWAITING_PLAN_APPROVAL`. After a `message`, use `wait-reply` (for plans and answers) or `wait-push` (for PR fixes).
7. Jules works from the `main` commit it started on. Don't push changes to `main` that touch files Jules is editing mid-task; `.orchestrator/` and `.claude/` are safe.
8. Each Jules session takes ~5–35 min. Run waits in the background and never start a second session while one is open.
9. **Jules pins GitHub Action versions from stale memory.** In #5 it chose `actions/cache@v4.1.2`, which GitHub had deprecated, so the first CI run failed at "Set up job". Verify pinned tags exist (`gh api repos/<owner>/<repo>/git/ref/tags/<tag>`) and look at the first CI run. Same for Docker image tags (`curl https://hub.docker.com/v2/repositories/<repo>/tags/<tag>`).
10. **Jules re-asks for approval.** After you `approve` a plan it sometimes posts "Does this plan look good?" and parks in `AWAITING_USER_FEEDBACK`. Answer with a short `message` ("approved, proceed"), then `wait` again.
11. **`jules.py wait` can return at once** if it reads the stale `AWAITING_PLAN_APPROVAL` state right after an approval. Re-check with `get` and restart the wait.
12. **Jules plans over-reach and reinvent things that already exist.** In #6 it planned a duplicate ADR, a Store interface plus dummy adapters (belongs to #40), and viper. Always compare the plan to what is already on `main` (`docs/adr/`, existing packages) as well as to the issue.
13. **Plan rounds are the cheap place to fix things.** Every task so far needed plan v2; most also needed one PR fix round. Put exact versions, file lists, scope limits and "do not touch" lists in the first correction message.
14. **Jules claims verification it didn't do.** In #4 it could only run `docker compose config`. Ask it to state what it did and didn't run, and flag unverified runtime behaviour to the human in #67.
15. A run of `sleep N && ...` in the Bash tool is blocked; use `run_in_background` for the helper waits and the Monitor tool (`until ...; do sleep 5; done`) to wait for CI.
16. **Put concrete signatures, semantics and an explicit do-not-add list in the plan-rejection message.** For #12 this produced a PR needing no fix round. When Jules musings leak into a plan (as in #14's wait-for-signal), decide the design yourself and tell it not to reopen it.
17. **Review against the layer above, not just the issue text:** what the engine stamps (`ExpectedVersion`), what a valid mutation is (effect-less mutations are rejected), who owns status transitions. #14's nodes violated several.
18. **`Graph` accessors (and `Registry.Types`) return fresh slices by design.** Fetch once per node, not inside inner loops (#13's FindCycle was O(d²) until fixed).
19. **Jules adds stray `// Package engine` comments to new files** (#11) and examples that are not compiled. Require exactly one package comment (doc.go) and runnable `Example` functions.
20. **Nodes/instances must not write to their own config in `Execute`** (data race on shared instances); `-race` only catches it if a test executes concurrently, so request one.
21. **Jules' completion message usually has no command output**; rely on CI and on diff review, and ask for raw output in the plan-approval message anyway.
22. **Approving a plan and sending binding clarifications in the same breath works** (approve, then `message`); Jules folds them into a v2.1 plan.
23. **Table edits in the #67 body:** `str.replace` on a row template can hit every occurrence (an early edit corrupted the Epic #1 header). Anchor on the full row and re-read the body after every edit.
24. **Verify "message sent" (and the wait started) after every `jules.py message`.** A long inline message with shell metacharacters broke once (#21's first rejection never reached Jules; it surfaced only as a failed background task). Write long messages to a file in the scratchpad and pass `"$(cat file)"`.
25. **Untrusted-input parsers need a budget for every expansion mechanism**, not just input length and depth: YAML aliases (alias bomb) in #17, plus duplicate keys that `encoding/json` silently accepts. Review parsers adversarially.
26. **Review public types for method receivers and `errors.As` targets and JSON behaviour** (#21: `Unwrap` returned values so `errors.As(*ValidationIssue)` never matched; a pointer-receiver `MarshalJSON` produced `{}` for values).
27. **Reference examples must be checked against the real implementations** (#16's example had node configs the #14 built-ins reject); keep a test that instantiates them through the registry.
28. **Errors shown to API clients must not contain Go type names** (`float64`, `map[string]interface {}`); use JSON type names.
29. **Jules's first plan is usually a bullet list of intentions; the useful lever is a complete spec in the rejection message** (files, exact signatures, error codes, test list, out-of-scope list). It keeps rounds to two (plan v2 + one PR fix round).
30. **Don't leave a background `wait` running across a handover**; record the resume command in HANDOVER §4 instead.


## 8. Chronological log (condensed; the full entries are #67's comments)
- **2026-10-03:** Spec read. 9 epics + 57 sub-issues created (first in `gishnucodes/gargi-ckg`). Language chosen: Go.
- **2026-10-05:**
  - Jules API key received and stored at `~/.config/jules/env`. Jules could only see `gargi-indic/gargi-ckg`, so the human chose to move there. Seed commit `9760dac` (README + spec) pushed, the backlog was recreated with identical numbers, and the old repo was archived.
  - `#67 Orchestrator Logs` (pinned) created, `.orchestrator/jules.py` + README committed (`944f539`), and `#68 Orchestrator Handover` (pinned) created (`1686c47`).
  - **#2:** session `14117555043668616096`. Plan v1 rejected (wrong module path, out-of-scope interfaces), plan v2 approved. PR #69 reviewed and squash-merged as `832dc75`; #2 closed and criteria ticked. Accepted nit: the `internal/*` doc comments don't cite future issues.
  - Helper: `wait-reply` added (`e3d9f9f`).
  - **#3:** session `7509376572595293231`. Plan v1 rejected (no depguard, unpinned v1 lint, fake `go:generate` lines), plan v2 approved. PR #70 has changes requested (see §4). Jules falsely claimed v2 doesn't exist, and the correction was sent.
  - Handover docs, the `wait-push` helper and the `gargi-orchestrator` skill were added (this commit).

- **2026-10-05 (cont.), second orchestrator:**
  - **#3:** PR #70 fixed (golangci-lint v2.14.0, scoped depguard, install.sh from the pinned tag) and merged `b41db29`.
  - **#5:** plan v1 rejected (every push, no go-version-file, testcontainers dependency, no `make build`); v2 approved; PR #71 failed CI on a deprecated `actions/cache`; bumped; merged `2a77554`. CI is live.
  - **#7:** plan v1 rejected (no ADR-004); v2 approved; PR #72 merged `f7d1a31`.
  - **#4:** plan v1 rejected (no tags/ports/credentials); v2 approved after verifying the image tags; PR #73 needed a Temporal healthcheck address fix; merged `3b1295f`. Runtime unverified (see §4).
  - **#6:** plan v1 rejected (duplicate ADR, store stubs, viper); v2 approved; PR #74 needed defaults, exact driver matching and strict YAML keys; merged `210f6a0`.
  - **Epic #1 closed**; Epic #8 build-order table added to #67; handover refreshed.
- **2026-10-05/06, third orchestration run (Sonnet 5.5):** fixed `jules.py` activities pagination (`40b63f4`); completed **Epic #8**: #9 (PR #75, `6be34c2`), #13 (#76, `9fa2371`), #12 (#77, `3cd1397`), #10 (#78, `f018ffd`), #11 (#79, `8479214`), #14 (#80, `846411e`). Every task: plan v1 rejected, v2 approved; five of six needed one PR fix round. Epic #8 closed; Epic #15 table and the #20 escalation posted on #67.
- **2026-10-06, same run, Epic #15 so far:** #16 (PR #81, `ed4c51a`), #21 (PR #82, `d30d45b`) merged; Epic #8 summary and the #20 escalation posted; handover refreshed (`e20c0e8`). **#17** started (session `18419591797003617012`): plan v1 rejected (parser in `pkg/engine`, plain struct decoding, no paths), plan v2 approved (with a `ToGraph` signature correction); PR #83 reviewed, **round-1 fixes requested** (alias-bomb security fix, parse-stage paths, JSON type names, int range, test gaps, tidy); Jules then pushed the fixes (PR head `43db22a`, re-review pending). Session ended here by the human; handover docs updated.


## 9. Open questions for the human
- Keep or drop Epic #61 (Observability), which isn't in the spec.
- Authentication, multi-tenancy, and graph/payload size limits aren't in the spec.
- Decisions on #20 (CEL for edge conditions), #40 (StateStore interface split), #41 (Temporal ↔ StateStore consistency), and #51 (grpc-gateway vs. hand-written REST).
- Whether to enable branch protection with required CI checks once #5 lands.
- Rotate the Jules API key.
- **Run `make up` once** and confirm all five compose services become healthy (never runtime-tested, see §4).
- Enable branch protection on `main` requiring `Lint and Unit Tests` and `Integration Tests` (CI is live; repo settings are the human's call).
- Confirm the Epic #8 designs recorded in §5 (public types and `NodeInterface` in `pkg/engine`, algorithms in `internal/graph`, reference nodes in `internal/nodes`, state transition table).
- **Decide #20** (edge condition language): CEL via cel-go (recommended) vs hand-written grammar vs JSONPath-style; adding cel-go means a new dependency outside the spec stack.
