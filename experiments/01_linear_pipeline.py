#!/usr/bin/env python3
"""
Experiment 01: Linear Multi-Stage Pipeline (Synchronous Wait)

Demonstrates:
  - Submitting a DAG with wait=true (synchronous HTTP request-response)
  - Sequential execution across multiple dependent nodes
  - State propagation and node output verification
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
    print("  Experiment 01: Linear Multi-Stage Pipeline (Submit-and-Wait)")
    print("=" * 70)
    print("Graph: fetch_user -> calculate_discount -> generate_invoice\n")

    json_path = EXAMPLES_DIR / "linear_pipeline.json"
    with open(json_path, "r", encoding="utf-8") as f:
        graph_def = f.read()

    exec_id = f"exp-linear-{int(time.time())}"
    payload = {
        "graphDefinition": graph_def,
        "executionId": exec_id,
        "wait": True,
        "timeout": "15s"
    }

    print(f"Submitting execution '{exec_id}' with wait=true...")
    start_time = time.time()
    code, res = http_post("/v1/executions", payload)
    elapsed = time.time() - start_time

    print(f"Completed in {elapsed:.2f}s with status code {code}!")
    print(f"Execution ID : {res.get('executionId')}")
    print(f"Final Status : {res.get('status')}")

    # Inspect node states
    code, details_body = http_get(f"/v1/executions/{exec_id}")
    details = json.loads(details_body)
    print("\nNode Execution Summary:")
    for node_id, state in sorted(details.get("nodeStates", {}).items()):
        status = state.get("status")
        output = state.get("output", {})
        print(f"  • Node '{node_id}': status={status}, output={output}")

    print(f"\nInspect workflow in Temporal Web UI: {TEMPORAL_UI}/namespaces/default/workflows/{exec_id}")
    return exec_id


if __name__ == "__main__":
    try:
        run()
    except Exception as e:
        print(f"[ERROR] Experiment failed: {e}", file=sys.stderr)
        sys.exit(1)
