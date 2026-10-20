# Gargi CKG Experiments Suite

This directory contains standalone and orchestrated experiments designed to validate and demonstrate the computational graph execution capabilities of Gargi CKG (Computational Knowledge Graph).

---

## Architecture Overview

```
                      +---------------------------------------+
                      |           Gargi REST Gateway          |
                      |        http://localhost:8080          |
                      +-------------------+-------------------+
                                          |
                        +-----------------+-----------------+
                        |                                   |
              [Workflow Dispatch]                 [Real-Time State]
                        |                                   |
                        v                                   v
             +--------------------+              +--------------------+
             |  Temporal Server   |              |  Redis StateStore  |
             |  localhost:7233    |              |  localhost:6379    |
             |  (UI: :8233)       |              +--------------------+
             +---------+----------+
                       |
                       v
             +--------------------+
             |    Gargi Worker    |
             | Task: gargi_tasks  |
             +--------------------+
```

---

## Experiments Included

### 1. Linear Multi-Stage Pipeline (`01_linear_pipeline.py`)
- **Graph Topology:** `fetch_user` $\rightarrow$ `calculate_discount` $\rightarrow$ `generate_invoice`
- **Execution Mode:** Synchronous request-response (`wait: true`)
- **Key Concepts:**
  - Sequential DAG topological sorting
  - Parameter mapping and node state transformation
  - Direct synchronous response with final status and node outputs

### 2. Fan-Out / Fan-In with Real-Time SSE Streaming (`02_fanout_fanin_streaming.py`)
- **Graph Topology:** `ingest_batch` $\rightarrow$ `[process_partition_a, process_partition_b]` $\rightarrow$ `aggregate_results`
- **Execution Mode:** Asynchronous dispatch (`wait: false`) + Server-Sent Events (`/v1/executions/{id}/events`)
- **Key Concepts:**
  - Concurrent branch execution across workers
  - Barrier synchronization where downstream node awaits all predecessor branches
  - Push-based event streaming as node statuses change (`RUNNING` $\rightarrow$ `SUCCEEDED`)

### 3. Conditional Branching & Dynamic Path Pruning (`03_conditional_branching.py`)
- **Graph Topology:** `evaluate_risk` $\rightarrow$ `(risk > 80 ? manual_review_queue : auto_approve)` $\rightarrow$ `notify_decision`
- **Execution Mode:** Dynamic edge evaluation (`wait: true`)
- **Key Concepts:**
  - Dynamic expression evaluation on node outputs (`output.risk_score > 80`)
  - Path pruning: unselected branches are marked `NODE_STATUS_SKIPPED`
  - Downstream convergence: unaffected nodes continue execution seamlessly

---

## Running the Experiments

### Prerequisites
Ensure the application stack is running:
```bash
# Terminal 1: Temporal Dev Server
temporal server start-dev --ip 0.0.0.0 --ui-ip 0.0.0.0

# Terminal 2: Local Redis Store
go run scripts/redis_dev.go

# Terminal 3: Gargi API Server
go run cmd/server/main.go -config config/config.local.yaml

# Terminal 4: Gargi Worker
go run cmd/worker/main.go -config config/config.local.yaml
```

### Run Entire Suite
```bash
python3 experiments/run_all.py
```

### Run Individual Experiments
```bash
python3 experiments/01_linear_pipeline.py
python3 experiments/02_fanout_fanin_streaming.py
python3 experiments/03_conditional_branching.py
```

### Web UI Inspection
Open [http://localhost:8233](http://localhost:8233) to inspect the live workflows, event history, activities, and DAG execution timelines.
