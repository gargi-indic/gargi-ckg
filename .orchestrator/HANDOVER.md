# Orchestrator Handover: gargi-flow

Snapshot refreshed on **2026-10-23**, after **Epic #124 (Simulation P0) completed** (11/11 merged, #168–#179). Next: **Epic #136 (Simulation P1)**, starting with #137.
The **live** state is in issue **#67** (body tables + latest comments) and on `main`. #67 overrides this file wherever they differ.

---

## 1. What this project is
- **Engine (done):** **gargi-flow** is a Go middleware for reactive, durable DAG execution. It runs on Temporal, with pluggable stores (memory/Redis/Postgres/Neo4j), gRPC/REST/SSE/WebSocket APIs, and a React Flow canvas in `web/`. Spec: [`docs/SPEC.md`](../docs/SPEC.md).
- **Product being built now:** a **simulation and decision-optimization engine** on top of the same graph model. The idea is to model a decision process once, simulate it across uncertain worlds without Temporal, find the best policy, then deploy it on the engine. Architecture: [`docs/simulation/ARCHITECTURE.md`](../docs/simulation/ARCHITECTURE.md). Boundaries: [`docs/adr/0009-simulation-engine.md`](../docs/adr/0009-simulation-engine.md). Reference prototype for the golden numbers: [`docs/simulation/prototypes/loan_sim.py`](../docs/simulation/prototypes/loan_sim.py).
- **How it is built:** the orchestrator agent assigns sub-issues one at a time to **Jules** (Google coding agent, via `.orchestrator/jules.py`), or to a **Claude Code sub-agent** for `agent/claude` issues and Jules failures. It reviews plans and PRs, squash-merges into `main`, and logs every step in #67.

## 2. Where everything lives
| Thing | Location |
|---|---|
| Repo | `gargi-indic/gargi-flow` (default branch `main`) |
| Local clone | `/home/gishnum/code/gargi-ckg` (WSL `Ubuntu-24.04`; Go 1.26 installed; pinned lint at `bin/golangci-lint-v2.14.0`) |
| Live log + status tables | Issue **#67** |
| Jules helper | `.orchestrator/jules.py` |
| Resume skill | `.claude/skills/gargi-orchestrator/SKILL.md` |
| Credentials | `~/.config/jules/env` (`GH_TOKEN`, `JULES_API_KEY`). Run `source` on it in the same shell as `gh`/`jules.py`. **Never** print or commit it. |
| Claude sub-agent worktrees | `.claude/worktrees/agent-*` (harness-managed, untracked; do not commit) |

## 3. Backlog map
- All original backend epics (#1, #8, #15, #22, #29, #39, #48, #56, #61) and UI Epic #101 are complete and closed.
- **Simulation epics** (created 2026-10-22, label `area/simulation`; each sub-issue is labelled `agent/jules` or `agent/claude`):

| Epic | Scope | Sub-issues | Status |
|---|---|---|---|
| #124 P0 | kernel + loan golden case | #125–#135 | ✅ complete |
| **#136 P1** | solvers, reports, API, MCP, canvas | #137–#147 | **active**, starts at #137 |
| #148 P2 | queues/resources, optimizers, agents | #149–#158 | ready |
| #159 P3 | deploy, outcomes, calibration, drift | #160–#167 | ready |

### Epic #124 build order
| Step | Issue | Agent | Status | PR / commit |
|---|---|---|---|---|
| 1 | #125 ADR-0009 + depguard rules | Claude | ✅ | #168 `234b768` |
| 2 | #126 `pkg/sim` public contracts | Jules | ✅ | #169 `85f30b9` |
| 3 | #127 sim/policy/experiment schemas + strict parsers + loan files | Jules | ✅ | #170 `47fe9b5` |
| 4 | #128 `decision` node (sim registry only) | Jules | ✅ | #171 `2349674` |
| 5 | #129 model compilation + CEL envs + info-set enforcement | Jules ×2 → **Opus sub-agent** | ✅ | #173 `537784f` (supersedes #172) |
| 6 | #130 behaviors + samplers (Monte Carlo + enumerating) | Jules | 🧭 planning | session `11182749359574682895` |
| 7 | #131 kernel mode A | Jules | ⏳ | |
| 8 | #132 experiment runner + Monte Carlo evaluator | Jules | ⏳ | |
| 9 | #133 exact evaluator + exhaustive policy search | Claude | ⏳ | |
| 10 | #134 `gargi` CLI (`--local`) | Jules | ⏳ | |
| 11 | #135 loan golden test, benchmark, quickstart | Claude | ⏳ | |

**P0 exit criterion:** `gargi sim run examples/simulation/loan/experiment.yaml --local` reproduces exactly 42 policies, best EV **460.0** (base) and **202.5** (recession), with Monte Carlo (200k runs, seed 42) within 3 SE.

## 4. Current state and immediate resume steps
1. **P0 is done.** Exit criterion verified: `gargi sim run examples/simulation/loan/experiment.yaml --local` gives exact EV 460.0 (base) and 202.5 (recession); Monte Carlo 464.0 / 201.0 within 3 SE. **The full run takes ~13 min on 16 threads and the human explicitly accepted that** (do not shrink the example). `TestLoanGolden` runs in CI (bounded: exact for 42×2, 200k MC only for the top policy per world; 20k under `-race`).
2. **Next task: #137** (backward-induction exact solver, Claude sub-agent). Epic #136's build-order table is in the #67 body. Claude tasks may run in parallel once dependencies are merged; Jules tasks run one at a time.
3. **#144 (MCP server) is `needs-discussion`** (SDK dependency): ask the human in #67 before starting it. Other open ones: #152, #153, #158 (later epics).
4. **Dispatch recipe that worked for `agent/claude` tasks:** spawn an Opus sub-agent (`model: opus`, worktree isolation) with the full issue text plus code pointers; it commits locally and writes the PR body to the scratchpad. Orchestrator then: detached-worktree verify (`go build ./...`, `go test -race -count=1 ./internal/sim/... ./pkg/sim/... ./cmd/gargi/...`, `bin/golangci-lint-v2.14.0 run ./...`, scope via `git diff --stat main`), `git push origin <branch>`, `gh pr create`, wait for 4 green checks, `gh pr merge --squash --delete-branch`, tick the issue boxes, edit the #67 body, post a log comment.
5. **Tooling gotcha:** `gh issue view` fails with a Projects (classic) deprecation error; use `gh api repos/gargi-indic/gargi-flow/issues/N` (`--jq .body`) and `gh api -X PATCH ... -F body=@file` instead. `source ~/.config/jules/env` in the same shell as `gh`.

## 5. Decisions recorded (also in the #67 body)
- **Human (2026-10-22):**
  - Build the simulation engine as Epics #124/#136/#148/#159, P0 first.
  - **License deferred**; do not add a LICENSE file.
  - **Jules failure fallback:** if Jules fails a task (session `FAILED`, timeout, or two attempts that miss required functionality), reassign it to an **Opus Claude sub-agent** (`model: opus`, worktree isolation) instead of escalating. Other escalations (needs-discussion, spec/interface changes, destructive actions) still go to the human.
- **Human (2026-10-24), supersedes the 2026-10-23 "P1 Jules tasks straight to Opus":**
  - **Jules first,** with large issues split into small sub-issues for early results (#143 → #185/#186/#187).
  - **Adaptive check-ins** at 5 → 10 → 15 min (never more than 15).
  - **Fallback:** after **2 consecutive check-ins with no response** from Jules, or a `FAILED` session, reassign to a Claude sub-agent sized to the task: Haiku for mechanical work, Sonnet for moderate single-package work, Opus for spec-heavy or cross-cutting work.
  - #142 stayed with the Opus sub-agent already working on it.
- **P0 defaults (orchestrator, open to override; also in ADR-0009 §7):**
  - The `decision` node is registered in the sim registry only; production registration is P3.
  - Abandonment ends the episode.
  - Constraints are per-run boolean.
  - No retries in simulation.
  - No new dependencies (stdlib `math/rand/v2`, stdlib `flag`, existing `cel-go`).
  - CEL uses the engine status strings (`'Succeeded'`).
- **Policies cannot read `world`** (orchestrator default for P0): rules see only the decision's observed node paths.
- **Simulation error codes** live in `pkg/sim/codes.go` (snake_case `engine.ErrorCode`), never in `pkg/engine`.
- **Open `needs-discussion` items** (later epics): #144 MCP SDK dependency, #152 TPE in-house vs library, #153 agent provider and spend limits, #158 mode C now or later.

## 6. Review checklists for the remaining P0 tasks
- **All tasks:**
  - Only the files in scope.
  - No scratch scripts (`fix_*.py` and the like).
  - Godoc on exported identifiers.
  - The depguard `simulation`/`sim_contracts` rules hold.
  - **Check out the PR locally and run `bin/golangci-lint-v2.14.0 run ./...` and `go test -race` yourself.** Jules has claimed lint passed when it had not.
- **#131 kernel:**
  - Mirror the `internal/orchestrator/workflow.go` step loop (`EvaluateStep` → `Running` via `state.Apply` → execute → completion/failure → `detector.AssessState`) without importing the orchestrator.
  - Virtual-time heap ordered by `(time, nodeID)`.
  - Pure nodes run their real implementation; effectful nodes run behaviors; decisions run the policy via `nodes.RegisterDecision`.
  - Abandon ends the episode.
  - `TestSim_NoWallClockAudit`.
  - The three `examples/*.yaml` graphs reach the expected statuses.
- **#132 runner:**
  - Byte-identical report JSON with 1 vs 8 workers.
  - Common random numbers: `sim.Seed(seed, world, r)` is the same for every policy.
  - Loan + `recession-v1`, 200k runs, mean within 3 SE of 202.5.
- **#133 exact/exhaustive (Claude):**
  - Exactly **42** reduced policies.
  - Exact EVs match `loan_sim.py` to 1e-9.
  - The emitted best policy re-parses and evaluates to the same EV.
- **#134 CLI:**
  - stdlib `flag`.
  - `--local` prints EV 460.0 / 202.5.
  - Without `--local`, exit 2.
- **#135 golden (Claude):**
  - Golden JSON for 42×2 policies.
  - Regret 267.5 / 142.2 and VOI 158.0 / 130.3.
  - Benchmarks recorded.
  - Quickstart verified verbatim.

## 7. Lessons learned (this session)
1. **Jules state lags its messages by up to ~30 min.** `jules.py wait` returns immediately while the state is `AWAITING_USER_FEEDBACK`. Poll for **both** a state change and a new `agentMessaged` activity.
2. **Jules repeats "please approve" after approval, and checks in mid-task.** Right after `approve`, send a "proceed with no check-ins" message that restates the key requirements.
3. **Jules' first plans are often vague.** Give exact specs up front (file names, signatures, exact YAML contents, test lists). On #127 this cut the rounds to one.
4. **Jules skips lint and commits scratch scripts** (#127, #129). Always verify locally, and say "no scratch scripts" in every proceed message.
5. **Verify behavior with your own adversarial probe tests** on a local checkout. On #129 this caught an information-set prefix leak (`band` vs `bandit`) and uncompiled CEL params that Jules' tests missed. Handing the verified regression test file to the next agent works well.
6. **Claude sub-agents:**
   - `gh` is not authenticated in their worktree, and **`git push` fails** because the Windows Git Credential Manager is unreachable from the agent context.
   - Paste the full issue and epic text into the brief.
   - Have the sub-agent write the PR body to the scratchpad. The orchestrator then reviews the commit, pushes from the main clone, and opens the PR.
7. **A branch checked out in an agent worktree** cannot be added to another worktree. Use `git worktree add --detach <path> <branch>` to verify it.
8. **Known flaky tests** (pre-existing; follow-ups): `internal/orchestrator` `TestGraphExecutionWorkflow_Linear_Diamond` ("unknown queryType graph_state") and `cmd/server` `TestRun_Server` (data race, goroutine at `cmd/server/main.go:110`). Rerun before concluding that a PR broke them.
9. **Before merging, edit the #67 body** (fetch it first): the Current status table plus the build-order row. Then post one comment per event, starting `**YYYY-MM-DD · #N <event>.**`.
10. **Jules reliability on this epic:** Jules delivered #126, #127, #128, #130 but FAILED #129 (twice), #132 and #134 (sessions ended `FAILED` or produced nothing for hours). Before relying on Jules, send a full spec up front; for spec-heavy tasks prefer a Claude sub-agent. **A mid-work Jules session does not honor "stop"**: it overwrote a sub-agent's fixes on #175 with a later push. Push sub-agent fixes only when the Jules session is `COMPLETED`/`FAILED`; if overwritten, restore the verified tree with a normal follow-up commit (never force-push).
11. `jules.py wait` returns immediately while the state is `AWAITING_USER_FEEDBACK` even when Jules is still working; use `wait-reply` for long waits. Background waits time out at 40 min while the session is still `IN_PROGRESS`; just restart them.
12. **Lint is the usual Jules gap** (gofumpt, errcheck) and audit-style tests may be vacuous: always probe them (e.g. add a `time.Now` file and confirm `TestSim_NoWallClockAudit` fails).
