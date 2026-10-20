#!/usr/bin/env python3
"""
Experiment 02: Fan-Out / Fan-In with Real-time SSE Streaming

Demonstrates:
  - Submitting a DAG asynchronously (wait=false)
  - Concurrency across fan-out nodes (partition_a and partition_b run in parallel)
  - Barrier synchronization at the fan-in node (aggregate_results)
  - Real-time event streaming via Server-Sent Events (SSE)
"""

import json
import pathlib
import sys
import threading
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
    print("  Experiment 02: Fan-Out / Fan-In with Real-time SSE Streaming")
    print("=" * 70)
    print("Graph: ingest_batch -> [process_partition_a, process_partition_b] -> aggregate_results\n")

    json_path = EXAMPLES_DIR / "fanout_fanin.json"
    with open(json_path, "r", encoding="utf-8") as f:
        graph_def = f.read()

    exec_id = f"exp-fanout-{int(time.time())}"

    # Background thread to listen to SSE events
    events_received = []
    stop_event = threading.Event()

    def listen_sse():
        sse_url = f"{BASE_URL}/v1/executions/{exec_id}/events"
        try:
            req = urllib.request.Request(sse_url)
            with urllib.request.urlopen(req, timeout=10) as resp:
                for line in resp:
                    if stop_event.is_set():
                        break
                    line_str = line.decode("utf-8")
                    if line_str.startswith("data:"):
                        data_part = line_str[5:].strip()
                        events_received.append(data_part)
                        print(f"  [SSE Event] {data_part}")
        except Exception:
            pass

    sse_thread = threading.Thread(target=listen_sse, daemon=True)
    sse_thread.start()
    time.sleep(0.3)

    print(f"Submitting async execution '{exec_id}'...")
    payload = {
        "graphDefinition": graph_def,
        "executionId": exec_id,
        "wait": False
    }
    code, res = http_post("/v1/executions", payload)
    print(f"Submission accepted: HTTP {code}, Initial Status: {res.get('status')}")

    # Poll execution state until terminal
    print("Waiting for DAG completion...")
    for _ in range(25):
        time.sleep(0.4)
        code, body = http_get(f"/v1/executions/{exec_id}")
        data = json.loads(body)
        status = data.get("status")
        if status in ["EXECUTION_STATUS_SUCCEEDED", "EXECUTION_STATUS_FAILED"]:
            print(f"Execution reached terminal status: {status}")
            break

    stop_event.set()
    time.sleep(0.3)

    code, body = http_get(f"/v1/executions/{exec_id}")
    data = json.loads(body)
    print("\nFinal Node States:")
    for node_id, state in sorted(data.get("nodeStates", {}).items()):
        print(f"  • Node '{node_id}': status={state.get('status')}, output={state.get('output', {})}")

    print(f"\nInspect workflow in Temporal Web UI: {TEMPORAL_UI}/namespaces/default/workflows/{exec_id}")
    return exec_id


if __name__ == "__main__":
    try:
        run()
    except Exception as e:
        print(f"[ERROR] Experiment failed: {e}", file=sys.stderr)
        sys.exit(1)
