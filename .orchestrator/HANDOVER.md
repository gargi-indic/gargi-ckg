# Orchestrator Handover: gargi-ckg

Snapshot written by the first orchestrator (Claude Code, Opus 5.5) on **2026-10-05, ~22:45 UTC**.
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

## 4. Current state (at snapshot time)
**Active epic: #1.** Build order: **#2 → #3 → #5 → #7 → #4 → #6**

| Step | Issue | Status | Jules session | PR |
|---|---|---|---|---|
| 1 | #2 Go module + layout | ✅ merged (squash `832dc75`) | `14117555043668616096` | #69 |
| 2 | #3 Makefile, golangci-lint, codegen | 🔍 **changes requested; Jules is reworking** | `7509376572595293231` | #70 (open) |
| 3 | #5 CI pipeline | ⏳ todo | | |
| 4 | #7 ADRs | ⏳ todo | | |
| 5 | #4 docker-compose stack | ⏳ todo | | |
| 6 | #6 Config + DI skeleton | ⏳ todo | | |

### Exactly where #3 stands
1. Plan v1 rejected; plan v2 approved (golangci-lint **v2**, pinned, depguard, full Makefile target list).
2. Jules opened PR #70 (head `035db2a`), but it used golangci-lint **v1.64.6** with a hybrid v1/v2 config, ran `curl | sh` from `master`, used unpinned `gofumpt@latest`, and had over-broad depguard globs. Changes were requested in a PR comment and through `message`.
3. Jules then wrongly claimed "golangci-lint v2 does not exist" and began reverting to v1. A correction was sent at ~22:45 UTC: v2 exists (since March 2025). If `go install` fails, the cause is the Go toolchain, so use the release binary through `install.sh` fetched from the **same pinned tag**. If no v2 binary can run at all, Jules should stop and say so.
4. **Next action:** `python3 .orchestrator/jules.py wait-push 7509376572595293231 70`, then review PR #70 against the checklist in §6. If Jules says v2 truly can't run, escalate to the human in #67 (options: bump `go` in go.mod, or accept v1 temporarily).

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

## 6. Review checklists for the remaining Epic 1 tasks
Every PR must stay within its issue's scope, leave `.orchestrator/` and `.claude/` untouched, contain no secrets, and meet every acceptance criterion in the issue. From #5 onward, CI must be green.

- **#3 (PR #70):** `.golangci.yml` uses the real v2 schema (`version: "2"`, `linters.default: none`, `linters.settings.depguard.rules`, `formatters.enable: [gofumpt]`). `GOLANGCI_LINT_VERSION` is a v2.x tag. There is no install script from `master` and no `@latest`. `make fmt` uses `./bin/golangci-lint fmt`. depguard `files` are scoped explicitly (`**/pkg/engine/**`; `**/internal/graph/**`, `**/internal/reactive/**`). The PR body shows real depguard failure output for both rules plus `make lint`/`make test` output. `make help` is the default target, and the Makefile works on GNU Make 3.81.
- **#5 CI:** a GitHub Actions workflow on PRs and pushes to `main`. It uses `actions/setup-go` with `go-version-file: go.mod` and runs `make tools lint test build` (the same pinned golangci-lint as the Makefile, so no separate golangci action with a different version). It has a separate integration job running `make test-integration` (`-tags=integration`; it may currently have no tests but must pass), Go module/build cache, and pinned action versions. After merge, note in #67 that the human could enable branch protection with required checks (repo settings are the human's call, so don't change them yourself).
- **#7 ADRs:** `docs/adr/` with a template. ADR-001 Go; ADR-002 package boundaries matching the README and depguard; ADR-003 error model (retryable / non-retryable / critical → compensation, consistent with #10); ADR-004 DI = manual constructor wiring (from §5). Each must be consistent with the README and must not contradict the spec.
- **#4 compose:** `docker-compose.yml` at the repo root (the Makefile's `COMPOSE_FILE` default). It runs Temporal server + UI, Redis, Postgres and Neo4j with **pinned image tags** (no `latest`) and health checks on every service. `.env.example` holds local-only credentials and no real `.env` is committed. `make up`/`make down` work, and the README documents ports.
- **#6 config/DI:** a typed config struct loaded from YAML with env overrides, and `store.driver: memory|redis|postgres|neo4j` (validated, failing fast). The composition root lives only in `cmd/*` with no globals. It follows ADR-004, has unit tests, and lint (including depguard) passes.

When Epic 1 is complete: close #1, post a summary comment on #67, and add an Epic #8 build-order table to the #67 body. The suggested order is **#9 → #13 → #12 → #10 → #11 → #14** (domain types → DAG utils → state model → NodeInterface → registry → reference nodes).

## 7. Lessons learned (read before reviewing anything)
1. **Jules over-scopes plans.** In #2 its plan defined interfaces that belonged to #10/#40/#16. Always compare the plan with the issue's scope.
2. **Jules reports steps as done without following them.** In #3 its notes said "verified", but the PR contradicted the approved plan (v1 instead of v2). Review the actual diff: versions, schemas, globs.
3. **Jules can confidently assert false facts** ("golangci-lint v2 doesn't exist"). Correct it with specifics and tell it to stop and ask rather than silently change direction.
4. **No local verification is possible yet.** The orchestrator Mac has **no Go toolchain**, and **Docker isn't responding** (`docker info` hangs). Until CI (#5) is merged, review by reading the diff and Jules's bash output (`activities`). Don't install tooling on the human's machine without asking.
5. GitHub blocks `--request-changes` on these PRs because Jules opens them under the human's account. Use `gh pr comment` and send the same text with `jules.py message`.
6. `jules.py wait` returns immediately while a session sits in `AWAITING_PLAN_APPROVAL`. After a `message`, use `wait-reply` (for plans and answers) or `wait-push` (for PR fixes).
7. Jules works from the `main` commit it started on. Don't push changes to `main` that touch files Jules is editing mid-task; `.orchestrator/` and `.claude/` are safe.
8. Each Jules session takes ~5–35 min. Run waits in the background and never start a second session while one is open.

## 8. Chronological log (condensed; the full entries are #67's comments)
- **2026-10-03:** Spec read. 9 epics + 57 sub-issues created (first in `gishnucodes/gargi-ckg`). Language chosen: Go.
- **2026-10-05:**
  - Jules API key received and stored at `~/.config/jules/env`. Jules could only see `gargi-indic/gargi-ckg`, so the human chose to move there. Seed commit `9760dac` (README + spec) pushed, the backlog was recreated with identical numbers, and the old repo was archived.
  - `#67 Orchestrator Logs` (pinned) created, `.orchestrator/jules.py` + README committed (`944f539`), and `#68 Orchestrator Handover` (pinned) created (`1686c47`).
  - **#2:** session `14117555043668616096`. Plan v1 rejected (wrong module path, out-of-scope interfaces), plan v2 approved. PR #69 reviewed and squash-merged as `832dc75`; #2 closed and criteria ticked. Accepted nit: the `internal/*` doc comments don't cite future issues.
  - Helper: `wait-reply` added (`e3d9f9f`).
  - **#3:** session `7509376572595293231`. Plan v1 rejected (no depguard, unpinned v1 lint, fake `go:generate` lines), plan v2 approved. PR #70 has changes requested (see §4). Jules falsely claimed v2 doesn't exist, and the correction was sent.
  - Handover docs, the `wait-push` helper and the `gargi-orchestrator` skill were added (this commit).

## 9. Open questions for the human (not blocking Epic 1)
- Keep or drop Epic #61 (Observability), which isn't in the spec.
- Authentication, multi-tenancy, and graph/payload size limits aren't in the spec.
- Decisions on #20 (CEL for edge conditions), #40 (StateStore interface split), #41 (Temporal ↔ StateStore consistency), and #51 (grpc-gateway vs. hand-written REST).
- Whether to enable branch protection with required CI checks once #5 lands.
- Rotate the Jules API key.
