#!/usr/bin/env python3
"""Minimal Jules API client used by the orchestrator agent.

Requires JULES_API_KEY in the environment (e.g. `source ~/.config/jules/env`).

Usage:
  jules.py start <issue-number>            Start a Jules session for a sub-issue (prompt built from the issue)
  jules.py get <session-id>                Print session state, URL and PR outputs as JSON
  jules.py activities <session-id> [n]     Print the latest n activities (default 10)
  jules.py approve <session-id>            Approve the pending plan
  jules.py message <session-id> <text>     Send a follow-up message to Jules
  jules.py wait <session-id> [timeout-s]   Block until the session needs attention or finishes
  jules.py wait-reply <session-id> [timeout-s]
                                           Block until Jules posts a new activity (e.g. a revised plan
                                           or an answer) after the latest one existing now
  jules.py wait-push <session-id> <pr-number> [timeout-s]
                                           After asking Jules for PR fixes: block until the PR head commit
                                           changes and the session stops working (or needs attention)
"""
import http.client
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request

API = "https://jules.googleapis.com/v1alpha"
REPO = "gargi-indic/gargi-flow"
SOURCE = f"sources/github/{REPO}"
# States in which the orchestrator must act (or the session is over).
ATTENTION = {"AWAITING_PLAN_APPROVAL", "AWAITING_USER_FEEDBACK", "PAUSED", "COMPLETED", "FAILED"}

PROMPT = """You are implementing one sub-task of the gargi-flow project: a pluggable Reactive Graph Engine middleware written in Go.

Read docs/SPEC.md for the overall requirements before starting.

## Task: #{number} {title}
Parent epic: {epic}

{body}

## Rules
- Implement ONLY the scope of this task. Do not start other backlog items; leave TODOs referencing issue numbers if needed.
- Language: Go (1.23+). Keep code idiomatic: gofmt/goimports clean, no global state, dependencies injected via constructors.
- Before finishing, run `go build ./...`, `go vet ./...` and `go test ./...` (and `make lint` / `make test` once a Makefile exists) and make sure they pass.
- Do not modify anything under `.orchestrator/`.
- Open one pull request titled "#{number}: {title}" whose description starts with "Closes #{number}" and lists how each acceptance criterion is met.
"""


def key():
    k = os.environ.get("JULES_API_KEY")
    if not k:
        sys.exit("JULES_API_KEY is not set (source ~/.config/jules/env)")
    return k


def all_activities(session):
    """Fetch every activity, following nextPageToken."""
    acts, token = [], ""
    while True:
        page = call("GET", f"{sid(session)}/activities?pageSize=30" + (f"&pageToken={token}" if token else ""))
        acts.extend(page.get("activities", []))
        token = page.get("nextPageToken")
        if not token:
            return acts


def call(method, path, body=None):
    for attempt in range(8):
        req = urllib.request.Request(
            f"{API}/{path}",
            method=method,
            data=json.dumps(body).encode() if body is not None else None,
            headers={"X-Goog-Api-Key": key(), "Content-Type": "application/json"},
        )
        try:
            with urllib.request.urlopen(req, timeout=60) as resp:
                raw = resp.read()
                return json.loads(raw) if raw else {}
        except urllib.error.HTTPError as e:
            sys.exit(f"HTTP {e.code} on {method} {path}: {e.read().decode()}")
        except (urllib.error.URLError, ConnectionError, OSError, http.client.IncompleteRead, http.client.HTTPException) as e:
            if attempt == 7:
                sys.exit(f"Network error on {method} {path} after 8 attempts: {e}")
            time.sleep(3 * (attempt + 1))


def sid(s):
    return s if s.startswith("sessions/") else f"sessions/{s}"


def summary(sess):
    prs = [o["pullRequest"] for o in sess.get("outputs", []) if "pullRequest" in o]
    return {
        "id": sess.get("id") or sess.get("name", "").split("/")[-1],
        "title": sess.get("title"),
        "state": sess.get("state"),
        "url": sess.get("url"),
        "updateTime": sess.get("updateTime"),
        "pullRequests": [{"url": p.get("url"), "title": p.get("title")} for p in prs],
    }


def start(number):
    issue = json.loads(subprocess.check_output(
        ["gh", "issue", "view", str(number), "-R", REPO, "--json", "title,body"]))
    parent = json.loads(subprocess.check_output(
        ["gh", "api", f"repos/{REPO}/issues/{number}/parent", "--jq", "{number, title}"]))
    prompt = PROMPT.format(number=number, title=issue["title"], body=issue["body"],
                           epic=f"#{parent['number']} {parent['title']}")
    sess = call("POST", "sessions", {
        "title": f"#{number}: {issue['title']}",
        "prompt": prompt,
        "sourceContext": {"source": SOURCE, "githubRepoContext": {"startingBranch": "main"}},
        "automationMode": "AUTO_CREATE_PR",
        "requirePlanApproval": True,
    })
    print(json.dumps(summary(sess), indent=2))


def main():
    if len(sys.argv) < 3:
        sys.exit(__doc__)
    cmd, arg = sys.argv[1], sys.argv[2]
    if cmd == "start":
        start(int(arg))
    elif cmd == "get":
        print(json.dumps(summary(call("GET", sid(arg))), indent=2))
    elif cmd == "activities":
        n = int(sys.argv[3]) if len(sys.argv) > 3 else 10
        acts = all_activities(arg)
        print(json.dumps(acts[-n:], indent=2))
    elif cmd == "approve":
        call("POST", f"{sid(arg)}:approvePlan", {})
        print("plan approved")
    elif cmd == "message":
        call("POST", f"{sid(arg)}:sendMessage", {"prompt": " ".join(sys.argv[3:])})
        print("message sent")
    elif cmd == "wait":
        deadline = time.time() + (int(sys.argv[3]) if len(sys.argv) > 3 else 3600)
        while True:
            s = summary(call("GET", sid(arg)))
            if s["state"] in ATTENTION or time.time() > deadline:
                print(json.dumps(s, indent=2))
                return
            time.sleep(60)
    elif cmd == "wait-reply":
        deadline = time.time() + (int(sys.argv[3]) if len(sys.argv) > 3 else 3600)
        seen = {a["id"] for a in all_activities(arg)}
        while True:
            acts = all_activities(arg)
            # Ignore Jules's frequent progressUpdated pings; wait for a real reply (message, plan, end).
            new = [a for a in acts if a["id"] not in seen and a.get("originator") == "agent"
                   and any(k in a for k in ("agentMessaged", "planGenerated", "sessionCompleted", "sessionFailed"))]
            if new or time.time() > deadline:
                print(json.dumps({"session": summary(call("GET", sid(arg))), "newActivities": new}, indent=2))
                return
            time.sleep(60)
    elif cmd == "wait-push":
        pr = sys.argv[3]
        deadline = time.time() + (int(sys.argv[4]) if len(sys.argv) > 4 else 3600)

        def head():
            return subprocess.check_output(
                ["gh", "pr", "view", pr, "-R", REPO, "--json", "headRefOid", "--jq", ".headRefOid"], text=True).strip()

        start_head = head()
        while True:
            time.sleep(60)
            s = summary(call("GET", sid(arg)))
            pushed = head() != start_head
            busy = s["state"] in {"QUEUED", "PLANNING", "IN_PROGRESS"}
            if (pushed and not busy) or s["state"] in ATTENTION - {"COMPLETED"} or time.time() > deadline:
                print(json.dumps({"session": s, "prHeadChanged": pushed, "prHead": head()}, indent=2))
                return
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main()
