# Orchestrator Handover: gargi-ckg

Snapshot written by the first orchestrator (Claude Code, Opus 5.5) on **2026-10-05, ~22:45 UTC** and refreshed at the end of **Epic #1** by the second (Claude Code, Sonnet 5.5) on **2026-10-05, ~23:55 UTC**.
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

## 4. Current state (end of Epic #1)
**Epic #1 is complete and closed.** No Jules session is running and no PR is open. The next orchestrator starts **Epic #8**; build order **#9 → #13 → #12 → #10 → #11 → #14** (table and rationale are in the #67 body).

| Epic #1 step | Issue | Status | Jules session | PR / commit |
|---|---|---|---|---|
| 1 | #2 Go module + layout | ✅ | `14117555043668616096` | #69 `832dc75` |
| 2 | #3 Makefile, golangci-lint v2.14.0, depguard | ✅ | `7509376572595293231` | #70 `b41db29` |
| 3 | #5 CI (GitHub Actions) | ✅ | `2953791257826166915` | #71 `2a77554` |
| 4 | #7 ADRs 0001–0004 | ✅ | `9997667611197910973` | #72 `f7d1a31` |
| 5 | #4 docker-compose stack | ✅ (see warning) | `17982997575160836669` | #73 `3b1295f` |
| 6 | #6 Config loading + DI skeleton | ✅ | `1613978973304618491` | #74 `210f6a0` |

**What exists on `main` now:** Go module `github.com/gargi-indic/gargi-ckg` (go 1.23), `cmd/{server,worker}` (load and validate config, log the store driver, TODOs for #40/API), `internal/config` (yaml.v3, `GARGI_*` env overrides, fail-fast `Validate`), `config/config.example.yaml`, Makefile (`help tools build test test-integration lint fmt generate up down`), `.golangci.yml` (v2, depguard), `.github/workflows/ci.yml` (jobs `Lint and Unit Tests`, `Integration Tests`), `docker-compose.yml` + `.env.example` + `scripts/init-temporal-db.sh`, `docs/adr/`.

**Next action for the successor:** boot sequence, then `python3 .orchestrator/jules.py start 9` and update the #67 tables.

**Warnings carried over:**
- The compose stack (#73) was **never started**: Docker doesn't work on the orchestrator Mac and Jules' sandbox couldn't pull the Neo4j/Temporal images. Only `docker compose config` ran. The human should run `make up` once and confirm all five services go healthy. The Temporal healthcheck (`temporal operator cluster health --address "$$(hostname -i):7233"`) is the likeliest to need a tweak. Treat any failure as a follow-up fix.
- CI is now the machine check for every PR. Branch protection on `main` is not enabled (the human's call).

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

## 6. Review checklists for Epic #8
Every PR must stay within its issue's scope, leave `.orchestrator/` and `.claude/` untouched, contain no secrets, meet every acceptance criterion in its issue, and have **both CI checks green** (`gh pr checks <n>`). Run `go.mod` through a quick eye: new dependencies need justification. Keep to ADR-002/003/004 and the depguard rules.

- **#9 domain types:** immutable, transport-agnostic Graph/Node/Edge/ExecutionID with constructors enforcing invariants; no imports of Temporal, storage or API packages; unit tests for constructors/invariants. No NodeInterface, StateStore or state types (those are #10/#40/#12).
- **#13 DAG utilities:** cycle detection returns the offending path; deterministic (stable) topological order; roots/leaves/upstream/downstream; benchmarks for 10k nodes; lives in `internal/graph`; operates on #9's types; no graph definition parsing (that's Epic #15).
- **#12 state model:** GraphState/NodeState with the 8-status enum (Pending, Ready, Running, Succeeded, Failed, Skipped, Compensating, Compensated); StateMutation patch type; monotonic version for optimistic concurrency; transition table enforced and unit-tested (all valid and a sample of invalid transitions). No storage code.
- **#10 NodeInterface:** `OnInit`, `Execute`, `OnSuccess`, `OnFailure`, `EvaluateReactivity` with `context.Context` and a node execution context; godoc maps each hook to the spec name (on_init, execute, ...); lifecycle order documented; `BaseNode` with no-op defaults; error contract implementing ADR-003 (retryable / non-retryable / critical → compensation). In `pkg/engine`, no imports of `internal/*`.
- **#11 registry:** `Register(typeName, factory, configSchema)`; duplicate registration is an error; typed config decoding from definition JSON; registry is injected, never a package-level global (ADR-004).
- **#14 reference nodes:** passthrough/transform, conditional branch, HTTP call (timeout + retryable status classification), wait-for-signal; each implements NodeInterface and is registered by default (through the injected registry, not `init()` globals); unit tests for each; no real network in tests.

When Epic #8 is complete: close #8, post a summary on #67, and add the next epic's build-order table. Suggested next: Epic #15 (Graph Definition & Validation, #16–#21; **#20 is needs-discussion, escalate**), then #22 (Reactive Event System). Check dependencies between epics before fixing the order.

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

## 9. Open questions for the human
- Keep or drop Epic #61 (Observability), which isn't in the spec.
- Authentication, multi-tenancy, and graph/payload size limits aren't in the spec.
- Decisions on #20 (CEL for edge conditions), #40 (StateStore interface split), #41 (Temporal ↔ StateStore consistency), and #51 (grpc-gateway vs. hand-written REST).
- Whether to enable branch protection with required CI checks once #5 lands.
- Rotate the Jules API key.
- **Run `make up` once** and confirm all five compose services become healthy (never runtime-tested, see §4).
- Enable branch protection on `main` requiring `Lint and Unit Tests` and `Integration Tests` (CI is live; repo settings are the human's call).
- Confirm the Epic #8 default: public types and `NodeInterface` in `pkg/engine`, algorithms in `internal/graph`.
