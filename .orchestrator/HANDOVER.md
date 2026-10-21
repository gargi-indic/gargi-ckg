# Orchestrator Handover: gargi-flow

Snapshot refreshed on **2026-10-21** during the execution of **Epic #101 (UI Prototype: Visual DAG Canvas with Live Execution)**.
The **live** state is in GitHub issues and repository commits on `main`.

---

## 1. What this project is
- **Product:** A pluggable Reactive Graph Engine middleware in **Go** named **gargi-flow** (*"Reactive and durable graph execution"*). It runs DAGs on Temporal, with pluggable state stores (in-memory, Redis, Postgres, Neo4j) and gRPC/REST/SSE/WebSocket APIs, alongside a visual React Flow execution canvas in `web/`. Requirements are in [`docs/SPEC.md`](../docs/SPEC.md).
- **How it is built:** An orchestrator agent assigns backlog sub-issues **one at a time** to **Jules** (Google coding agent) through the Jules API. It reviews each plan and PR, squash-merges into `main`, and logs every step in #67.

## 2. Where everything lives
| Thing | Location |
|---|---|
| Active repo | `gargi-indic/gargi-flow` (default branch `main`) |
| Local clone (orchestrator environment) | `/home/gishnum/code/gargi-ckg` (WSL `Ubuntu-24.04`) |
| Requirements spec | `docs/SPEC.md` |
| Live log + status tables | Issue **#67 Orchestrator Logs** (pinned) |
| Successor onboarding | Issue **#68 Orchestrator Handover** (pinned) |
| Jules API helper | `.orchestrator/jules.py` (Python 3 client with pagination and backoff) |
| Resume skill | `.claude/skills/gargi-orchestrator/SKILL.md` |
| Credentials | `~/.config/jules/env` in WSL (`GH_TOKEN`, `JULES_API_KEY`). **Never** print or commit these keys. |
| Jules source id | `sources/github/gargi-indic/gargi-flow` |
| Jules web UI | `https://jules.google.com/session/<session-id>` |

## 3. Backlog Map & Epic Status
All original backend epics (#1, #8, #15, #22, #29, #39, #48, #56, #61) are fully implemented, merged, and closed.

### Active Epic: #101 `[Epic] UI Prototype: Visual DAG Canvas with Live Execution`
| Sub-issue | Title | PR | Status | Commit |
|---|---|---|---|---|
| #102 | [Backend] Add configurable CORS middleware to REST/SSE gateway | #113 | ✅ Merged | `0cff997` |
| #103 | [Backend] Stream terminal node transitions over SSE | #114 | ✅ Merged | `7855b07` |
| #104 | [Backend] Add GET /v1/executions list endpoint (paginated) | #115 | ✅ Merged | `aaaa7c3` |
| #105 | [UI] Scaffold web/ app (Vite + React + TS + React Flow) | #116 | ✅ Merged | `ad396a5` |
| #106 | [UI] Render GraphDefinition as an auto-laid-out DAG | #117 | ✅ Merged | `eb877f1` |
| #107 | [UI] Run execution and animate node status live via SSE | #118 | ✅ Merged | `75996fc` |
| #108 | [UI] Node inspector drawer (input, output, error, timing) | #119 | ✅ Merged | `4e1d5c7` |
| #109 | [UI] Experiments gallery + recent executions sidebar | #120 | ✅ Merged | `adb0474`|
| #110 | [UI] Minimal graph authoring / node editing | #121 | ✅ Merged | `1167557` |
| #111 | [UI] Trigger human-in-the-loop signals from node inspector | - | ⏳ Todo | Next after #110 |
| #112 | [UI] Documentation & unified dev-up script | - | ⏳ Todo | Final task in Epic #101 |

---

## 4. Current State & Immediate Resume Instructions

### Active Task: Issue #109 (`[UI] Experiments gallery + recent executions sidebar`)
- **Active Jules Session:** `16366454225729211062`
- **Jules URL:** https://jules.google.com/session/16366454225729211062
- **Work Delivered in Session so Far:**
  - Added `ListExecutions` RPC to Protobuf (`proto/gargi/v1/api.proto`) with HTTP GET `/v1/executions`.
  - Implemented `ListExecutions` handler in `internal/api/service.go`.
  - Added tests in `internal/api/service_test.go` and `internal/api/http_server_test.go`.
- **Latest Interaction / Guidance Sent:**
  - Jules asked about seeding `memory.Store` in unit tests and why `ListExecutions` returned 0 items.
  - Provided guidance:
    1. In `service.go`, if `req.Status == EXECUTION_STATUS_UNSPECIFIED`, set `filter.Status = ""` (do not filter out all items).
    2. Seed `memory.Store` directly with `SetState` in unit tests (since `SubmitExecution` delegates to Temporal).
    3. Pass `expectedVersion = 0` when calling `SetState` on new records.
- **Immediate Next Action for Successor:**
  1. Load environment: `source ~/.config/jules/env && cd /home/gishnum/code/gargi-ckg`.
  2. Check session status: `python3 .orchestrator/jules.py get 16366454225729211062`.
  3. Wait for Jules to complete implementation: `python3 .orchestrator/jules.py wait 16366454225729211062 1800` (run in background).
  4. When PR is opened:
     - Review diff (`gh api repos/gargi-indic/gargi-flow/pulls/<PR> -H "Accept: application/vnd.github.v3.diff"`).
     - Check CI checks: `gh pr checks <PR> -R gargi-indic/gargi-flow` (all 4 jobs: Docker, Integration, Lint/Unit, UI Lint/Build).
     - Merge: `gh pr merge <PR> -R gargi-indic/gargi-flow --squash --delete-branch`.
     - Update Issue #109 checkboxes (`- [x]`) and close.
     - Pull local main: `git pull --rebase`.
     - Log merge comment on Issue #67.
  5. Dispatch Task #110 (`python3 .orchestrator/jules.py start 110`).

---

## 5. Review Checklists & Key Architecture Decisions
- **Frontend Stack in `web/`:** Vite + React 19 + TypeScript + `@xyflow/react` + `dagre` (top-to-bottom layout) + `js-yaml` + `openapi-fetch`.
- **Backend Streaming Endpoint:** The browser SSE endpoint is `/v1/executions/{id}/events` (mounted in `internal/api/http_server.go`).
- **One Jules Session at a Time:** Adhere strictly to the build order.
- **Do Not Touch `.orchestrator/` or `.claude/` in Product PRs:** Jules PRs must only touch production code and relevant tests.
- **All 4 CI Checks Required:** Every PR must have passing `UI Lint and Build`, `Integration Tests`, `Lint and Unit Tests`, and `Build Docker Images` before merging.
