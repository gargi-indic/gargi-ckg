#!/usr/bin/env python3
"""
Experiment 03: Conditional Branching & Dynamic Path Pruning

Demonstrates:
  - Dynamic evaluation of edge expressions based on node outputs
  - Execution of selected branch: manual_review_queue (when risk > 80)
  - Skipping/pruning unselected branch: auto_approve (marked NODE_STATUS_SKIPPED)
  - Downstream convergence on notify_decision
"""

import json
import pathlib
import sys
import time
import urllib.error
import urllib.request

REPO_ROOT = pathlib.Path(__file__).resolve().parent.parent
EXAMPLES_DIR = REPO_ROOT / "examples"
BASE_URL = "http://localhost:8080"
TEMPORAL_UI = "http://localhost:8233"


def http_get(path):
    url = f"{BASE_URL}{path}"
    req = urllib.request.Request(url)
    with urllib.request.urlopen(req) as resp:
        return resp.getcode(), resp.read().decode("utf-8")


def http_post(path, data):
    url = f"{BASE_URL}{path}"
    payload = json.dumps(data).encode("utf-8")
    req = urllib.request.Request(url, data=payload, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req) as resp:
        return resp.getcode(), json.loads(resp.read().decode("utf-8"))


def run():
    print("=" * 70)
    print("  Experiment 03: Conditional Branching & Dynamic Path Pruning")
    print("=" * 70)
    print("Graph: evaluate_risk -> (risk > 80 ? manual_review_queue : auto_approve) -> notify_decision\n")

    json_path = EXAMPLES_DIR / "conditional_branch.json"
    with open(json_path, "r", encoding="utf-8") as f:
        graph_def = f.read()

    exec_id = f"exp-branch-{int(time.time())}"
    payload = {
        "graphDefinition": graph_def,
        "executionId": exec_id,
        "wait": True,
        "timeout": "15s"
    }

    print(f"Submitting conditional DAG '{exec_id}' with wait=true...")
    code, res = http_post("/v1/executions", payload)
    print(f"Execution finished with status: {res.get('status')}")

    code, body = http_get(f"/v1/executions/{exec_id}")
    data = json.loads(body)
    print("\nBranching Decisions Observed:")
    for node_id, state in sorted(data.get("nodeStates", {}).items()):
        st = state.get("status")
        if node_id == "manual_review_queue":
            print(f"  • Branch Node '{node_id}': {st} (TAKEN because requires_manual_review == true)")
        elif node_id == "auto_approve":
            print(f"  • Branch Node '{node_id}': {st} (PRUNED/SKIPPED because condition was false)")
        else:
            print(f"  • Node '{node_id}': {st}")

    print(f"\nInspect workflow in Temporal Web UI: {TEMPORAL_UI}/namespaces/default/workflows/{exec_id}")
    return exec_id


if __name__ == "__main__":
    try:
        run()
    except Exception as e:
        print(f"[ERROR] Experiment failed: {e}", file=sys.stderr)
        sys.exit(1)
