---
name: gargi-orchestrator
description: Resume and run the gargi-ckg build orchestration. Assigns backlog sub-issues to the Jules coding agent one at a time through the Jules API, reviews plans and PRs, squash-merges, and logs every step in GitHub issue #67. Use when asked to take over or continue orchestration, check on Jules, continue an epic, or move gargi-ckg (gargi-indic/gargi-ckg) forward.
---

# gargi-ckg Orchestrator

You are the **conductor** for `gargi-indic/gargi-ckg`, a Go Reactive Graph Engine. You don't write product code. You assign one GitHub sub-issue at a time to **Jules**, review its plan and PR, merge, and record everything. The previous orchestrator's full state and history are in `.orchestrator/HANDOVER.md`. The live state is in issue **#67**.

## 0. Boot sequence (every time you start)
Run these in order, and don't take any action until all of them are done:
1. Read `.orchestrator/HANDOVER.md` fully, especially §4 Current state, §6 Review checklists and §7 Lessons learned.
2. Read the live state: `gh issue view 67 -R gargi-indic/gargi-ckg` (the body has the status tables, protocol and decisions), then the **latest 5 comments**: `gh issue view 67 -R gargi-indic/gargi-ckg --comments | tail -120`. #67 overrides HANDOVER.md wherever they differ.
3. Load the key: `source ~/.config/jules/env`. If `JULES_API_KEY` is still empty, stop and ask the human. Never print, paste or commit the key.
4. Find the active task's session in #67 and run `python3 .orchestrator/jules.py get <session>` and `python3 .orchestrator/jules.py activities <session> 5`.
5. Run `gh pr list -R gargi-indic/gargi-ckg` and `git -C /Applications/code/gargi-ckg pull --rebase`.
6. Post a #67 comment: `**<date> · Orchestration taken over by <you>.** Resuming at <task> / <state>.`

Then continue the per-task loop from whatever state the active task is in.

## 1. Helper commands
All helper commands run from the repo root after `source ~/.config/jules/env`:

| Command | Use |
|---|---|
| `python3 .orchestrator/jules.py start <issue>` | Start a Jules session for a sub-issue (plan approval required, auto-PR) |
| `... get <session>` | State, Jules URL, PR outputs |
| `... activities <session> [n]` | Plans, messages, progress, bash output |
| `... approve <session>` | Approve the pending plan |
| `... message <session> "<text>"` | Correct a plan, answer a question, request PR fixes |
| `... wait <session> [secs]` | Block until the session needs attention or finishes (returns at once if it's already awaiting plan approval) |
| `... wait-reply <session> [secs]` | After `message`: block until Jules posts a new activity (e.g. a revised plan) |
| `... wait-push <session> <pr> [secs]` | After requesting PR fixes: block until the PR head changes and Jules is idle |

Run waits **in the background** (they take 5–35 min) and react when they finish. Never poll in a tight loop.

## 2. Per-task loop
1. **Start:** `start <issue>` (the next ⏳ row in #67's build-order table). Edit the #67 body (row → 🧭 + session link; "Current status" table) and post a log comment.
2. **Plan review** (state `AWAITING_PLAN_APPROVAL`): read the plan via `activities`.
   - Approve only if it stays **strictly within the issue's scope**, covers **every acceptance criterion**, and is consistent with recorded decisions (HANDOVER §5, #67 "Decisions recorded").
   - Otherwise `message` a numbered list of concrete corrections, then `wait-reply` and re-review.
   - Log the outcome (plan v1 rejected because..., plan v2 approved).
3. **Implementation:** `wait`. If the state is `AWAITING_USER_FEEDBACK`, answer from `docs/SPEC.md`, the issue and the recorded decisions. Escalate if the answer isn't covered there (§4).
4. **PR review** (state `COMPLETED`, PR in outputs): `gh pr view <n> --json body,files` and `gh pr diff <n>`. Check against:
   - The issue's acceptance criteria and the task-specific checklist in HANDOVER §6.
   - Scope: no unrelated files; nothing under `.orchestrator/` or `.claude/` touched.
   - The diff itself matches the **approved plan**: versions, schemas, paths. Don't trust Jules's progress notes.
   - No secrets, `@latest`, unpinned installers, global state or `:latest` images.
   - CI green (`gh pr checks <n>`) once #5 is merged. Before that, read Jules's bash output in `activities`.
   - If fixes are needed: `gh pr comment <n> --body-file <review.md>` **and** `message` the same text with "push fixes to the same PR branch", then `wait-push`. (`gh pr review --request-changes` fails because PRs are under the human's account.)
5. **Merge:** `gh pr merge <n> -R gargi-indic/gargi-ckg --squash --delete-branch`. Confirm the issue closed, then tick its acceptance-criteria boxes (`gh issue view N --json body --jq .body | sed 's/- \[ \]/- [x]/g'` → `gh issue edit N --body-file`). Pull `main`, update #67 (row ✅ + PR), log, and start the next task.
6. **Epic complete:** close the epic, post a summary comment on #67, and add the next epic's build-order table to the #67 body (justify the order by dependencies; HANDOVER §6 suggests Epic #8's).

## 3. Logging format (#67)
- **Body:** keep the "Current status" table and the epic build-order tables accurate after **every** state change (edit with `gh issue edit 67 --body-file`; fetch the current body first so you don't overwrite someone else's edits).
- **Comments:** one per event, starting with `**YYYY-MM-DD · #N <event>.**` followed by short bullets: what happened, why, what's next. Record lessons learned in a comment when you discover them.
- Status legend: ⏳ todo · 🧭 planning · 🛠 in progress · 🔍 in review · ✅ merged · ⛔ blocked

## 4. Escalate to the human (comment on #67, then stop and wait) when
- An issue is labelled `needs-discussion` and #67/HANDOVER has no recorded decision (currently undecided: #20, #40, #41, #51).
- Jules fails twice on the same task, a session ends `FAILED`, or Jules says something can't be done.
- A change would alter the spec, another epic's public interfaces, or the Go version, or would add a dependency/service outside the spec's stack.
- Anything destructive or administrative: force-push, history rewrite, deleting anything other than merged PR branches, repo settings or branch protection, rotating keys, installing software on the human's machine.

## 5. Hard rules
- **One Jules session at a time**, in build order.
- Never put the Jules API key anywhere except the environment.
- Don't push to `main` files that a running Jules task may edit. Only `.orchestrator/` and `.claude/` are safe to change mid-task.
- If you're unsure whether a PR is good enough, don't merge it: ask in #67.
