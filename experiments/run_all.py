#!/usr/bin/env python3
"""
Gargi CKG Experiment Runner
Executes full suite of workflow graph execution experiments:
  1. Service health and readiness verification
  2. Synchronous linear DAG execution (Submit & Wait)
  3. Real-time event streaming via SSE (Fan-out / Fan-in)
  4. Conditional branching & dynamic path pruning
"""

import importlib.util
import json
import pathlib
import sys
import urllib.error
import urllib.request

REPO_ROOT = pathlib.Path(__file__).resolve().parent.parent
BASE_URL = "http://localhost:8080"


def print_banner(title):
    print("\n" + "=" * 70)
    print(f"  {title}")
    print("=" * 70)


def check_health():
    print_banner("Preflight: Checking Service Health & Readiness")
    req = urllib.request.Request(f"{BASE_URL}/healthz")
    with urllib.request.urlopen(req) as resp:
        print(f"[Healthz] HTTP {resp.getcode()}: {resp.read().decode('utf-8').strip()}")

    req = urllib.request.Request(f"{BASE_URL}/readyz")
    with urllib.request.urlopen(req) as resp:
        body = resp.read().decode("utf-8").strip()
        print(f"[Readyz]  HTTP {resp.getcode()}: {body}")
        ready_data = json.loads(body)
        assert ready_data.get("status") == "READY", f"Server not ready: {ready_data}"

    print("[OK] Services (Gargi API, Temporal Engine, Redis StateStore) are fully healthy!\n")


def import_module(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def main():
    experiments_dir = pathlib.Path(__file__).resolve().parent

    check_health()

    print_banner("Running Experiment 1: Linear Multi-Stage Pipeline")
    exp1 = import_module(experiments_dir / "01_linear_pipeline.py", "exp1")
    id1 = exp1.run()

    print_banner("Running Experiment 2: Fan-Out / Fan-In with SSE Streaming")
    exp2 = import_module(experiments_dir / "02_fanout_fanin_streaming.py", "exp2")
    id2 = exp2.run()

    print_banner("Running Experiment 3: Conditional Branching & Dynamic Pruning")
    exp3 = import_module(experiments_dir / "03_conditional_branching.py", "exp3")
    id3 = exp3.run()

    print_banner("ALL EXPERIMENTS COMPLETED SUCCESSFULLY!")
    print(f"Summary of Executions Created:")
    print(f"  1. Linear Pipeline:       {id1}")
    print(f"  2. Fan-Out / Fan-In:      {id2}")
    print(f"  3. Conditional Branching: {id3}")
    print("\nInspect all executions at Temporal Web UI: http://localhost:8233")


if __name__ == "__main__":
    try:
        main()
    except Exception as e:
        print(f"\n[ERROR] Experiment runner failed: {e}", file=sys.stderr)
        import traceback
        traceback.print_exc()
        sys.exit(1)
