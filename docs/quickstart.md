# Quickstart Guide

This quickstart guides you through launching the Gargi Reactive Graph Engine, submitting graph workflows via gRPC and REST, streaming real-time updates, and interacting with running executions.

---

## 1. Prerequisites and Setup

### Prerequisites
- **Go** 1.22+
- **Docker** and **Docker Compose** (for Redis, PostgreSQL, Neo4j, Temporal services)
- **curl** or **grpcurl** (for API testing)

### Clone & Dependencies
```bash
git clone https://github.com/gargi-indic/gargi-flow.git
cd gargi-flow
go mod download
```

---

## 2. Launch the Infrastructure Stack

Launch Temporal, Redis, PostgreSQL, and Neo4j using the provided compose file:

```bash
# Start backend services
make up

# Check service status
docker compose ps
```

The Temporal Web UI is accessible at `http://localhost:8080`.

---

## 3. Run the Server & Worker

In one terminal, start the API server (gRPC port `50051`, HTTP gateway port `8080`):
```bash
go run ./cmd/server -config config/server.yaml
```

In a second terminal, start the Temporal workflow worker:
```bash
go run ./cmd/worker -config config/worker.yaml
```

Verify service readiness:
```bash
curl -i http://localhost:8080/healthz
# Response: HTTP/200 {"status":"SERVING"}
```

---

## 4. Submit an Execution

Gargi supports both **Async (fire-and-forget)** and **Sync (submit-and-wait)** execution modes.

### Option A: Asynchronous Submission (Fire-and-Forget)
Submit a graph and immediately receive the execution ID to poll or stream:

```bash
curl -X POST http://localhost:8080/v1/executions \
  -H "Content-Type: application/json" \
  -d '{
    "executionId": "order-exec-101",
    "graphDefinition": "{\"apiVersion\":\"graph/v1\",\"id\":\"simple-pipe\",\"nodes\":[{\"id\":\"step1\",\"type\":\"transform\",\"config\":{\"set\":{\"item\":\"laptop\"}}}],\"edges\":[]}"
  }'
```
Response:
```json
{
  "executionId": "order-exec-101",
  "status": "EXECUTION_STATUS_PENDING",
  "startedAt": "2026-10-19T13:20:00Z"
}
```

### Option B: Synchronous Submission (Submit-and-Wait)
Set `"wait": true` and an optional `"timeout"` (e.g. `"10s"`). The HTTP call blocks until the graph completes and returns the final state snapshot:

```bash
curl -X POST http://localhost:8080/v1/executions \
  -H "Content-Type: application/json" \
  -d '{
    "executionId": "order-exec-102",
    "wait": true,
    "timeout": "15s",
    "graphDefinition": "{\"apiVersion\":\"graph/v1\",\"id\":\"simple-pipe\",\"nodes\":[{\"id\":\"step1\",\"type\":\"transform\",\"config\":{\"set\":{\"verified\":true}}}],\"edges\":[]}"
  }'
```
Response:
```json
{
  "executionId": "order-exec-102",
  "status": "EXECUTION_STATUS_SUCCEEDED",
  "startedAt": "2026-10-19T13:20:00Z",
  "completedAt": "2026-10-19T13:20:01Z",
  "nodeStates": {
    "step1": {
      "nodeId": "step1",
      "status": "NODE_STATUS_SUCCEEDED",
      "output": {"verified": true}
    }
  }
}
```

---

## 5. Stream Real-Time Execution Updates

Gargi provides multiple streaming mechanisms for live reactive updates.

### Server-Sent Events (SSE)
Stream execution events in browser clients or scripts:
```bash
curl -N -H "Accept: text/event-stream" \
  http://localhost:8080/v1/executions/order-exec-101/events
```

Resuming from a specific sequence offset:
```bash
curl -N -H "Last-Event-ID: 5" \
  http://localhost:8080/v1/executions/order-exec-101/events
```

### WebSockets
Connect to `/v1/ws` or `/v1/executions/{id}/ws` using any standard WebSocket client:
```javascript
const ws = new WebSocket("ws://localhost:8080/v1/executions/order-exec-101/ws");
ws.onmessage = (event) => {
  const update = JSON.parse(event.data);
  console.log(`Node ${update.nodeId} transitioned to ${update.toStatus}`);
};
```

### gRPC Server Streaming
Using `grpcurl`:
```bash
grpcurl -plaintext -d '{"execution_id": "order-exec-101", "from_sequence": 0}' \
  localhost:50051 gargi.v1.GargiService/StreamExecutionUpdates
```

---

## 6. Query Execution State & Send Signals

### Query Full Execution Snapshot
```bash
curl http://localhost:8080/v1/executions/order-exec-101
```

### Query Single Node State
```bash
curl http://localhost:8080/v1/executions/order-exec-101/nodes/step1
```

### Send External Signal
Unblock a `wait_for_signal` node:
```bash
curl -X POST http://localhost:8080/v1/executions/order-exec-101/nodes/await_step/signal \
  -H "Content-Type: application/json" \
  -d '{
    "signalName": "approval_sig",
    "payload": "approved"
  }'
```

### Cancel an In-Flight Execution
```bash
curl -X POST http://localhost:8080/v1/executions/order-exec-101/cancel \
  -H "Content-Type: application/json" \
  -d '{"reason": "user requested cancellation"}'
```
