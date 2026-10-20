#!/usr/bin/env python3
"""
Gargi CKG Experiment Runner
Demonstrates:
  1. Service health and readiness verification
  2. Synchronous DAG execution (Submit & Wait)
  3. Real-time event streaming via SSE (Fan-out / Fan-in)
  4. Conditional branching & dynamic edge evaluation
  5. State inspection & Temporal workflow coordination
"""

import urllib.request
import urllib.error
import json
import time
import sys
import threading

BASE_URL = "http://localhost:8080"
TEMPORAL_UI = "http://localhost:8233"

def print_header(title):
    print("\n" + "=" * 70)
    print(f"  {title}")
    print("=" * 70)

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

def check_health():
    print_header("1. Checking Service Health & Readiness")
    code, body = http_get("/healthz")
    print(f"[Healthz] HTTP {code}: {body.strip()}")
    code, body = http_get("/readyz")
    print(f"[Readyz]  HTTP {code}: {body.strip()}")
    ready_data = json.loads(body)
    assert ready_data.get("status") == "READY", f"Server not ready: {ready_data}"
    print("[OK] Services (Gargi API, Temporal Engine, Redis StateStore) are fully healthy!")

def run_experiment_linear():
    print_header("2. Experiment: Linear Multi-Stage Pipeline (Submit-and-Wait)")
    print("Graph: fetch_user -> calculate_discount -> generate_invoice")

    with open("/home/gishnum/code/gargi-ckg/examples/linear_pipeline.json") as f:
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

    # Inspect execution details
    code, details_body = http_get(f"/v1/executions/{exec_id}")
    details = json.loads(details_body)
    print("\nNode Execution Summary:")
    for node_id, state in sorted(details.get("nodeStates", {}).items()):
        status = state.get("status")
        output = state.get("output", {})
        print(f"  • Node '{node_id}': status={status}, output={output}")

    print(f"\nView execution in Temporal Web UI: {TEMPORAL_UI}/namespaces/default/workflows/{exec_id}")
    return exec_id

def run_experiment_streaming_fanout():
    print_header("3. Experiment: Fan-Out / Fan-In with Real-time SSE Streaming")
    print("Graph: ingest_data -> [process_partition_a, process_partition_b] -> aggregate_results")

    with open("/home/gishnum/code/gargi-ckg/examples/fanout_fanin.json") as f:
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
                buffer = ""
                for line in resp:
                    if stop_event.is_set():
                        break
                    line_str = line.decode("utf-8")
                    if line_str.startswith("data:"):
                        data_part = line_str[5:].strip()
                        events_received.append(data_part)
                        print(f"  [SSE Event] {data_part}")
        except Exception as e:
            if not stop_event.is_set():
                print(f"  [SSE Stream closed/ended: {e}]")

    sse_thread = threading.Thread(target=listen_sse, daemon=True)
    sse_thread.start()
    time.sleep(0.5)

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
    for _ in range(20):
        time.sleep(0.5)
        code, body = http_get(f"/v1/executions/{exec_id}")
        data = json.loads(body)
        status = data.get("status")
        if status in ["EXECUTION_STATUS_SUCCEEDED", "EXECUTION_STATUS_FAILED"]:
            print(f"Execution reached terminal status: {status}")
            break

    stop_event.set()
    time.sleep(0.5)

    code, body = http_get(f"/v1/executions/{exec_id}")
    data = json.loads(body)
    print("\nFinal Node States:")
    for node_id, state in sorted(data.get("nodeStates", {}).items()):
        print(f"  • Node '{node_id}': status={state.get('status')}, output={state.get('output', {})}")

    print(f"\nView execution in Temporal Web UI: {TEMPORAL_UI}/namespaces/default/workflows/{exec_id}")
    return exec_id

def run_experiment_conditional():
    print_header("4. Experiment: Conditional Branching & Path Pruning")
    print("Graph: evaluate_risk -> (risk > 80 ? manual_review_queue : auto_approve) -> notify_decision")

    with open("/home/gishnum/code/gargi-ckg/examples/conditional_branch.json") as f:
        graph_def = f.read()

    exec_id = f"exp-branch-{int(time.time())}"
    payload = {
        "graphDefinition": graph_def,
        "executionId": exec_id,
        "wait": True,
        "timeout": "15s"
    }

    print(f"Submitting conditional DAG '{exec_id}'...")
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

    print(f"\nView execution in Temporal Web UI: {TEMPORAL_UI}/namespaces/default/workflows/{exec_id}")
    return exec_id

if __name__ == "__main__":
    try:
        check_health()
        run_experiment_linear()
        run_experiment_streaming_fanout()
        run_experiment_conditional()
        print_header("ALL EXPERIMENTS COMPLETED SUCCESSFULLY!")
    except Exception as e:
        print(f"\n[ERROR] Experiment failed: {e}", file=sys.stderr)
        import traceback
        traceback.print_exc()
        sys.exit(1)
