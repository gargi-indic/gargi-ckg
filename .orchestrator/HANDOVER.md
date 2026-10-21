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

### Epic #101: `[Epic] UI Prototype: Visual DAG Canvas with Live Execution` (✅ Complete)
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
| #111 | [UI] Trigger human-in-the-loop signals from node inspector | #122 | ✅ Merged | `58bbbb0` |
| #112 | [UI] Documentation & unified dev-up script | #123 | ✅ Merged | `16b31e9` |

---

## 4. Current State & Immediate Resume Instructions

### Epic #101 Complete
- All 11 sub-issues (#102–#112) have been implemented, reviewed, tested with all 4 CI checks passing green, and squash-merged into `main`.
- Epic #101 issue closed.
- Prototype capabilities delivered:
  - Vite + React 19 + TypeScript + React Flow (`@xyflow/react`) visual DAG execution canvas in `web/`.
  - Auto-layout via Dagre and live node animation via SSE streaming (`/v1/executions/{id}/events`).
  - Node inspector drawer (timings, inputs, outputs, errors, live config editing, signal form).
  - Graph authoring palette with drag-and-drop, connect, delete, cycle detection, edge CEL editing, and YAML/JSON export.
  - Interactive Human-in-the-Loop signal triggers for `wait_for_signal` nodes.
  - Unified docker-free local dev stack (`scripts/dev-up.sh`) and Playwright E2E smoke tests.
- **Immediate Next Action**: Consult backlog / maintainers for next epic or production hardening items.
  3. Review implementation and PR, ensure all 4 CI checks pass, and squash-merge.
  4. Close Epic #101 upon merge.

---

## 5. Review Checklists & Key Architecture Decisions
- **Frontend Stack in `web/`:** Vite + React 19 + TypeScript + `@xyflow/react` + `dagre` (top-to-bottom layout) + `js-yaml` + `openapi-fetch`.
- **Backend Streaming Endpoint:** The browser SSE endpoint is `/v1/executions/{id}/events` (mounted in `internal/api/http_server.go`).
- **One Jules Session at a Time:** Adhere strictly to the build order.
- **Do Not Touch `.orchestrator/` or `.claude/` in Product PRs:** Jules PRs must only touch production code and relevant tests.
- **All 4 CI Checks Required:** Every PR must have passing `UI Lint and Build`, `Integration Tests`, `Lint and Unit Tests`, and `Build Docker Images` before merging.
